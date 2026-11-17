package agent

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
	"github.com/cruciblehq/utils-go/mediatype"
)

func post(t *testing.T, url string, mt mediatype.MediaType, v any) *http.Response {
	t.Helper()
	body, err := codec.Encode(v, compute.BodyFormat)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := http.Post(url, mt.With(compute.BodyFormat), bytes.NewReader(body))
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
	if err := codec.Decode(data, v, compute.BodyFormat); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func loadContainer(t *testing.T, baseURL string) compute.ContainerID {
	t.Helper()
	resp := post(t, fmt.Sprintf("%s%s", baseURL, compute.EndpointLoad), compute.MediaTypeLoadRequest, compute.LoadRequest{})
	var lr compute.LoadResponse
	decodeBody(t, resp, &lr)
	return lr.ID
}
