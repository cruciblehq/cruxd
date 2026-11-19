package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/codec"
)

func TestClassifyNotFound(t *testing.T) {
	code, ok := classify(agent.ErrNotFound)
	if !ok || code != agent.ErrorCodeNotFound {
		t.Errorf("classify = (%q, %v), want (%q, true)", code, ok, agent.ErrorCodeNotFound)
	}
}

func TestClassifyAlreadyStarted(t *testing.T) {
	code, ok := classify(agent.ErrAlreadyStarted)
	if !ok || code != agent.ErrorCodeConflict {
		t.Errorf("classify = (%q, %v), want (%q, true)", code, ok, agent.ErrorCodeConflict)
	}
}

func TestClassifyNotStarted(t *testing.T) {
	code, ok := classify(agent.ErrNotStarted)
	if !ok || code != agent.ErrorCodePreconditionFailed {
		t.Errorf("classify = (%q, %v), want (%q, true)", code, ok, agent.ErrorCodePreconditionFailed)
	}
}

func TestClassifyUnrecognized(t *testing.T) {
	if _, ok := classify(errors.New("disk full")); ok {
		t.Error("unrecognized error should not classify")
	}
}

func TestFailOp(t *testing.T) {
	a := New(&fakeAgent{}, enrolled(), Build{})
	w := httptest.NewRecorder()

	a.failOp(w, agent.ErrorCodeNotFound, "missing")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	var e agent.Error
	if err := codec.Decode(w.Body.Bytes(), &e, agent.BodyFormat); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != agent.ErrorCodeNotFound || e.Message != "missing" {
		t.Errorf("error = %+v", e)
	}
}

func TestFailErrKnown(t *testing.T) {
	a := New(&fakeAgent{}, enrolled(), Build{})
	w := httptest.NewRecorder()

	a.failErr(w, agent.ErrNotStarted)

	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusPreconditionFailed)
	}
	var e agent.Error
	if err := codec.Decode(w.Body.Bytes(), &e, agent.BodyFormat); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != agent.ErrorCodePreconditionFailed {
		t.Errorf("code = %q, want %q", e.Code, agent.ErrorCodePreconditionFailed)
	}
}

func TestFailErrUnknown(t *testing.T) {
	a := New(&fakeAgent{}, enrolled(), Build{})
	w := httptest.NewRecorder()

	a.failErr(w, errors.New("disk full"))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	var e agent.Error
	if err := codec.Decode(w.Body.Bytes(), &e, agent.BodyFormat); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != agent.ErrorCodeInternalError {
		t.Errorf("code = %q, want %q", e.Code, agent.ErrorCodeInternalError)
	}
}
