package server

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/httpx"
)

func TestEnroll(t *testing.T) {
	srv := httptest.NewTLSServer(New(&fakeAgent{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := srv.Client().Post(fmt.Sprintf("%s%s", srv.URL, agent.EndpointEnroll), agent.MediaTypeEnrollRequest.With(agent.BodyFormat), strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var er agent.EnrollResponse
	decodeBody(t, resp, &er)
	if !bytes.Equal(er.Request, []byte("request")) {
		t.Errorf("Request = %q, want %q", er.Request, "request")
	}
	if er.Proof.Type != trust.ProofToken {
		t.Errorf("Proof.Type = %q, want %q", er.Proof.Type, trust.ProofToken)
	}
}

func TestEnrollWithoutTLS(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, agent.EndpointEnroll), agent.MediaTypeEnrollRequest.With(agent.BodyFormat), strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestEnrollProofFails(t *testing.T) {
	srv := httptest.NewTLSServer(New(&fakeAgent{}, &fakeIdentity{unenrolled: true, err: errors.New("no token")}, Build{}))
	defer srv.Close()

	resp, err := srv.Client().Post(fmt.Sprintf("%s%s", srv.URL, agent.EndpointEnroll), agent.MediaTypeEnrollRequest.With(agent.BodyFormat), strings.NewReader("{}"))
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
	srv := httptest.NewServer(New(&fakeAgent{}, id, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointCertify), agent.MediaTypeCertifyRequest, agent.CertifyRequest{Certificate: []byte("cert")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct, want := resp.Header.Get(httpx.HeaderContentType), agent.MediaTypeCertifyResponse.With(agent.BodyFormat); ct != want {
		t.Errorf("Content-Type = %q, want %q", ct, want)
	}
	if !bytes.Equal(id.installed, []byte("cert")) {
		t.Errorf("installed = %q, want %q", id.installed, "cert")
	}
}

func TestCertifyRejected(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, &fakeIdentity{unenrolled: true, err: errors.New("bad certificate")}, Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointCertify), agent.MediaTypeCertifyRequest, agent.CertifyRequest{Certificate: []byte("cert")})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
	var opErr agent.Error
	decodeBody(t, resp, &opErr)
	if opErr.Code != agent.ErrorCodeBadRequest || opErr.Message != "bad certificate" {
		t.Errorf("error = %+v, want bad_request with the install failure", opErr)
	}
}

func TestCertifyBadRequest(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, &fakeIdentity{unenrolled: true}, Build{}))
	defer srv.Close()

	resp, err := http.Post(fmt.Sprintf("%s%s", srv.URL, agent.EndpointCertify), agent.MediaTypeCertifyRequest.With(agent.BodyFormat), bytes.NewReader([]byte("{bad")))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
