package server

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/spec/trust"
)

type fakeAgent struct {
	ctr *fakeContainer
	err error
}

func (f *fakeAgent) Import(context.Context, io.Reader) (agent.ImageRef, error) {
	if f.err != nil {
		return "", f.err
	}
	return "img:imported", nil
}

func (f *fakeAgent) Export(_ context.Context, _ agent.ImageRef, w io.Writer) error {
	if f.err != nil {
		return f.err
	}
	_, err := w.Write([]byte("archive"))
	return err
}

func (f *fakeAgent) Extract(context.Context, agent.ImageRef, string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader([]byte("tar"))), nil
}

func (f *fakeAgent) Inspect(context.Context, agent.ImageRef) (agent.ImageConfig, error) {
	if f.err != nil {
		return agent.ImageConfig{}, f.err
	}
	return agent.ImageConfig{User: "image"}, nil
}

func (f *fakeAgent) Remove(context.Context, agent.ImageRef) error {
	return f.err
}

func (f *fakeAgent) Load(context.Context, agent.ImageRef, agent.Options) (agent.Container, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.ctr == nil {
		f.ctr = &fakeContainer{output: "hello\n"}
	}
	return f.ctr, nil
}
func (f *fakeAgent) Health(context.Context) error {
	return f.err
}

func (f *fakeAgent) Close() error { return nil }

type fakeIdentity struct {
	unenrolled  bool
	installed   []byte
	revocations []byte
	err         error
}

func enrolled() *fakeIdentity {
	return &fakeIdentity{}
}

func (f *fakeIdentity) Enrolled() bool {
	return !f.unenrolled
}

func (f *fakeIdentity) Proof(tls.ConnectionState) (trust.Proof, error) {
	return trust.Proof{Type: trust.ProofToken, Value: []byte("proof")}, f.err
}

func (f *fakeIdentity) Request() ([]byte, error) {
	return []byte("request"), f.err
}

func (f *fakeIdentity) Install(der []byte) error {
	f.installed = der
	return f.err
}

func (f *fakeIdentity) UpdateRevocations(der []byte) error {
	f.revocations = der
	return f.err
}

func TestServeHTTPNotEnrolled(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointHealth), agent.MediaTypeHealthRequest, agent.HealthRequest{})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServeHTTPAlreadyEnrolled(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, agent.EndpointEnroll), "", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestServeHTTPUnknownPath(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s/does-not-exist", srv.URL))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestServeHTTPWrongMethod(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s%s", srv.URL, agent.EndpointLoad)) // Load is POST-only.
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
