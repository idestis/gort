package gort

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListScripts(t *testing.T) {
	req, err := http.NewRequest("GET", "/v1/list-dist", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(ListScriptsHandler)
	handler.ServeHTTP(rr, req)
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}

	expected := "./dist seems like empty"
	if rr.Body.String() != expected {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}

func TestNotFound(t *testing.T) {
	req, _ := http.NewRequest("GET", "/any-url", nil)
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(NotFoundHandler)
	handler.ServeHTTP(rr, req)
	expected := "This page does not exist!"
	if rr.Body.String() != expected && rr.Code != http.StatusNotFound {
		t.Errorf("handler returned unexpected body: got %v want %v",
			rr.Body.String(), expected)
	}
}
