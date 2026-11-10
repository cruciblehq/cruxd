package agent

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

func TestDecode(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	body, err := codec.Encode(compute.StartRequest{ID: "c1"}, codec.JSON)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(compute.HeaderContentType, compute.MediaTypeStartRequest.JSON())

	var got compute.StartRequest
	if err := a.decode(req, compute.MediaTypeStartRequest, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "c1" {
		t.Errorf("ID = %q, want %q", got.ID, "c1")
	}
}

func TestDecodeWrongMediaType(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	body, err := codec.Encode(compute.StartRequest{ID: "c1"}, codec.JSON)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set(compute.HeaderContentType, compute.MediaTypeStopRequest.JSON())

	var got compute.StartRequest
	if err := a.decode(req, compute.MediaTypeStartRequest, &got); err == nil {
		t.Fatal("expected a media type error")
	}
}

func TestDecodeInvalid(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("{not json")))
	req.Header.Set(compute.HeaderContentType, compute.MediaTypeStartRequest.JSON())

	var got compute.StartRequest
	if err := a.decode(req, compute.MediaTypeStartRequest, &got); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestEncode(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	w := httptest.NewRecorder()

	a.encode(w, compute.MediaTypeLoadResponse, http.StatusCreated, compute.LoadResponse{ID: "c2"})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if ct := w.Header().Get(compute.HeaderContentType); ct != compute.MediaTypeLoadResponse.JSON() {
		t.Errorf("content-type = %q, want %q", ct, compute.MediaTypeLoadResponse.JSON())
	}
	var lr compute.LoadResponse
	if err := codec.Unmarshal(w.Body.Bytes(), &lr, codec.JSON); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if lr.ID != "c2" {
		t.Errorf("ID = %q, want %q", lr.ID, "c2")
	}
}
