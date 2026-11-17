package agent

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

func TestHandshake(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, enrolled(), Build{Version: "1.2.3", Commit: "abc123"}))
	defer srv.Close()

	body, err := codec.Encode(compute.HandshakeRequest{Version: compute.Version}, compute.BodyFormat)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("%s%s", srv.URL, compute.EndpointHandshake),
		compute.MediaTypeHandshakeRequest.With(compute.BodyFormat),
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
	if err := codec.Decode(data, &hr, compute.BodyFormat); err != nil {
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
	srv := httptest.NewServer(New(&fakeRuntime{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, compute.EndpointHandshake), compute.MediaTypeHandshakeRequest.With(compute.BodyFormat), bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandshakeRevocations(t *testing.T) {
	id := enrolled()
	srv := httptest.NewServer(New(&fakeRuntime{}, id, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, compute.EndpointHandshake), compute.MediaTypeHandshakeRequest, compute.HandshakeRequest{Version: compute.Version, Revocations: []byte("crl")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !bytes.Equal(id.revocations, []byte("crl")) {
		t.Errorf("revocations = %q, want %q", id.revocations, "crl")
	}
}

func TestHandshakeBadRevocations(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, &fakeIdentity{err: errors.New("bad list")}, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, compute.EndpointHandshake), compute.MediaTypeHandshakeRequest, compute.HandshakeRequest{Version: compute.Version, Revocations: []byte("crl")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
