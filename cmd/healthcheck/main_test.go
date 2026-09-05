package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	if err := run(server.URL); err != nil {
		t.Fatal(err)
	}
}

func TestRunRejectsInvalidEndpointAndStatus(t *testing.T) {
	if err := run(""); err == nil {
		t.Fatal("run() error = nil for empty endpoint")
	}
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	if err := run(server.URL); err == nil {
		t.Fatal("run() error = nil for non-2xx status")
	}
}
