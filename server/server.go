package server

import (
	"fmt"
	"net/http"

	"github.com/cruciblehq/spec/agent"
)

// HTTP handler serving the agent protocol.
//
// Until the agent is enrolled, only the enrollment endpoints are served.
// After that, image operations go to the agent and container operations to
// the live container (looked up from the registry). build is reported during
// the handshake.
type Server struct {
	mux        *http.ServeMux // HTTP request multiplexer for routing endpoints.
	agent      agent.Agent    // Agent that runs image and container operations.
	identity   Identity       // Agent identity enrolled through the server.
	build      Build          // Build identity reported during handshake.
	containers *registry      // Live containers by handle.
}

// Creates a server for the agent protocol backed by a.
//
// a backs the image and container endpoints and identity the enrollment
// endpoints. Both must be non-nil. build is reported during the handshake.
func New(a agent.Agent, identity Identity, build Build) *Server {
	s := &Server{
		mux:        http.NewServeMux(),
		agent:      a,
		identity:   identity,
		build:      build,
		containers: newRegistry(),
	}

	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointEnroll), s.enroll)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointCertify), s.certify)

	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointHealth), s.health)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointHandshake), s.handshake)

	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointImport), s.importImage)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointExport), s.exportImage)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointExtract), s.extractImage)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointInspect), s.inspectImage)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointRemove), s.removeImage)

	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointLoad), s.load)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointStart), s.start)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointStop), s.stop)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointExec), s.exec)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointCopy), s.copyLayer)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointCommit), s.commit)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointConfig), s.configure)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointInspectContainer), s.inspectContainer)
	s.mux.HandleFunc(fmt.Sprintf("POST %s", agent.EndpointDestroy), s.destroy)

	return s
}

// Serves an HTTP request by routing it to the matching endpoint.
//
// Before enrollment, every endpoint except enrollment answers 403. After
// enrollment, the enrollment endpoints answer 409.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	enrolled := s.identity.Enrolled()
	switch {
	case !enrolled && !enrollmentPath(r.URL.Path):
		http.Error(w, "agent is not enrolled", http.StatusForbidden)
	case enrolled && enrollmentPath(r.URL.Path):
		http.Error(w, "agent is already enrolled", http.StatusConflict)
	default:
		s.mux.ServeHTTP(w, r)
	}
}
