package agent

import (
	"context"
	"io"
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/crypto"
)

// Number of random bytes in a generated container handle.
const handleLen = 16

// Looks up a live container by handle.
//
// When no container can be returned, writes the failure response and returns
// an error. An empty id is a bad request. An unknown id is a not-found
// condition.
func (a *Agent) lookup(w http.ResponseWriter, id compute.ContainerID) (compute.Container, error) {
	if id == "" {
		_ = a.failOp(w, compute.ErrorCodeBadRequest, errMissingContainerID.Error())
		return nil, errMissingContainerID
	}
	c, ok := a.containers.get(id)
	if !ok {
		err := crex.Newf(errContainerNotFound, "id %q", id)
		_ = a.failOp(w, compute.ErrorCodeNotFound, err.Error())
		return nil, err
	}
	return c, nil
}

// Creates a container from an image and registers it under a new handle.
func (a *Agent) load(w http.ResponseWriter, r *http.Request) {
	var req compute.LoadRequest
	if err := a.decode(r, compute.MediaTypeLoadRequest, &req); err != nil {
		http.Error(w, "invalid load request", http.StatusBadRequest)
		return
	}
	c, err := a.runtime.Load(r.Context(), req.Ref, req.Options)
	if err != nil {
		_ = a.failErr(w, err)
		return
	}
	id := compute.ContainerID(crypto.RandHex(handleLen))
	a.containers.add(id, c)
	if err := a.encode(w, compute.MediaTypeLoadResponse, http.StatusOK, compute.LoadResponse{ID: id}); err != nil {
		// The client never learned the handle, so nothing can reach the container.
		a.containers.remove(id)
		_ = c.Destroy(context.WithoutCancel(r.Context()))
	}
}

// Starts the container's main process.
func (a *Agent) start(w http.ResponseWriter, r *http.Request) {
	var req compute.StartRequest
	if err := a.decode(r, compute.MediaTypeStartRequest, &req); err != nil {
		http.Error(w, "invalid start request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Start(r.Context()); err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.ok(w)
}

// Stops the container's main process.
func (a *Agent) stop(w http.ResponseWriter, r *http.Request) {
	var req compute.StopRequest
	if err := a.decode(r, compute.MediaTypeStopRequest, &req); err != nil {
		http.Error(w, "invalid stop request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Stop(r.Context()); err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.ok(w)
}

// Applies a tar archive (request body) to the container's writable snapshot.
//
// The archive is expected as the request body and the target container handle
// is expected in the "id" query parameter.
func (a *Agent) copyLayer(w http.ResponseWriter, r *http.Request) {
	id := compute.ContainerID(r.URL.Query().Get(compute.QueryParamID))
	c, err := a.lookup(w, id)
	if err != nil {
		return
	}
	if err := c.Copy(r.Context(), r.Body); err != nil {
		// Drain the unread body so the error response is not lost to a connection reset.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = a.failErr(w, err)
		return
	}
	_ = a.ok(w)
}

// Commits the container's current state as a new image.
func (a *Agent) commit(w http.ResponseWriter, r *http.Request) {
	var req compute.CommitRequest
	if err := a.decode(r, compute.MediaTypeCommitRequest, &req); err != nil {
		http.Error(w, "invalid commit request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}
	ref, err := c.Commit(r.Context(), req.By)
	if err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.encode(w, compute.MediaTypeCommitResponse, http.StatusOK, compute.CommitResponse{Ref: ref})
}

// Stores the pending configuration applied at the next commit.
func (a *Agent) configure(w http.ResponseWriter, r *http.Request) {
	var req compute.ConfigureRequest
	if err := a.decode(r, compute.MediaTypeConfigureRequest, &req); err != nil {
		http.Error(w, "invalid configure request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Configure(r.Context(), req.Config); err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.ok(w)
}

// Returns the container's current OCI config.
func (a *Agent) inspectContainer(w http.ResponseWriter, r *http.Request) {
	var req compute.InspectContainerRequest
	if err := a.decode(r, compute.MediaTypeInspectContainerRequest, &req); err != nil {
		http.Error(w, "invalid inspect request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}
	cfg, err := c.Inspect(r.Context())
	if err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.encode(w, compute.MediaTypeInspectContainerResponse, http.StatusOK, compute.InspectContainerResponse{Config: cfg})
}

// Releases the container and removes it from the registry.
func (a *Agent) destroy(w http.ResponseWriter, r *http.Request) {
	var req compute.DestroyRequest
	if err := a.decode(r, compute.MediaTypeDestroyRequest, &req); err != nil {
		http.Error(w, "invalid destroy request", http.StatusBadRequest)
		return
	}
	c, err := a.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Destroy(r.Context()); err != nil {
		_ = a.failErr(w, err)
		return
	}
	a.containers.remove(req.ID)
	_ = a.ok(w)
}
