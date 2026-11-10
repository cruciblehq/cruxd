package agent

import (
	"io"
	"net/http"

	"github.com/cruciblehq/spec/compute"
)

// Imports an OCI archive carried in the request body into the image store.
func (a *Agent) importImage(w http.ResponseWriter, r *http.Request) {
	ref, err := a.runtime.Import(r.Context(), r.Body)
	if err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.encode(w, compute.MediaTypeImportResponse, http.StatusOK, compute.ImportResponse{Ref: ref})
}

// Exports the named image as an OCI archive written to the response body.
func (a *Agent) exportImage(w http.ResponseWriter, r *http.Request) {
	var req compute.ExportRequest
	if err := a.decode(r, compute.MediaTypeExportRequest, &req); err != nil {
		http.Error(w, "invalid export request", http.StatusBadRequest)
		return
	}
	w.Header().Set(compute.HeaderContentType, compute.MediaTypeImageArchive.String())
	if err := a.runtime.Export(r.Context(), req.Ref, w); err != nil {
		// If nothing was written yet this sets a status; otherwise it's a no-op.
		_ = a.failErr(w, err)
	}
}

// Extracts a path from the named image as a tar stream in the response body.
func (a *Agent) extractImage(w http.ResponseWriter, r *http.Request) {
	var req compute.ExtractRequest
	if err := a.decode(r, compute.MediaTypeExtractRequest, &req); err != nil {
		http.Error(w, "invalid extract request", http.StatusBadRequest)
		return
	}
	rc, err := a.runtime.Extract(r.Context(), req.Ref, req.Path)
	if err != nil {
		_ = a.failErr(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set(compute.HeaderContentType, compute.MediaTypeFileTree.String())
	_, _ = io.Copy(w, rc)
}

// Returns the OCI config of the named image.
func (a *Agent) inspectImage(w http.ResponseWriter, r *http.Request) {
	var req compute.InspectImageRequest
	if err := a.decode(r, compute.MediaTypeInspectImageRequest, &req); err != nil {
		http.Error(w, "invalid inspect request", http.StatusBadRequest)
		return
	}
	cfg, err := a.runtime.Inspect(r.Context(), req.Ref)
	if err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.encode(w, compute.MediaTypeInspectImageResponse, http.StatusOK, compute.InspectImageResponse{Config: cfg})
}

// Removes the named image from the store.
func (a *Agent) removeImage(w http.ResponseWriter, r *http.Request) {
	var req compute.RemoveRequest
	if err := a.decode(r, compute.MediaTypeRemoveRequest, &req); err != nil {
		http.Error(w, "invalid remove request", http.StatusBadRequest)
		return
	}
	if err := a.runtime.Remove(r.Context(), req.Ref); err != nil {
		_ = a.failErr(w, err)
		return
	}
	_ = a.ok(w)
}
