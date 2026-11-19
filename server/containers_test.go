package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cruciblehq/spec/agent"
)

type fakeContainer struct {
	started  bool
	output   string
	exitCode int
	err      error
}

func (f *fakeContainer) Start(context.Context) error {
	if f.err != nil {
		return f.err
	}
	if f.started {
		return errors.New("container already started")
	}
	f.started = true
	return nil
}
func (f *fakeContainer) Stop(context.Context) error {
	return f.err
}

func (f *fakeContainer) Exec(_ context.Context, stdout, _ io.Writer, _ string, _ agent.Options) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	_, _ = io.WriteString(stdout, f.output)
	return f.exitCode, nil
}

func (f *fakeContainer) Copy(context.Context, io.Reader) error {
	return f.err
}

func (f *fakeContainer) Configure(context.Context, agent.ImageConfig) error {
	return f.err
}

func (f *fakeContainer) Inspect(context.Context) (agent.ImageConfig, error) {
	if f.err != nil {
		return agent.ImageConfig{}, f.err
	}
	return agent.ImageConfig{User: "container"}, nil
}

func (f *fakeContainer) Commit(context.Context, string) (agent.ImageRef, error) {
	if f.err != nil {
		return "", f.err
	}
	return "img:committed", nil
}

func (f *fakeContainer) Destroy(context.Context) error {
	return f.err
}

func TestLoad(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	if id == "" {
		t.Fatal("empty container handle")
	}
}

func TestStart(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointStart), agent.MediaTypeStartRequest, agent.StartRequest{ID: id})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestExec(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointStart), agent.MediaTypeStartRequest, agent.StartRequest{ID: id})
	resp.Body.Close()

	resp = post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointExec), agent.MediaTypeExecRequest,
		agent.ExecRequest{ID: id, Command: "echo hello"})
	var gotOutput strings.Builder
	var exitSeen bool
	for _, msg := range execMessages(t, resp) {
		switch msg.Type {
		case agent.ExecOutput:
			gotOutput.Write(msg.Data)
		case agent.ExecExit:
			exitSeen = true
			if msg.Code != 0 {
				t.Errorf("exit code = %d, want 0", msg.Code)
			}
		}
	}
	if gotOutput.String() != "hello\n" {
		t.Errorf("exec output = %q, want %q", gotOutput.String(), "hello\n")
	}
	if !exitSeen {
		t.Error("no terminal exit message")
	}
}

func TestDestroy(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointDestroy), agent.MediaTypeDestroyRequest, agent.DestroyRequest{ID: id})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestStartAfterDestroy(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointDestroy), agent.MediaTypeDestroyRequest, agent.DestroyRequest{ID: id})
	resp.Body.Close()

	resp = post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointStart), agent.MediaTypeStartRequest, agent.StartRequest{ID: id})
	var opErr agent.Error
	decodeBody(t, resp, &opErr)
	if opErr.Code != agent.ErrorCodeNotFound {
		t.Errorf("error code = %q, want %q", opErr.Code, agent.ErrorCodeNotFound)
	}
}

func TestStop(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointStop), agent.MediaTypeStopRequest, agent.StopRequest{ID: id})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCopy(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := postData(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointCopy), agent.MediaTypeCopyRequest, agent.CopyRequest{ID: id}, agent.MediaTypeLayer, "tar")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCopyNotFound(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := postData(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointCopy), agent.MediaTypeCopyRequest, agent.CopyRequest{ID: "nope"}, agent.MediaTypeLayer, "x")
	var e agent.Error
	decodeBody(t, resp, &e)
	if e.Code != agent.ErrorCodeNotFound {
		t.Errorf("code = %q, want %q", e.Code, agent.ErrorCodeNotFound)
	}
}

func TestCommit(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointCommit), agent.MediaTypeCommitRequest, agent.CommitRequest{ID: id, By: "me"})
	var cr agent.CommitResponse
	decodeBody(t, resp, &cr)
	if cr.Ref != "img:committed" {
		t.Errorf("ref = %q, want %q", cr.Ref, "img:committed")
	}
}

func TestConfigure(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointConfig), agent.MediaTypeConfigureRequest,
		agent.ConfigureRequest{ID: id, Config: agent.ImageConfig{User: "x"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestInspectContainer(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointInspectContainer), agent.MediaTypeInspectContainerRequest,
		agent.InspectContainerRequest{ID: id})
	var ir agent.InspectContainerResponse
	decodeBody(t, resp, &ir)
	if ir.Config.User != "container" {
		t.Errorf("user = %q, want %q", ir.Config.User, "container")
	}
}

func TestStartMissingID(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := post(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointStart), agent.MediaTypeStartRequest, agent.StartRequest{ID: ""})
	var e agent.Error
	decodeBody(t, resp, &e)
	if e.Code != agent.ErrorCodeBadRequest {
		t.Errorf("code = %q, want %q", e.Code, agent.ErrorCodeBadRequest)
	}
}

func TestCopyMissingID(t *testing.T) {
	srv := httptest.NewServer(New(&fakeAgent{}, enrolled(), Build{}))
	defer srv.Close()

	resp := postData(t, fmt.Sprintf("%s%s", srv.URL, agent.EndpointCopy), agent.MediaTypeCopyRequest, agent.CopyRequest{}, agent.MediaTypeLayer, "x")
	var e agent.Error
	decodeBody(t, resp, &e)
	if e.Code != agent.ErrorCodeBadRequest {
		t.Errorf("code = %q, want %q", e.Code, agent.ErrorCodeBadRequest)
	}
}
