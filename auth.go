package gort

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

// authTokenEnv names the environment variable holding the shared API token.
const authTokenEnv = "GORT_AUTH_TOKEN"

// RequireToken returns middleware that rejects requests whose
// "Authorization: Bearer <token>" header does not match token.
// An empty token disables the check, which is meant only for trusted
// networks; main logs a warning in that case.
func RequireToken(token string) func(http.Handler) http.Handler {
	if token == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	// Compare fixed-length digests so the comparison time does not leak the token length.
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := bearerToken(r.Header.Get("Authorization"))
			gotSum := sha256.Sum256([]byte(got))
			if !ok || subtle.ConstantTimeCompare(gotSum[:], want[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="gort"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearerToken extracts the token from an Authorization header value.
func bearerToken(header string) (string, bool) {
	const prefix = "bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	return token, token != ""
}
