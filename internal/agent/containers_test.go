package agent

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
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

func (f *fakeContainer) Exec(_ context.Context, stdout, _ io.Writer, _ string, _ compute.Options) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	_, _ = io.WriteString(stdout, f.output)
	return f.exitCode, nil
}

func (f *fakeContainer) Copy(context.Context, io.Reader) error {
	return f.err
}

func (f *fakeContainer) Configure(context.Context, compute.ImageConfig) error {
	return f.err
}

func (f *fakeContainer) Inspect(context.Context) (compute.ImageConfig, error) {
	if f.err != nil {
		return compute.ImageConfig{}, f.err
	}
	return compute.ImageConfig{User: "container"}, nil
}

func (f *fakeContainer) Commit(context.Context, string) (compute.ImageRef, error) {
	if f.err != nil {
		return "", f.err
	}
	return "img:committed", nil
}

func (f *fakeContainer) Destroy(context.Context) error {
	return f.err
}

func TestLoad(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	id := loadContainer(t, srv.URL)
	if id == "" {
		t.Fatal("empty container handle")
	}
}

func TestStart(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointStart, compute.MediaTypeStartRequest, compute.StartRequest{ID: id})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestExec(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointStart, compute.MediaTypeStartRequest, compute.StartRequest{ID: id})
	resp.Body.Close()

	resp = post(t, srv.URL+compute.EndpointExec, compute.MediaTypeExecRequest,
		compute.ExecRequest{ID: id, Command: "echo hello"})
	var gotOutput string
	var exitSeen bool
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var msg compute.ExecResponse
		if err := codec.Unmarshal(scanner.Bytes(), &msg, codec.JSON); err != nil {
			t.Fatalf("decode exec msg: %v", err)
		}
		switch msg.Type {
		case compute.ExecOutput:
			gotOutput += string(msg.Data)
		case compute.ExecExit:
			exitSeen = true
			if msg.Code != 0 {
				t.Errorf("exit code = %d, want 0", msg.Code)
			}
		}
	}
	resp.Body.Close()
	if gotOutput != "hello\n" {
		t.Errorf("exec output = %q, want %q", gotOutput, "hello\n")
	}
	if !exitSeen {
		t.Error("no terminal exit message")
	}
}

func TestDestroy(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointDestroy, compute.MediaTypeDestroyRequest, compute.DestroyRequest{ID: id})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestStartAfterDestroy(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointDestroy, compute.MediaTypeDestroyRequest, compute.DestroyRequest{ID: id})
	resp.Body.Close()

	resp = post(t, srv.URL+compute.EndpointStart, compute.MediaTypeStartRequest, compute.StartRequest{ID: id})
	var opErr compute.Error
	decodeBody(t, resp, &opErr)
	if opErr.Code != compute.ErrorCodeNotFound {
		t.Errorf("error code = %q, want %q", opErr.Code, compute.ErrorCodeNotFound)
	}
}

func TestStop(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointStop, compute.MediaTypeStopRequest, compute.StopRequest{ID: id})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCopy(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	url := fmt.Sprintf("%s%s?%s=%s", srv.URL, compute.EndpointCopy, compute.QueryParamID, id)
	resp, err := http.Post(url, compute.MediaTypeLayer.String(), bytes.NewReader([]byte("tar")))
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestCopyNotFound(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	url := fmt.Sprintf("%s%s?%s=nope", srv.URL, compute.EndpointCopy, compute.QueryParamID)
	resp, err := http.Post(url, compute.MediaTypeLayer.String(), bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	var e compute.Error
	decodeBody(t, resp, &e)
	if e.Code != compute.ErrorCodeNotFound {
		t.Errorf("code = %q, want %q", e.Code, compute.ErrorCodeNotFound)
	}
}

func TestCommit(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointCommit, compute.MediaTypeCommitRequest, compute.CommitRequest{ID: id, By: "me"})
	var cr compute.CommitResponse
	decodeBody(t, resp, &cr)
	if cr.Ref != "img:committed" {
		t.Errorf("ref = %q, want %q", cr.Ref, "img:committed")
	}
}

func TestConfigure(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointConfig, compute.MediaTypeConfigureRequest,
		compute.ConfigureRequest{ID: id, Config: compute.ImageConfig{User: "x"}})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestInspectContainer(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()
	id := loadContainer(t, srv.URL)

	resp := post(t, srv.URL+compute.EndpointInspectContainer, compute.MediaTypeInspectContainerRequest,
		compute.InspectContainerRequest{ID: id})
	var ir compute.InspectContainerResponse
	decodeBody(t, resp, &ir)
	if ir.Config.User != "container" {
		t.Errorf("user = %q, want %q", ir.Config.User, "container")
	}
}

func TestStartMissingID(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	resp := post(t, srv.URL+compute.EndpointStart, compute.MediaTypeStartRequest, compute.StartRequest{ID: ""})
	var e compute.Error
	decodeBody(t, resp, &e)
	if e.Code != compute.ErrorCodeBadRequest {
		t.Errorf("code = %q, want %q", e.Code, compute.ErrorCodeBadRequest)
	}
}

func TestCopyMissingID(t *testing.T) {
	srv := httptest.NewServer(New(&fakeRuntime{}, Build{}))
	defer srv.Close()

	url := fmt.Sprintf("%s%s", srv.URL, compute.EndpointCopy)
	resp, err := http.Post(url, compute.MediaTypeLayer.String(), bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	var e compute.Error
	decodeBody(t, resp, &e)
	if e.Code != compute.ErrorCodeBadRequest {
		t.Errorf("code = %q, want %q", e.Code, compute.ErrorCodeBadRequest)
	}
}
