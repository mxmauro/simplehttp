package simplehttp

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

// -----------------------------------------------------------------------------

// Response wraps the http response for quick access to most used fields and also handles automatic
// closing of body.
type Response struct {
	Status     string // e.g. "200 OK"
	StatusCode int    // e.g. 200
	Proto      string // e.g. "HTTP/1.0"
	ProtoMajor int    // e.g. 1
	ProtoMinor int    // e.g. 0

	Header http.Header

	bodyClosed sync.Once
	Body       io.ReadCloser

	ContentLength int64
}

// -----------------------------------------------------------------------------

// ReadBodyAsBytes saves all the response body into a slice.
func (resp *Response) ReadBodyAsBytes() ([]byte, error) {
	return io.ReadAll(resp.Body)
}

// ReadBodyAsJSON decodes the response body into a JSON.
func (resp *Response) ReadBodyAsJSON(v interface{}) error {
	decoder := json.NewDecoder(resp.Body)
	return decoder.Decode(v)
}

// CloseBody closes the response body reader. No need to call this function unless you want to free resources
// in advance.
func (resp *Response) CloseBody() {
	resp.bodyClosed.Do(func() {
		_ = resp.Body.Close()
	})
}
