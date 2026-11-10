package agent

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
)

func post(t *testing.T, url string, mt compute.MediaType, v any) *http.Response {
	t.Helper()
	body, err := codec.Encode(v, codec.JSON)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := http.Post(url, mt.JSON(), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := codec.Unmarshal(data, v, codec.JSON); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func loadContainer(t *testing.T, baseURL string) compute.ContainerID {
	t.Helper()
	resp := post(t, baseURL+compute.EndpointLoad, compute.MediaTypeLoadRequest, compute.LoadRequest{})
	var lr compute.LoadResponse
	decodeBody(t, resp, &lr)
	return lr.ID
}
