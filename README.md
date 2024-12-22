# simplehttp

A simple HTTP client in Go with support for automatic retry on server errors and network failures and simplified
response body handling.

## Usage

```golang
package main

import (
    "context"
    "fmt"
    "net/http"

    "github.com/mxmauro/simplehttp"
)

func main() {
    err := simplehttp.New("GET", "https://some-web/test").
		WithContext(context.Background()).
		WithHeader("X-Auth", "1234").
		WithRetry(100*time.Millisecond, time.Second, 4).
		Exec(func(resp *simplehttp.Response) error {
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			}

			return nil
		})
    if err != nil {
        
    }
}
```

## LICENSE

[MIT](/LICENSE)
