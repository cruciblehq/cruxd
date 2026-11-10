package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
)

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + compute.EndpointHealth)
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHealthRuntimeUnavailable(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{err: errors.New("containerd is down")}, Build{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + compute.EndpointHealth)
	if err != nil {
		t.Fatalf("get health: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
