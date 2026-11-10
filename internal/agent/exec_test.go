package agent

import (
	"bufio"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

func execMessages(t *testing.T, resp *http.Response) []compute.ExecResponse {
	t.Helper()
	defer resp.Body.Close()

	var msgs []compute.ExecResponse
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var m compute.ExecResponse
		if err := codec.Unmarshal(scanner.Bytes(), &m, codec.JSON); err != nil {
			t.Fatalf("decode exec msg: %v", err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestExecNotFound(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp := post(t, srv.URL+compute.EndpointExec, compute.MediaTypeExecRequest,
		compute.ExecRequest{ID: "nope", Command: "x"})
	var e compute.Error
	decodeBody(t, resp, &e)
	if e.Code != compute.ErrorCodeNotFound {
		t.Errorf("code = %q, want %q", e.Code, compute.ErrorCodeNotFound)
	}
}

func TestExecError(t *testing.T) {
	rt := &fakeRuntime{ctr: &fakeContainer{err: compute.ErrNotStarted}}
	srv := httptest.NewServer(New(rt, Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	resp := post(t, srv.URL+compute.EndpointExec, compute.MediaTypeExecRequest,
		compute.ExecRequest{ID: id, Command: "x"})

	msgs := execMessages(t, resp)
	if len(msgs) == 0 {
		t.Fatal("no exec messages")
	}
	last := msgs[len(msgs)-1]
	if last.Type != compute.ExecError {
		t.Fatalf("terminal type = %q, want %q", last.Type, compute.ExecError)
	}
	if last.Error == nil || last.Error.Code != compute.ErrorCodePreconditionFailed {
		t.Errorf("terminal error = %+v", last.Error)
	}
}

func TestExecExitCode(t *testing.T) {
	rt := &fakeRuntime{ctr: &fakeContainer{exitCode: 2}}
	srv := httptest.NewServer(New(rt, Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	resp := post(t, srv.URL+compute.EndpointExec, compute.MediaTypeExecRequest,
		compute.ExecRequest{ID: id, Command: "x"})

	msgs := execMessages(t, resp)
	if len(msgs) == 0 {
		t.Fatal("no exec messages")
	}
	last := msgs[len(msgs)-1]
	if last.Type != compute.ExecExit || last.Code != 2 {
		t.Errorf("terminal = %+v, want ExecExit with code 2", last)
	}
}

func TestExecInternalError(t *testing.T) {
	rt := &fakeRuntime{ctr: &fakeContainer{err: errors.New("runtime failure")}}
	srv := httptest.NewServer(New(rt, Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	resp := post(t, srv.URL+compute.EndpointExec, compute.MediaTypeExecRequest,
		compute.ExecRequest{ID: id, Command: "x"})

	msgs := execMessages(t, resp)
	if len(msgs) == 0 {
		t.Fatal("no exec messages")
	}
	last := msgs[len(msgs)-1]
	if last.Type != compute.ExecError {
		t.Fatalf("terminal type = %q, want %q", last.Type, compute.ExecError)
	}
	if last.Error == nil || last.Error.Code != compute.ErrorCodeInternalError {
		t.Errorf("terminal error = %+v", last.Error)
	}
}
