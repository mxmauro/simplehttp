# simplehttp

A simple HTTP client in Go with support for automatic retry on server errors and network failures and simplified
response body handling.

### Important note:

This library is in beta stage. The functionality and callbacks may vary on different releases.

## Usage

```golang
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mxmauro/simplehttp"
)

func main() {
	err := simplehttp.New("GET", "https://some-web/test").
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
		// ...
	}
}
```

## LICENSE

[MIT](/LICENSE)
