package agent

import (
	"fmt"
	"net/http"

	"github.com/cruciblehq/spec/compute"
)

// Build identity reported during handshake.
type Build struct {
	Version string // cruxd build version.
	Commit  string // cruxd git commit.
}

// HTTP handler serving the compute contract.
//
// Until the machine is enrolled, only the enrollment endpoints are served.
// After that, image operations go to the runtime and container operations to
// the live container (looked up from the registry). build is reported during
// the handshake.
type Agent struct {
	mux        *http.ServeMux  // HTTP request multiplexer for routing endpoints.
	runtime    compute.Runtime // Backing runtime for image and container operations.
	identity   Identity        // Machine identity enrolled through the agent.
	build      Build           // Build identity reported during handshake.
	containers *registry       // Registry for managing live containers.
}

// Creates an agent serving the compute contract backed by runtime.
//
// runtime backs the image and container endpoints and identity the enrollment
// endpoints. Both must be non-nil. build is reported during the handshake.
func New(runtime compute.Runtime, identity Identity, build Build) *Agent {
	a := &Agent{
		mux:        http.NewServeMux(),
		runtime:    runtime,
		identity:   identity,
		build:      build,
		containers: newRegistry(),
	}

	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointEnroll), a.enroll)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointCertify), a.certify)

	a.mux.HandleFunc(fmt.Sprintf("GET %s", compute.EndpointHealth), a.health)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointHandshake), a.handshake)

	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointImport), a.importImage)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointExport), a.exportImage)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointExtract), a.extractImage)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointInspect), a.inspectImage)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointRemove), a.removeImage)

	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointLoad), a.load)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointStart), a.start)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointStop), a.stop)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointExec), a.exec)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointCopy), a.copyLayer)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointCommit), a.commit)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointConfig), a.configure)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointInspectContainer), a.inspectContainer)
	a.mux.HandleFunc(fmt.Sprintf("POST %s", compute.EndpointDestroy), a.destroy)

	return a
}

// Serves an HTTP request by routing it to the matching endpoint.
//
// Before enrollment, every endpoint except enrollment answers 403. After
// enrollment, the enrollment endpoints answer 409.
func (a *Agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	enrolled := a.identity.Enrolled()
	switch {
	case !enrolled && !enrollmentPath(r.URL.Path):
		http.Error(w, "machine is not enrolled", http.StatusForbidden)
	case enrolled && enrollmentPath(r.URL.Path):
		http.Error(w, "machine is already enrolled", http.StatusConflict)
	default:
		a.mux.ServeHTTP(w, r)
	}
}
