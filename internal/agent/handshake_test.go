package agent

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

func TestHandshake(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{Version: "1.2.3", Commit: "abc123"}))
	defer srv.Close()

	body, err := codec.Encode(compute.HandshakeRequest{Version: compute.Version}, codec.JSON)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	resp, err := http.Post(
		srv.URL+compute.EndpointHandshake,
		compute.MediaTypeHandshakeRequest.JSON(),
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("post handshake: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var hr compute.HandshakeResponse
	if err := codec.Unmarshal(data, &hr, codec.JSON); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if hr.Version != compute.Version {
		t.Errorf("Version = %q, want %q", hr.Version, compute.Version)
	}
	if hr.Build != "1.2.3" {
		t.Errorf("Build = %q, want %q", hr.Build, "1.2.3")
	}
	if hr.Commit != "abc123" {
		t.Errorf("Commit = %q, want %q", hr.Commit, "abc123")
	}
}

func TestHandshakeBadRequest(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+compute.EndpointHandshake, compute.MediaTypeHandshakeRequest.JSON(), bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
