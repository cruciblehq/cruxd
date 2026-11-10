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
// Dispatches image operations to the runtime and container operations to the
// live container (looked up from the registry). build is reported during the
// handshake.
type Agent struct {
	mux        *http.ServeMux  // HTTP request multiplexer for routing endpoints.
	runtime    compute.Runtime // Backing runtime for image and container operations.
	build      Build           // Build identity reported during handshake.
	containers *registry       // Registry for managing live containers.
}

// Creates an agent serving the compute contract backed by runtime.
//
// runtime backs the image and container endpoints and must be non-nil. build
// is reported during the handshake.
func New(runtime compute.Runtime, build Build) *Agent {
	a := &Agent{
		mux:        http.NewServeMux(),
		runtime:    runtime,
		build:      build,
		containers: newRegistry(),
	}

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
func (a *Agent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mux.ServeHTTP(w, r)
}
