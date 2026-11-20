package server

import (
	"context"
	"crypto/rand"
	"io"
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/crex"
	"github.com/cruciblehq/utils-go/httpx"
)

// Looks up a live container by handle.
//
// When no container can be returned, writes the failure response and returns
// an error. An empty id is a bad request. An unknown id is a not-found
// condition.
func (s *Server) lookup(w http.ResponseWriter, id agent.ContainerID) (agent.Container, error) {
	if id == "" {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, errMissingContainerID.Error())
		return nil, errMissingContainerID
	}
	c, ok := s.containers.get(id)
	if !ok {
		err := crex.Newf(errContainerNotFound, "id %q", id)
		_ = s.failOp(w, agent.ErrorCodeNotFound, err.Error())
		return nil, err
	}
	return c, nil
}

// Creates a container from an image and registers it under a new handle.
func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	var req agent.LoadRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeLoadRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid load request")
		return
	}
	c, err := s.agent.Load(r.Context(), req.Ref, req.Options)
	if err != nil {
		_ = s.failErr(w, err)
		return
	}
	id := agent.ContainerID(rand.Text())
	s.containers.add(id, c)
	if err := httpx.Write(w, agent.MediaTypeLoadResponse, agent.BodyFormat, http.StatusOK, agent.LoadResponse{ID: id}); err != nil {
		// The client never learned the handle, so nothing can reach the container.
		s.containers.remove(id)
		_ = c.Destroy(context.WithoutCancel(r.Context()))
	}
}

// Starts the container's main process.
func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	var req agent.StartRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeStartRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid start request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Start(r.Context()); err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeStartResponse, agent.BodyFormat, http.StatusOK, agent.StartResponse{})
}

// Stops the container's main process.
func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	var req agent.StopRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeStopRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid stop request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Stop(r.Context()); err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeStopResponse, agent.BodyFormat, http.StatusOK, agent.StopResponse{})
}

// Applies the tar archive in the data part of the request to the container's
// writable snapshot.
func (s *Server) copyLayer(w http.ResponseWriter, r *http.Request) {
	var req agent.CopyRequest
	layer, err := httpx.ReadMultipart(r.Header.Get(httpx.HeaderContentType), r.Body, agent.MediaTypeCopyRequest, agent.BodyFormat, &req, agent.MediaTypeLayer)
	if err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid copy request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Copy(r.Context(), layer); err != nil {
		// Drain the unread body so the error response is not lost to a connection reset.
		_, _ = io.Copy(io.Discard, r.Body)
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeCopyResponse, agent.BodyFormat, http.StatusOK, agent.CopyResponse{})
}

// Commits the container's current state as a new image.
func (s *Server) commit(w http.ResponseWriter, r *http.Request) {
	var req agent.CommitRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeCommitRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid commit request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	ref, err := c.Commit(r.Context(), req.By)
	if err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeCommitResponse, agent.BodyFormat, http.StatusOK, agent.CommitResponse{Ref: ref})
}

// Stores the pending configuration applied at the next commit.
func (s *Server) configure(w http.ResponseWriter, r *http.Request) {
	var req agent.ConfigureRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeConfigureRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid configure request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Configure(r.Context(), req.Config); err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeConfigureResponse, agent.BodyFormat, http.StatusOK, agent.ConfigureResponse{})
}

// Returns the container's current OCI config.
func (s *Server) inspectContainer(w http.ResponseWriter, r *http.Request) {
	var req agent.InspectContainerRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeInspectContainerRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid inspect request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	cfg, err := c.Inspect(r.Context())
	if err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeInspectContainerResponse, agent.BodyFormat, http.StatusOK, agent.InspectContainerResponse{Config: cfg})
}

// Releases the container and removes it from the registry.
func (s *Server) destroy(w http.ResponseWriter, r *http.Request) {
	var req agent.DestroyRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeDestroyRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid destroy request")
		return
	}
	c, err := s.lookup(w, req.ID)
	if err != nil {
		return
	}
	if err := c.Destroy(r.Context()); err != nil {
		_ = s.failErr(w, err)
		return
	}
	s.containers.remove(req.ID)
	_ = httpx.Write(w, agent.MediaTypeDestroyResponse, agent.BodyFormat, http.StatusOK, agent.DestroyResponse{})
}
