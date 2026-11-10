package agent

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
)

func TestImageOps(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+compute.EndpointImport, compute.MediaTypeImageArchive.String(), bytes.NewReader([]byte("archive")))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var ir compute.ImportResponse
	decodeBody(t, resp, &ir)
	if ir.Ref != "img:imported" {
		t.Errorf("import ref = %q", ir.Ref)
	}

	resp = post(t, srv.URL+compute.EndpointInspect, compute.MediaTypeInspectImageRequest,
		compute.InspectImageRequest{Ref: "img:imported"})
	var iresp compute.InspectImageResponse
	decodeBody(t, resp, &iresp)
	if iresp.Config.User != "image" {
		t.Errorf("inspect user = %q, want %q", iresp.Config.User, "image")
	}
}

func TestExport(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp := post(t, srv.URL+compute.EndpointExport, compute.MediaTypeExportRequest,
		compute.ExportRequest{Ref: "img:x"})
	defer resp.Body.Close()
	if ct := resp.Header.Get(compute.HeaderContentType); ct != compute.MediaTypeImageArchive.String() {
		t.Errorf("content-type = %q", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "archive" {
		t.Errorf("body = %q, want %q", data, "archive")
	}
}

func TestExtract(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp := post(t, srv.URL+compute.EndpointExtract, compute.MediaTypeExtractRequest,
		compute.ExtractRequest{Ref: "img:x", Path: "/p"})
	defer resp.Body.Close()
	if ct := resp.Header.Get(compute.HeaderContentType); ct != compute.MediaTypeFileTree.String() {
		t.Errorf("content-type = %q", ct)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "tar" {
		t.Errorf("body = %q, want %q", data, "tar")
	}
}

func TestRemove(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp := post(t, srv.URL+compute.EndpointRemove, compute.MediaTypeRemoveRequest,
		compute.RemoveRequest{Ref: "img:x"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestImportError(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{err: errors.New("boom")}, Build{}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+compute.EndpointImport, compute.MediaTypeImageArchive.String(), bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}
