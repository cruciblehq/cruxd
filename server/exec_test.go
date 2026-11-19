package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/agent"
)

func execMessages(t *testing.T, resp *http.Response) []agent.ExecResponse {
	t.Helper()
	defer resp.Body.Close()

	var msgs []agent.ExecResponse
	r := agent.NewExecReader(resp.Body)
	for {
		m, err := r.Next()
		if errors.Is(err, io.EOF) {
			return msgs
		}
		if err != nil {
			t.Fatalf("decode exec msg: %v", err)
		}
		msgs = append(msgs, m)
	}
}

func TestExecNotFound(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExec), agent.MediaTypeExecRequest,
		agent.ExecRequest{ID: "nope", Command: "x"})
	var e agent.Error
	decodeBody(t, resp, &e)
	if e.Code != agent.ErrorCodeNotFound {
		t.Errorf("code = %q, want %q", e.Code, agent.ErrorCodeNotFound)
	}
}

func TestExecError(t *testing.T) {
	rt := &fakeAgent{ctr: &fakeContainer{err: agent.ErrNotStarted}}
	srv := httptest.NewServer(New(rt, enrolled(), Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExec), agent.MediaTypeExecRequest,
		agent.ExecRequest{ID: id, Command: "x"})

	msgs := execMessages(t, resp)
	if len(msgs) == 0 {
		t.Fatal("no exec messages")
	}
	last := msgs[len(msgs)-1]
	if last.Type != agent.ExecError {
		t.Fatalf("terminal type = %q, want %q", last.Type, agent.ExecError)
	}
	if last.Error == nil || last.Error.Code != agent.ErrorCodePreconditionFailed {
		t.Errorf("terminal error = %+v", last.Error)
	}
}

func TestExecExitCode(t *testing.T) {
	rt := &fakeAgent{ctr: &fakeContainer{exitCode: 2}}
	srv := httptest.NewServer(New(rt, enrolled(), Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExec), agent.MediaTypeExecRequest,
		agent.ExecRequest{ID: id, Command: "x"})

	msgs := execMessages(t, resp)
	if len(msgs) == 0 {
		t.Fatal("no exec messages")
	}
	last := msgs[len(msgs)-1]
	if last.Type != agent.ExecExit || last.Code != 2 {
		t.Errorf("terminal = %+v, want ExecExit with code 2", last)
	}
}

func TestExecInternalError(t *testing.T) {
	rt := &fakeAgent{ctr: &fakeContainer{err: errors.New("runtime failure")}}
	srv := httptest.NewServer(New(rt, enrolled(), Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExec), agent.MediaTypeExecRequest,
		agent.ExecRequest{ID: id, Command: "x"})

	msgs := execMessages(t, resp)
	if len(msgs) == 0 {
		t.Fatal("no exec messages")
	}
	last := msgs[len(msgs)-1]
	if last.Type != agent.ExecError {
		t.Fatalf("terminal type = %q, want %q", last.Type, agent.ExecError)
	}
	if last.Error == nil || last.Error.Code != agent.ErrorCodeInternalError {
		t.Errorf("terminal error = %+v", last.Error)
	}
}
