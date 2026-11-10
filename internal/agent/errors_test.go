package agent

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

func TestClassifyNotFound(t *testing.T) {
	code, ok := classify(compute.ErrNotFound)
	if !ok || code != compute.ErrorCodeNotFound {
		t.Errorf("classify = (%q, %v), want (%q, true)", code, ok, compute.ErrorCodeNotFound)
	}
}

func TestClassifyAlreadyStarted(t *testing.T) {
	code, ok := classify(compute.ErrAlreadyStarted)
	if !ok || code != compute.ErrorCodeConflict {
		t.Errorf("classify = (%q, %v), want (%q, true)", code, ok, compute.ErrorCodeConflict)
	}
}

func TestClassifyNotStarted(t *testing.T) {
	code, ok := classify(compute.ErrNotStarted)
	if !ok || code != compute.ErrorCodePreconditionFailed {
		t.Errorf("classify = (%q, %v), want (%q, true)", code, ok, compute.ErrorCodePreconditionFailed)
	}
}

func TestClassifyUnrecognized(t *testing.T) {
	if _, ok := classify(errors.New("disk full")); ok {
		t.Error("unrecognized error should not classify")
	}
}

func TestFailOp(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	w := httptest.NewRecorder()

	a.failOp(w, compute.ErrorCodeNotFound, "missing")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var e compute.Error
	if err := codec.Unmarshal(w.Body.Bytes(), &e, codec.JSON); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != compute.ErrorCodeNotFound || e.Message != "missing" {
		t.Errorf("error = %+v", e)
	}
}

func TestFailErrKnown(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	w := httptest.NewRecorder()

	a.failErr(w, compute.ErrNotStarted)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var e compute.Error
	if err := codec.Unmarshal(w.Body.Bytes(), &e, codec.JSON); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != compute.ErrorCodePreconditionFailed {
		t.Errorf("code = %q, want %q", e.Code, compute.ErrorCodePreconditionFailed)
	}
}

func TestFailErrUnknown(t *testing.T) {
	a := New(&fakeRuntime{}, Build{})
	w := httptest.NewRecorder()

	a.failErr(w, errors.New("disk full"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
