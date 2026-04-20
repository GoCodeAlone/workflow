package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBootstrapDOSpacesBucket_AlreadyExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "unexpected method", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	if err := bootstrapDOSpacesBucketAt(context.Background(), "my-bucket", "nyc3", srv.URL); err != nil {
		t.Fatalf("expected no error when bucket already exists, got: %v", err)
	}
}

func TestBootstrapDOSpacesBucket_CreatesNew(t *testing.T) {
	var postCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			postCalled = true
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "unexpected: "+r.Method, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	if err := bootstrapDOSpacesBucketAt(context.Background(), "my-bucket", "nyc3", srv.URL); err != nil {
		t.Fatalf("expected no error creating new bucket, got: %v", err)
	}
	if !postCalled {
		t.Error("expected POST to be called when bucket does not exist")
	}
}

func TestBootstrapDOSpacesBucket_CreateErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"id":"unprocessable_entity","message":"region is invalid"}`)) //nolint:errcheck
		default:
			http.Error(w, "unexpected: "+r.Method, http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	t.Setenv("DIGITALOCEAN_TOKEN", "test-token")
	err := bootstrapDOSpacesBucketAt(context.Background(), "bad-bucket", "invalid-region", srv.URL)
	if err == nil {
		t.Fatal("expected error on 4xx create response")
	}
	if !strings.Contains(err.Error(), "region is invalid") {
		t.Errorf("expected response body in error, got: %v", err)
	}
}

func TestBootstrapDOSpacesBucket_MissingToken(t *testing.T) {
	t.Setenv("DIGITALOCEAN_TOKEN", "")
	err := bootstrapDOSpacesBucketAt(context.Background(), "my-bucket", "nyc3", "http://unused")
	if err == nil {
		t.Fatal("expected error when DIGITALOCEAN_TOKEN unset")
	}
	if !strings.Contains(err.Error(), "DIGITALOCEAN_TOKEN") {
		t.Errorf("expected DIGITALOCEAN_TOKEN in error, got: %v", err)
	}
}
