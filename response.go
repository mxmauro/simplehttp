package simplehttp

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

// -----------------------------------------------------------------------------

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

func (resp *Response) ReadBodyAsBytes() ([]byte, error) {
	return io.ReadAll(resp.Body)
}

func (resp *Response) ReadBodyAsJSON(v interface{}) error {
	decoder := json.NewDecoder(resp.Body)
	return decoder.Decode(v)
}

func (resp *Response) CloseBody() {
	resp.bodyClosed.Do(func() {
		_ = resp.Body.Close()
	})
}
