package agent

import (
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/httpx"
)

// Negotiates cruxd's protocol version and build identity.
//
// crux sends its supported protocol version and its newest revocation list;
// cruxd stores the list if it is newer and answers with its own version and
// build identity so each side can refuse an incompatible peer. A malformed
// request or revocation list is a transport failure and gets a non-2xx status.
func (a *Agent) handshake(w http.ResponseWriter, r *http.Request) {
	var req compute.HandshakeRequest
	if err := httpx.DecodeRequest(r, compute.MediaTypeHandshakeRequest, compute.BodyFormat, &req); err != nil {
		http.Error(w, "invalid handshake request", http.StatusBadRequest)
		return
	}
	if len(req.Revocations) > 0 {
		if err := a.identity.UpdateRevocations(req.Revocations); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	resp := compute.HandshakeResponse{
		Version: compute.Version,
		Build:   a.build.Version,
		Commit:  a.build.Commit,
	}
	_ = httpx.Write(w, compute.MediaTypeHandshakeResponse, compute.BodyFormat, http.StatusOK, resp)
}
