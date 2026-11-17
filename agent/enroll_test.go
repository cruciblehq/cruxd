package agent

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/spec/trust"
)

func TestEnroll(t *testing.T) {
	srv := httptest.NewTLSServer(New(&fakeRuntime{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := srv.Client().Post(fmt.Sprintf("%s%s", srv.URL, compute.EndpointEnroll), "", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var er compute.EnrollResponse
	decodeBody(t, resp, &er)
	if !bytes.Equal(er.Request, []byte("request")) {
		t.Errorf("Request = %q, want %q", er.Request, "request")
	}
	if er.Proof.Type != trust.ProofToken {
		t.Errorf("Proof.Type = %q, want %q", er.Proof.Type, trust.ProofToken)
	}
}

func TestEnrollWithoutTLS(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, compute.EndpointEnroll), "", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestEnrollProofFails(t *testing.T) {
	srv := httptest.NewTLSServer(New(&fakeRuntime{}, &fakeIdentity{unenrolled: true, err: errors.New("no token")}, Build{}))
	defer srv.Close()

	resp, err := srv.Client().Post(fmt.Sprintf("%s%s", srv.URL, compute.EndpointEnroll), "", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

func TestCertify(t *testing.T) {
	id := &fakeIdentity{unenrolled: true}
	srv := httptest.NewServer(New(&fakeRuntime{}, id, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, compute.EndpointCertify), compute.MediaTypeCertifyRequest, compute.CertifyRequest{Certificate: []byte("cert")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if !bytes.Equal(id.installed, []byte("cert")) {
		t.Errorf("installed = %q, want %q", id.installed, "cert")
	}
}

func TestCertifyRejected(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, &fakeIdentity{unenrolled: true, err: errors.New("bad certificate")}, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, compute.EndpointCertify), compute.MediaTypeCertifyRequest, compute.CertifyRequest{Certificate: []byte("cert")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCertifyBadRequest(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, compute.EndpointCertify), compute.MediaTypeCertifyRequest.With(compute.BodyFormat), bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
