package agent

import (
	"io"
	"mime"
	"net/http"

	"github.com/cruciblehq/spec/compute"
	"github.com/cruciblehq/utils-go/codec"
	"github.com/cruciblehq/utils-go/crex"
)

// Decodes a request body of the expected media type into v.
//
// Reads the whole body and decodes it. Returns an error when the request
// Content-Type does not match the expected media type.
func (a *Agent) decode(r *http.Request, expected compute.MediaType, v any) error {
	ct := r.Header.Get(compute.HeaderContentType)
	if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != expected.JSON() {
		return crex.Newf(errUnexpectedContentType, "%q does not match %q", ct, expected.JSON())
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return codec.Unmarshal(data, v, codec.JSON)
}

// Encodes v as the response body with the given media type and status.
//
// Writes a 500 and returns the error when v cannot be encoded. Returns the
// error when the body cannot be written.
func (a *Agent) encode(w http.ResponseWriter, mt compute.MediaType, status int, v any) error {
	data, err := codec.Encode(v, codec.JSON)
	if err != nil {
		http.Error(w, "could not encode response", http.StatusInternalServerError)
		return err
	}
	w.Header().Set(compute.HeaderContentType, mt.JSON())
	w.WriteHeader(status)
	if _, err := w.Write(data); err != nil {
		return err
	}
	return nil
}
