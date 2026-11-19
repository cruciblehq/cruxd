package server

import (
	"crypto/tls"
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/httpx"
)

// Agent identity the server enrolls and keeps up to date.
type Identity interface {
	Enrolled() bool                                       // Whether an agent certificate is installed.
	Proof(state tls.ConnectionState) (trust.Proof, error) // Token proof bound to the TLS session.
	Request() ([]byte, error)                             // DER certificate request for the agent key.
	Install(der []byte) error                             // Installs the agent certificate.
	UpdateRevocations(der []byte) error                   // Stores a newer revocation list.
}

// Returns the agent's certificate request and token proof.
//
// The proof is bound to the TLS session the request arrived on, so crux can
// tell it came from the agent holding the key it sees in TLS.
func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	var req agent.EnrollRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeEnrollRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid enroll request")
		return
	}
	if r.TLS == nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "enrollment requires TLS")
		return
	}
	proof, err := s.identity.Proof(*r.TLS)
	if err != nil {
		_ = s.failOp(w, agent.ErrorCodeInternalError, err.Error())
		return
	}
	request, err := s.identity.Request()
	if err != nil {
		_ = s.failOp(w, agent.ErrorCodeInternalError, err.Error())
		return
	}
	_ = httpx.Write(w, agent.MediaTypeEnrollResponse, agent.BodyFormat, http.StatusOK, agent.EnrollResponse{
		Request: request,
		Proof:   proof,
	})
}

// Installs the agent certificate crux signed.
func (s *Server) certify(w http.ResponseWriter, r *http.Request) {
	var req agent.CertifyRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeCertifyRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid certify request")
		return
	}
	if err := s.identity.Install(req.Certificate); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, err.Error())
		return
	}
	_ = httpx.Write(w, agent.MediaTypeCertifyResponse, agent.BodyFormat, http.StatusOK, agent.CertifyResponse{})
}

// Whether path is served before the agent is enrolled.
func enrollmentPath(path string) bool {
	return path == agent.EndpointEnroll || path == agent.EndpointCertify
}
