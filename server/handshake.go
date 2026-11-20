package server

import (
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/httpx"
)

// Exchanges protocol versions and build identities with crux.
//
// crux sends its supported protocol version and its newest revocation list.
// The agent stores the list if it is newer and answers with its own version and
// build identity so each side can refuse an incompatible peer. A malformed
// request or revocation list gets a bad request error.
func (s *Server) handshake(w http.ResponseWriter, r *http.Request) {
	var req agent.HandshakeRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeHandshakeRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid handshake request")
		return
	}
	if len(req.Revocations) > 0 {
		if err := s.identity.UpdateRevocations(req.Revocations); err != nil {
			_ = s.failOp(w, agent.ErrorCodeBadRequest, err.Error())
			return
		}
	}

	resp := agent.HandshakeResponse{
		Version: agent.Version,
		Build:   s.build.Version,
		Commit:  s.build.Commit,
	}
	_ = httpx.Write(w, agent.MediaTypeHandshakeResponse, agent.BodyFormat, http.StatusOK, resp)
}
