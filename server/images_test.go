package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/agent"
)

func TestImageOps(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := postData(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointImport), agent.MediaTypeImportRequest, agent.ImportRequest{}, agent.MediaTypeImageArchive, "archive")
	var ir agent.ImportResponse
	decodeBody(t, resp, &ir)
	if ir.Ref != "img:imported" {
		t.Errorf("import ref = %q", ir.Ref)
	}

	resp = post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointInspect), agent.MediaTypeInspectImageRequest,
		agent.InspectImageRequest{Ref: "img:imported"})
	var iresp agent.InspectImageResponse
	decodeBody(t, resp, &iresp)
	if iresp.Config.User != "image" {
		t.Errorf("inspect user = %q, want %q", iresp.Config.User, "image")
	}
}

func TestExport(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExport), agent.MediaTypeExportRequest,
		agent.ExportRequest{Ref: "img:x"})
	var er agent.ExportResponse
	if data := readData(t, resp, agent.MediaTypeExportResponse, &er, agent.MediaTypeImageArchive); data != "archive" {
		t.Errorf("data = %q, want %q", data, "archive")
	}
}

func TestExtract(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExtract), agent.MediaTypeExtractRequest,
		agent.ExtractRequest{Ref: "img:x", Path: "/p"})
	var er agent.ExtractResponse
	if data := readData(t, resp, agent.MediaTypeExtractResponse, &er, agent.MediaTypeFileTree); data != "tar" {
		t.Errorf("data = %q, want %q", data, "tar")
	}
}

func TestRemove(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointRemove), agent.MediaTypeRemoveRequest,
		agent.RemoveRequest{Ref: "img:x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestImportError(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{err: errors.New("boom")}, enrolled(), Build{}))
	defer srv.Close()

	resp := postData(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointImport), agent.MediaTypeImportRequest, agent.ImportRequest{}, agent.MediaTypeImageArchive, "x")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}
