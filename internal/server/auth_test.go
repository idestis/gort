package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	tests := []struct {
		name   string
		token  string
		header string
		want   int
	}{
		{"disabled when token empty", "", "", http.StatusNoContent},
		{"missing header", "s3cret", "", http.StatusUnauthorized},
		{"wrong token", "s3cret", "Bearer nope", http.StatusUnauthorized},
		{"prefix of token", "s3cret", "Bearer s3cre", http.StatusUnauthorized},
		{"wrong scheme", "s3cret", "Basic s3cret", http.StatusUnauthorized},
		{"empty bearer", "s3cret", "Bearer ", http.StatusUnauthorized},
		{"valid", "s3cret", "Bearer s3cret", http.StatusNoContent},
		{"scheme is case-insensitive", "s3cret", "bearer s3cret", http.StatusNoContent},
		{"surrounding spaces trimmed", "s3cret", "Bearer   s3cret  ", http.StatusNoContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/list-dist", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rr := httptest.NewRecorder()
			RequireToken(tt.token)(ok).ServeHTTP(rr, req)
			if rr.Code != tt.want {
				t.Fatalf("status = %d, want %d", rr.Code, tt.want)
			}
			if tt.want == http.StatusUnauthorized && rr.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate header on 401")
			}
		})
	}
}
