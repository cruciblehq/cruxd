package server

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/codec"
)

func TestHandshake(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{Version: "1.2.3", Commit: "abc123"}))
	defer srv.Close()

	body, err := codec.Encode(agent.HandshakeRequest{Version: agent.Version}, agent.BodyFormat)
	if err != nil {
		t.Fatalf("encode request: %v", err)
	}

	resp, err := http.Post(
		fmt.Sprintf("%s%s", srv.URL, agent.EndpointHandshake),
		agent.MediaTypeHandshakeRequest.With(agent.BodyFormat),
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
	var hr agent.HandshakeResponse
	if err := codec.Decode(data, &hr, agent.BodyFormat); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if hr.Version != agent.Version {
		t.Errorf("Version = %q, want %q", hr.Version, agent.Version)
	}
	if hr.Build != "1.2.3" {
		t.Errorf("Build = %q, want %q", hr.Build, "1.2.3")
	}
	if hr.Commit != "abc123" {
		t.Errorf("Commit = %q, want %q", hr.Commit, "abc123")
	}
}

func TestHandshakeBadRequest(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, agent.EndpointHandshake), agent.MediaTypeHandshakeRequest.With(agent.BodyFormat), bytes.NewReader([]byte("{bad")))
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
	srv := httptest.NewServer(New(&fakeAgent{}, id, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointHandshake), agent.MediaTypeHandshakeRequest, agent.HandshakeRequest{Version: agent.Version, Revocations: []byte("crl")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !bytes.Equal(id.revocations, []byte("crl")) {
		t.Errorf("revocations = %q, want %q", id.revocations, "crl")
	}
}

func TestHandshakeBadRevocations(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, &fakeIdentity{err: errors.New("bad list")}, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointHandshake), agent.MediaTypeHandshakeRequest, agent.HandshakeRequest{Version: agent.Version, Revocations: []byte("crl")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
