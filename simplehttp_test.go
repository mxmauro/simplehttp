package simplehttp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mxmauro/simplehttp"
)

// -----------------------------------------------------------------------------

func TestBasic(t *testing.T) {
	type Response struct {
		Message string `json:"message"`
	}
	var failureCount int32

	ts, err := NewTestServer(TestServerHandlerList{
		"/test": func(w http.ResponseWriter, r *http.Request) {
			// Atomically increment the failure count
			currentCount := atomic.AddInt32(&failureCount, 1)

			if currentCount <= 2 {
				// Respond with a simulated 500 error
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}

			response := Response{
				Message: "Success!",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ts.Shutdown()

	err = simplehttp.New("GET", ts.BaseUrl()+"/test").
		WithContext(context.Background()).
		WithHeader("X-Auth", "1234").
		WithRetry(100*time.Millisecond, time.Second, 4, nil).
		Exec(func(ctx context.Context, resp *simplehttp.Response) error {
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}

			return nil
		})
	if err != nil {
		t.Fatal(err)
	}

	// Done
	t.Log("Success!")
}
