package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cruciblehq/spec/agent"
	"github.com/cruciblehq/utils-go/codec"
	"github.com/cruciblehq/utils-go/httpx"
	"github.com/cruciblehq/utils-go/mediatype"
)

func post(t *testing.T, url string, mt mediatype.MediaType, v any) *http.Response {
	t.Helper()
	body, err := codec.Encode(v, agent.BodyFormat)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := http.Post(url, mt.With(agent.BodyFormat), bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}

func postData(t *testing.T, url string, mt mediatype.MediaType, v any, dataType mediatype.MediaType, data string) *http.Response {
	t.Helper()
	var body bytes.Buffer
	mw := httpx.NewMultipartWriter(&body, mt, agent.BodyFormat)
	if err := mw.Write(v, dataType, strings.NewReader(data)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	resp, err := http.Post(url, mw.ContentType(), &body)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}

func readData(t *testing.T, resp *http.Response, mt mediatype.MediaType, v any, dataType mediatype.MediaType) string {
	t.Helper()
	defer resp.Body.Close()
	data, err := httpx.ReadMultipart(resp.Header.Get(httpx.HeaderContentType), resp.Body, mt, agent.BodyFormat, v, dataType)
	if err != nil {
		t.Fatalf("read multipart: %v", err)
	}
	b, err := io.ReadAll(data)
	if err != nil {
		t.Fatalf("read data: %v", err)
	}
	return string(b)
}

func decodeBody(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := codec.Decode(data, v, agent.BodyFormat); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

func loadContainer(t *testing.T, baseURL string) agent.ContainerID {
	t.Helper()
	resp := post(t, fmt.Sprintf("%s%s", baseURL, agent.EndpointLoad), agent.MediaTypeLoadRequest, agent.LoadRequest{})
	var lr agent.LoadResponse
	decodeBody(t, resp, &lr)
	return lr.ID
}
