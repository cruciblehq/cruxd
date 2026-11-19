package server

import (
	"io"
	"net/http"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/httpx"
	"github.com/cruciblehq/utils-go/mediatype"
)

// Imports the OCI archive in the data part of the request into the image store.
func (s *Server) importImage(w http.ResponseWriter, r *http.Request) {
	var req agent.ImportRequest
	archive, err := httpx.ReadMultipart(r.Header.Get(httpx.HeaderContentType), r.Body, agent.MediaTypeImportRequest, agent.BodyFormat, &req, agent.MediaTypeImageArchive)
	if err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid import request")
		return
	}
	ref, err := s.agent.Import(r.Context(), archive)
	if err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeImportResponse, agent.BodyFormat, http.StatusOK, agent.ImportResponse{Ref: ref})
}

// Exports the named image as an OCI archive in the data part of the response.
func (s *Server) exportImage(w http.ResponseWriter, r *http.Request) {
	var req agent.ExportRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeExportRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid export request")
		return
	}
	archive, pw := io.Pipe()
	defer archive.Close()
	go func() {
		pw.CloseWithError(s.agent.Export(r.Context(), req.Ref, pw))
	}()
	s.writeData(w, agent.MediaTypeExportResponse, agent.ExportResponse{}, agent.MediaTypeImageArchive, archive)
}

// Extracts a path from the named image as a tar stream in the data part of the response.
func (s *Server) extractImage(w http.ResponseWriter, r *http.Request) {
	var req agent.ExtractRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeExtractRequest, agent.BodyFormat, &req); err != nil {
		_ = s.failOp(w, agent.ErrorCodeBadRequest, "invalid extract request")
		return
	}
	rc, err := s.agent.Extract(r.Context(), req.Ref, req.Path)
	if err != nil {
		_ = s.failErr(w, err)
		return
	}
	defer rc.Close()
	s.writeData(w, agent.MediaTypeExtractResponse, agent.ExtractResponse{}, agent.MediaTypeFileTree, rc)
}

// Returns the OCI config of the named image.
func (s *Server) inspectImage(w http.ResponseWriter, r *http.Request) {
	var req agent.InspectImageRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeInspectImageRequest, agent.BodyFormat, &req); err != nil {
		http.Error(w, "invalid inspect request", http.StatusBadRequest)
		return
	}
	cfg, err := s.agent.Inspect(r.Context(), req.Ref)
	if err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeInspectImageResponse, agent.BodyFormat, http.StatusOK, agent.InspectImageResponse{Config: cfg})
}

// Removes the named image from the store.
func (s *Server) removeImage(w http.ResponseWriter, r *http.Request) {
	var req agent.RemoveRequest
	if err := httpx.DecodeRequest(r, agent.MediaTypeRemoveRequest, agent.BodyFormat, &req); err != nil {
		http.Error(w, "invalid remove request", http.StatusBadRequest)
		return
	}
	if err := s.agent.Remove(r.Context(), req.Ref); err != nil {
		_ = s.failErr(w, err)
		return
	}
	_ = httpx.Write(w, agent.MediaTypeRemoveResponse, agent.BodyFormat, http.StatusOK, agent.RemoveResponse{})
}

// Writes msg as the message part and data as the data part of the response.
//
// The message has media type mt and the data part dataType. When data fails
// before its first byte, the failure is written as an operation Error payload
// instead.
func (s *Server) writeData(w http.ResponseWriter, mt mediatype.MediaType, msg any, dataType mediatype.MediaType, data io.Reader) {
	mw := httpx.NewMultipartWriter(w, mt, agent.BodyFormat)
	w.Header().Set(httpx.HeaderContentType, mw.ContentType())
	if err := mw.Write(msg, dataType, data); err != nil {
		_ = s.failErr(w, err)
	}
}
