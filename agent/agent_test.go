package agent

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/spec/trust"
)

type fakeRuntime struct {
	ctr *fakeContainer
	err error
}

func (f *fakeRuntime) Import(context.Context, io.Reader) (compute.ImageRef, error) {
	if f.err != nil {
		return "", f.err
	}
	return "img:imported", nil
}

func (f *fakeRuntime) Export(_ context.Context, _ compute.ImageRef, w io.Writer) error {
	if f.err != nil {
		return f.err
	}
	_, err := w.Write([]byte("archive"))
	return err
}

func (f *fakeRuntime) Extract(context.Context, compute.ImageRef, string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader([]byte("tar"))), nil
}

func (f *fakeRuntime) Inspect(context.Context, compute.ImageRef) (compute.ImageConfig, error) {
	if f.err != nil {
		return compute.ImageConfig{}, f.err
	}
	return compute.ImageConfig{User: "image"}, nil
}

func (f *fakeRuntime) Remove(context.Context, compute.ImageRef) error {
	return f.err
}

func (f *fakeRuntime) Load(context.Context, compute.ImageRef, compute.Options) (compute.Container, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.ctr == nil {
		f.ctr = &fakeContainer{output: "hello\n"}
	}
	return f.ctr, nil
}
func (f *fakeRuntime) Health(context.Context) error {
	return f.err
}

func (f *fakeRuntime) Close() error { return nil }

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
	srv := httptest.NewServer(New(&fakeRuntime{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s%s", srv.URL, compute.EndpointHealth))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
}

func TestServeHTTPAlreadyEnrolled(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, compute.EndpointEnroll), "", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestServeHTTPUnknownPath(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, enrolled(), Build{}))
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
	srv := httptest.NewServer(New(&fakeRuntime{}, enrolled(), Build{}))
	defer srv.Close()

	resp, err := http.Get(fmt.Sprintf("%s%s", srv.URL, compute.EndpointLoad)) // Load is POST-only.
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}
