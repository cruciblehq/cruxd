package agent

import (
	"crypto/tls"
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/spec/trust"
	"github.com/cruciblehq/utils-go/httpx"
)

// Machine identity the agent enrolls and keeps up to date.
type Identity interface {
	Enrolled() bool                                       // Whether a machine certificate is installed.
	Proof(state tls.ConnectionState) (trust.Proof, error) // Token proof bound to the TLS session.
	Request() ([]byte, error)                             // DER certificate request for the machine key.
	Install(der []byte) error                             // Installs the machine certificate.
	UpdateRevocations(der []byte) error                   // Stores a newer revocation list.
}

// Returns the machine's certificate request and token proof.
//
// The proof is bound to the TLS session the request arrived on, so crux can
// tell it came from the machine holding the key it sees in TLS.
func (a *Agent) enroll(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil {
		http.Error(w, "enrollment requires TLS", http.StatusBadRequest)
		return
	}
	proof, err := a.identity.Proof(*r.TLS)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	request, err := a.identity.Request()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = httpx.Write(w, compute.MediaTypeEnrollResponse, compute.BodyFormat, http.StatusOK, compute.EnrollResponse{
		Request: request,
		Proof:   proof,
	})
}

// Installs the machine certificate crux signed.
func (a *Agent) certify(w http.ResponseWriter, r *http.Request) {
	var req compute.CertifyRequest
	if err := httpx.DecodeRequest(r, compute.MediaTypeCertifyRequest, compute.BodyFormat, &req); err != nil {
		http.Error(w, "invalid certify request", http.StatusBadRequest)
		return
	}
	if err := a.identity.Install(req.Certificate); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = a.ok(w)
}

// Whether path is served before the machine is enrolled.
func enrollmentPath(path string) bool {
	return path == compute.EndpointEnroll || path == compute.EndpointCertify
}
