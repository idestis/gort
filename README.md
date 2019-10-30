# GORT

[![Build Status](https://cloud.drone.io/api/badges/idestis/gort/status.svg)](https://cloud.drone.io/idestis/gort)

![Moving Gopher as GORT](./assets/MovingGopher.png)

**GORT** is a simple HTTP handler to receive remote calls to run scripts bundled in Docker containers

## Usage

> Refer to [Install](#Install) for getting `gort` binary

To use `gort` in your Docker container download the latest version from [Project Release Page](https://github.com/idestis/gort/releases)

**Gort** uses the following environment variables for a customized run

`PORT` as default will be handled over **5000** port

`SCRIPTS_DIR` by default will search scripts at **./dist** in the same directory where the binary

### Endpoints

#### Health

Allows you to check the health of application/container

* **URL**
  
  `/v1/health`

* **Method**

  `GET`

* **Success Response**

  * **Code:** `200`<br/>
    **Content:** `OK`

#### List

List scripts directory

* **URL**
  
  `/v1/list-dist`

* **Method**

  `GET`

* **Success Response**

  * **Code:** `200`

#### Start

Allows you to start script from the scripts directory

* **URL**
  
  `/v1/start`

* **Method**

  `POST`

* **Data**

  Requires JSON data as payload

  ```json
  {
    "executor": "node",
    "script": "script.js",
    "env_vars": [
      "FOO=bar",
      "BAR=foo"
    ]
  }
  ```

* **Success Response**

  * **Code:** `200` <br/>
    **Content:** `The function is executed in the background. Refer to container logs to see the output`

* **Error Responses**

  * **Code:** `400` <br/>
    **Content:** `Required parameters 'executor' and 'script' were not found in the payload`

  * **Code:** `422`<br/>
    **Content:** `Not able to parse data as valid JSON`

  * **Code:** `500` <br>
    **Content:** `Requested executor is not installed`

  * **Code:** `501` <br>
    **Content:** `Requested script is not found in the scripts directory`
  
* **Sample Call:**

```bash
$ curl -X POST https://127.0.0.1:5000/v1/start -d '{"executor":"python", "script": "crawler.py"}'
The function will be executed in the background. Refer to container logs to see the output
```

## Install

### Binary

Binary are available for download on the [Project Release Page](https://github.com/idestis/gort/releases)

However, you also able to change something in the source code and build your ``Gort`` for yourself

```bash
$ go build ./...
```

## Use cases

## Dockerfile Example Usage

For instance, we have a few crawlers which should be executed on demand.

They was built by NPM

```Dockerfile
#
# STEP ONE: Build scripts
#
FROM node:10 AS builder
# Create app directory
WORKDIR /app
# Copy our files inside of the docker builder
COPY . .
# Install dependencies
RUN npm install && npm build
#
# STEP TWO: Build our images bundled with gort and copy scripts from builder
#
FROM alpine
# Create app directory
WORKDIR /app
# Install dependecies
RUN apk add --no-cache bash nodejs ca-certificates && \
    wget https://githublink/gort-amd64-linux -O gort && chmox +x gort
# Copy our scripts from builder step
COPY --from=builder /app/dist /app/dist
# PORT variable for Gort
ENV PORT 8080
# Expose outside
EXPOSE 8080
ENTRYPOINT ['gort']
```

## Contribute

Refer to [CONTRIBUTING.md](./CONTRIBUTING.md)

## Dependencies

* github.com/go-chi/chi

## TBD

* Authorization method to protect your endpoints
