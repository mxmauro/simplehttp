package simplehttp

import (
	"io"
)

// -----------------------------------------------------------------------------

type nullBody struct {
}

// -----------------------------------------------------------------------------

func (nullBody) Read(p []byte) (n int, err error) {
	return 0, io.EOF
}

func (nullBody) Close() error {
	return nil
}
