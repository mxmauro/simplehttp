package simplehttp_test

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------

type TestServerHandler func(http.ResponseWriter, *http.Request)
type TestServerHandlerList map[string]TestServerHandler

type TestServer struct {
	baseUrl  string
	shutdown func()
}

// -----------------------------------------------------------------------------

func NewTestServer(handlerList TestServerHandlerList) (*TestServer, error) {
	ts := TestServer{}

	mux := http.NewServeMux()
	for ep, handler := range handlerList {
		if !strings.HasPrefix(ep, "/") {
			ep = "/" + ep
		}
		mux.HandleFunc(ep, handler)
	}

	// Create a custom listener on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	ts.baseUrl = "http://127.0.0.1:" + strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)

	server := &http.Server{
		Handler: mux,
	}

	ts.shutdown = func() {
		// Create a context with timeout for the shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = server.Shutdown(ctx)
	}

	go func() {
		_ = server.Serve(listener)
	}()

	return &ts, nil
}

func (srv *TestServer) Shutdown() {
	srv.shutdown()
}

func (srv *TestServer) BaseUrl() string {
	return srv.baseUrl
}
