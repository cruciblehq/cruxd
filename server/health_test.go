package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/agent"
)

func TestHealth(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointHealth), agent.MediaTypeHealthRequest, agent.HealthRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHealthRuntimeUnavailable(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{err: errors.New("containerd is down")}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointHealth), agent.MediaTypeHealthRequest, agent.HealthRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}
