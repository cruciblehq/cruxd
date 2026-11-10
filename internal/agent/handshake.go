package agent

import (
	"net/http"

	"github.com/cruciblehq/spec/compute"
)

// Negotiates cruxd's protocol version and build identity.
//
// crux sends its supported protocol version; cruxd answers with its own version
// and build identity so each side can refuse an incompatible peer. A malformed
// request is a transport failure and gets a non-2xx status.
func (a *Agent) handshake(w http.ResponseWriter, r *http.Request) {
	var req compute.HandshakeRequest
	if err := a.decode(r, compute.MediaTypeHandshakeRequest, &req); err != nil {
		http.Error(w, "invalid handshake request", http.StatusBadRequest)
		return
	}

	resp := compute.HandshakeResponse{
		Version: compute.Version,
		Build:   a.build.Version,
		Commit:  a.build.Commit,
	}
	_ = a.encode(w, compute.MediaTypeHandshakeResponse, http.StatusOK, resp)
}
