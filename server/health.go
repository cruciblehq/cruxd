package server

import (
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/httpx"
)

// Answers the health check with 200 when the agent can serve requests.
//
// Writes a 503 with the failure when the agent cannot serve requests.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	var req agent.HealthRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeHealthRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid health request")
		return
	}
	if err := s.agent.Health(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeHealthResponse, agent.BodyFormat, http.StatusOK, agent.HealthResponse{})
}
