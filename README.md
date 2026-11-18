# GORT

[![CI](https://github.com/idestis/gort/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/idestis/gort/actions/workflows/ci.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/idestis/gort.svg)](https://pkg.go.dev/github.com/idestis/gort) [![Go Report Card](https://goreportcard.com/badge/github.com/idestis/gort)](https://goreportcard.com/report/github.com/idestis/gort)

<p align="center">
  <img src="./assets/MovingGopher.png" alt="Moving Gopher as GORT" width="150">
</p>

**GORT** ("GO-Run-Things") is a small HTTP server that runs scripts bundled in a Docker container on request. You ship your scripts and the interpreters they need in one image, put `gort` in front, and trigger runs with a `POST` from a scheduler, CI job or another service.

> **Internal service.** GORT is meant to run inside a private network or a Kubernetes cluster, never exposed to the internet. Set `GORT_AUTH_TOKEN` and keep it behind your firewall.

## Quick start

```bash
mkdir -p dist && printf 'echo "hello $1"\n' > dist/hello.sh

GORT_AUTH_TOKEN=s3cret GORT_ALLOWED_EXECUTORS=sh ./gort
```

```bash
curl -X POST http://127.0.0.1:5000/v1/start \
  -H 'Authorization: Bearer s3cret' \
  -d '{"executor": "sh", "script": "hello.sh", "args": ["world"]}'
```

The script runs in the background and its output goes to gort's stdout/stderr, i.e. the container logs.

## Configuration

Everything is configured through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `PORT` | `5000` | Port to listen on. |
| `SCRIPTS_DIR` | `./dist` | Directory with the scripts that may be run. It is scanned once at startup; restart gort after adding scripts. |
| `GORT_AUTH_TOKEN` | *(unset)* | Shared token. When set, every endpoint except `/v1/health` requires `Authorization: Bearer <token>`. When unset, auth is disabled and a warning is logged. |
| `GORT_ALLOWED_EXECUTORS` | *(unset)* | Comma-separated interpreters that may be requested, e.g. `sh,bash,python3,node`. Matching is exact: `python3` does not allow `/usr/bin/python3`. **When unset, every `/v1/start` request is rejected.** |
| `GORT_ALLOWED_ENV_VARS` | *(unset)* | Comma-separated env var names a request may set, e.g. `FOO,APP_*` (a trailing `*` matches a prefix). When unset, requests with `env_vars` are rejected. |
| `GORT_MAX_CONCURRENT` | `10` | Maximum number of scripts running at the same time. Further requests get `429` until a script finishes. |
| `GORT_RATE_LIMIT` | *(unset)* | Optional cap on in-flight HTTP requests to `/v1/*` (must be a positive integer). This limits requests, not running scripts — use `GORT_MAX_CONCURRENT` for that. |

### Security model

GORT only runs scripts that exist in `SCRIPTS_DIR`, and only with interpreters you explicitly allow. Some environment variables can hijack an interpreter, so they are **always** rejected, even if they are on the allowlist:

`PATH`, `IFS`, `BASH_ENV`, `ENV`, `SHELLOPTS`, `PS4`, `PROMPT_COMMAND`, `NODE_OPTIONS`, `PYTHONPATH`, `PYTHONSTARTUP`, `PERL5OPT`, `PERL5LIB`, `RUBYOPT`, `GCONV_PATH`, `LD_*`, `DYLD_*`.

GORT logs a warning at startup for every insecure or disabling setting.

### Server behaviour

- HTTP timeouts: 5s to read headers, 15s to read the request, 30s to write the response, 60s for idle keep-alive connections. Request bodies are limited to 1 MiB.
- Every started script is waited on, so finished scripts don't pile up as zombie processes.
- On `SIGTERM`/`SIGINT` gort stops accepting requests, waits up to 30s for running scripts to finish, then sends `SIGTERM` to the rest.

## API

All endpoints except `/v1/health` require the bearer token when `GORT_AUTH_TOKEN` is set. A missing or wrong token returns `401 Unauthorized`.

### `GET /v1/health`

Liveness check. Returns `200 OK` with body `OK`. Never requires auth, so it can be used by Docker and Kubernetes probes.

### `GET /v1/list-dist`

Lists the scripts found in `SCRIPTS_DIR`, one per line.

### `POST /v1/start`

Starts a script in the background.

```json
{
  "executor": "node",
  "script": "script.js",
  "env_vars": ["FOO=bar", "APP_MODE=nightly"],
  "args": ["--foo=bar", "--bar=foo"]
}
```

| Field | Required | Description |
| --- | --- | --- |
| `executor` | yes | Interpreter to run the script with. Must be in `GORT_ALLOWED_EXECUTORS` and installed. |
| `script` | yes | File name inside `SCRIPTS_DIR`. |
| `env_vars` | no | `KEY=VALUE` pairs added to the script's environment. Keys must be in `GORT_ALLOWED_ENV_VARS`. |
| `args` | no | Extra arguments passed to the script. |

Responses:

| Code | Meaning |
| --- | --- |
| `200` | Script started. Its output is in the container logs. |
| `400` | `executor` or `script` is missing. |
| `401` | Missing or invalid token. |
| `403` | Executor or env var is not allowed. |
| `413` | Request body is larger than 1 MiB. |
| `422` | Body is not valid JSON. |
| `429` | `GORT_MAX_CONCURRENT` scripts are already running. Retry later. |
| `500` | Executor is not installed, or the script failed to start. |
| `501` | Script is not in `SCRIPTS_DIR`. |
| `503` | gort is shutting down. |

## Install

### Binary

Download a binary from the [releases page](https://github.com/idestis/gort/releases). Releases are built for **arm64 only** (linux, darwin, windows, freebsd). Assets are raw binaries named `gort_<version>_<os>_arm64` (for example `gort_1.3.0_linux_arm64`; Windows adds `.exe`), plus a `checksums.txt` file. For other architectures, build from source.

### From source

Requires Go 1.27 or newer.

```bash
go build -o gort ./cmd/gort
```

## Docker example

Build your scripts in one stage, then bundle them with gort and the interpreters they need. The image verifies the download against the release's `checksums.txt`, runs as a non-root user, and has a health check:

```Dockerfile
# syntax=docker/dockerfile:1
# Gort releases are arm64-only, so the runtime image is pinned to linux/arm64.

# STEP 1: build the scripts (output must land in /app/dist)
FROM --platform=$BUILDPLATFORM node:24-alpine AS builder
WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
RUN npm run build

# STEP 2: runtime image with gort, the interpreters and the scripts
FROM --platform=linux/arm64 alpine:3.24
ARG GORT_VERSION=1.3.0
ARG GORT_URL=https://github.com/idestis/gort/releases/download/v${GORT_VERSION}
RUN apk add --no-cache nodejs ca-certificates tini && \
    cd /tmp && \
    wget -q "${GORT_URL}/gort_${GORT_VERSION}_linux_arm64" "${GORT_URL}/checksums.txt" && \
    grep " gort_${GORT_VERSION}_linux_arm64\$" checksums.txt | sha256sum -c - && \
    install -m 0755 "gort_${GORT_VERSION}_linux_arm64" /usr/local/bin/gort && \
    rm -f /tmp/gort_* /tmp/checksums.txt && \
    adduser -D -H -s /sbin/nologin gort

WORKDIR /app
COPY --from=builder /app/dist ./dist
USER gort

# Provide GORT_AUTH_TOKEN at runtime, e.g. from a Kubernetes Secret.
ENV PORT=8080 \
    GORT_ALLOWED_EXECUTORS=node
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO /dev/null http://127.0.0.1:8080/v1/health || exit 1
# tini runs as PID 1 and reaps orphaned grandchildren of your scripts.
ENTRYPOINT ["/sbin/tini", "--", "gort"]
```

```bash
docker build -t my-crawlers .
docker run -d -p 8080:8080 -e GORT_AUTH_TOKEN=s3cret my-crawlers
```

[tini](https://github.com/krallin/tini) runs as PID 1. Gort reaps the scripts it starts, but if a script backgrounds its own child processes and exits, those orphans are only cleaned up by a real init. `GORT_URL` can point at a mirror if GitHub is not reachable from your build.

Trigger a run from anywhere inside the cluster:

```bash
curl -X POST http://gort:8080/v1/start \
  -H "Authorization: Bearer $GORT_AUTH_TOKEN" \
  -d '{"executor": "node", "script": "crawler.js"}'
```

That makes gort a simple target for scheduled jobs in Kubernetes CronJobs, Jenkins, GitHub Actions or any other scheduler.

## Development

```text
cmd/gort/           entry point: wiring, signal handling, graceful shutdown
internal/config/    environment variable loading
internal/policy/    executor and env var allowlists
internal/runner/    process launch, reaping and concurrency limit
internal/server/    HTTP router, handlers, auth middleware
```

Common commands are in the [Taskfile](./Taskfile.yml) (install [Task](https://taskfile.dev/installation/), e.g. `brew install go-task`). Run `task` to list them.

| Command | What it does |
| --- | --- |
| `task run` | Run gort locally with `sh` allowed and token `dev`, serving `./dist` |
| `task lint` | `gofmt` check, `go vet`, and `golangci-lint` if installed |
| `task test` | Tests with the race detector, coverage in `c.out` |
| `task build` | Build `./gort` for this machine (`task build:arm64` for the linux/arm64 release binary) |
| `task ci` | `lint`, `test` and `build`, exactly what CI runs |
| `task release:snapshot` | Build every release artifact locally into `.goreleaser-dist`, publishing nothing |

CI on GitHub Actions runs the same `task lint`, `task test` and `task build`, plus `golangci-lint`, on every push to `main` and every pull request.

## Releasing

Releases are built by GoReleaser in GitHub Actions and published to the [releases page](https://github.com/idestis/gort/releases). Pick one way:

- **From your machine:** `task release VERSION=1.3.0`. It checks that you are on a clean `main` in sync with `origin`, runs `task ci`, then creates and pushes tag `v1.3.0`. The tag triggers the Release workflow.
- **From GitHub:** go to **Actions → Release → Run workflow** and enter `1.3.0`. The workflow tags the tip of `main`, runs the tests, pushes the tag and publishes.

Both refuse versions that aren't semver or tags that already exist.

## Roadmap

- List of running scripts and their status

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).
