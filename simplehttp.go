package simplehttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	neturl "net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// -----------------------------------------------------------------------------

// Request defines a retryable and augmented HTTP request.
type Request struct {
	transport *http.Transport

	method  string
	url     string
	query   neturl.Values
	headers http.Header

	maxRedirects int

	ctx     context.Context
	timeout time.Duration

	getBody getBodyFunc

	retry struct {
		minWait time.Duration
		maxWait time.Duration
		count   int

		checkCB CheckRetryCallback
	}

	preRequestCB PreRequestCallback
}

// ExecCallback is called after a successful request is completed. Take into account a successful request
// does NOT imply a http status 200.
type ExecCallback func(ctx context.Context, resp *Response) error

// PreRequestCallback is a callback to call before the inner request is executed.
type PreRequestCallback func(ctx context.Context, req *http.Request) error

// CheckRetryCallback is called to decide if the request must be retried. Mostly when the server returns status
// 5xx because the body may contain useful information. On the other hand, on certain conditions like timeouts,
// the request is retried without calling the callback.
type CheckRetryCallback func(ctx context.Context, resp *Response) (canRetry bool, err error)

// -----------------------------------------------------------------------------

var (
	defaultMaxRedirects = 10

	defaultRetryMinWait = time.Second
	defaultRetryMaxWait = 30 * time.Second
	defaultRetryCount   = 4

	tooManyRedirectsError = errors.New("too many redirects")

	nb = nullBody{}

	transportInitOnce        sync.Once
	defaultTransport         *http.Transport
	defaultInsecureTransport *http.Transport
)

type getBodyFunc func() (io.ReadCloser, int64, error)

// -----------------------------------------------------------------------------

// New creates a new request object.
func New(method string, url string) *Request {
	// Initialize default transport if not done yet.
	transportInitOnce.Do(func() {
		// From: https://www.loginradius.com/blog/async/tune-the-go-http-client-for-high-performance/
		defaultTransport = http.DefaultTransport.(*http.Transport).Clone()
		defaultTransport.MaxIdleConns = 32
		defaultTransport.MaxConnsPerHost = 10
		defaultTransport.IdleConnTimeout = 60 * time.Second
		defaultTransport.MaxIdleConnsPerHost = 10
		defaultTransport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}

		defaultInsecureTransport = defaultTransport.Clone()
		defaultInsecureTransport.TLSClientConfig.MinVersion = tls.VersionTLS10
		defaultInsecureTransport.TLSClientConfig.InsecureSkipVerify = true
	})

	// Set default method if none is set.
	if len(method) == 0 {
		method = "GET"
	}

	// Create the request object.
	req := Request{
		method:       method,
		url:          url,
		query:        make(neturl.Values),
		headers:      make(http.Header),
		maxRedirects: defaultMaxRedirects,
		transport:    defaultTransport,
		timeout:      10 * time.Second,
	}
	req.retry.minWait = defaultRetryMinWait
	req.retry.maxWait = defaultRetryMaxWait
	req.retry.count = defaultRetryCount

	// Done
	return &req
}

// WithTransport sets a HTTP transport.
func (req *Request) WithTransport(transport *http.Transport) *Request {
	req.transport = transport

	// Done
	return req
}

// WithInsecureDefaultTransport sets a default HTTPS INSECURE transport.
func (req *Request) WithInsecureDefaultTransport() *Request {
	req.transport = defaultInsecureTransport

	// Done
	return req
}

// WithRetry establishes the number of retry attempts and the minimum and maximum wait times between them.
func (req *Request) WithRetry(min time.Duration, max time.Duration, count int, cb CheckRetryCallback) *Request {
	if min <= 0 {
		min = defaultRetryMinWait
	}
	if max <= 0 {
		max = defaultRetryMaxWait
	}
	if min > max {
		min = max
	} else if max < min {
		max = min
	}
	if count <= 0 {
		count = defaultRetryCount
	}
	req.retry.minWait = min
	req.retry.maxWait = max
	req.retry.count = count
	req.retry.checkCB = cb

	// Done
	return req
}

// WithContext sets the context.Context to use during the request.
func (req *Request) WithContext(ctx context.Context) *Request {
	req.ctx = ctx

	// Done
	return req
}

// WithMaxRedirects sets the maximum number of allowed redirections. Defaults to 10.
func (req *Request) WithMaxRedirects(count int) *Request {
	if count < 0 {
		count = defaultMaxRedirects
	}
	req.maxRedirects = count

	// Done
	return req
}

// WithTimeout sets the maximum allowed time to complete a request. This value sets the timeout for a single
// request. If they are retried because, for e.g., due to server busy response, the timeout is evaluated for each
// operation. A value of zero means no timeout. Defaults to 10 seconds.
func (req *Request) WithTimeout(timeout time.Duration) *Request {
	if timeout < 0 {
		timeout = 10 * time.Second
	}
	req.timeout = timeout

	// Done
	return req
}

// WithHeader sets a request header key/value pair.
func (req *Request) WithHeader(key string, value string) *Request {
	req.headers.Set(key, value)

	// Done
	return req
}

// Headers allows direct access to the request headers.
func (req *Request) Headers() *http.Header {
	return &req.headers
}

// WithQuery sets an url query parameter.
func (req *Request) WithQuery(key string, value string) *Request {
	req.query.Set(key, value)

	// Done
	return req
}

// Query allows direct access to the request query parameter.
func (req *Request) Query() *neturl.Values {
	return &req.query
}

// WithBody sets the request's body.
func (req *Request) WithBody(body io.Reader) *Request {
	if body != nil {
		// Set up a body reader cloning function.
		switch v := body.(type) {
		case *bytes.Buffer:
			buf := v.Bytes()
			req.getBody = func() (io.ReadCloser, int64, error) {
				r := bytes.NewReader(buf)
				return io.NopCloser(r), int64(len(buf)), nil
			}

		case *bytes.Reader:
			copyOfV := v
			req.getBody = func() (io.ReadCloser, int64, error) {
				r := copyOfV
				return io.NopCloser(r), int64(r.Len()), nil
			}

		case *strings.Reader:
			copyOfV := v
			req.getBody = func() (io.ReadCloser, int64, error) {
				r := copyOfV
				return io.NopCloser(r), int64(r.Len()), nil
			}

		default:
			// Check if the body reader implements a seeker.
			if seeker, hasSeeker := v.(io.Seeker); hasSeeker {
				// Try to determine content size.
				bodySize := int64(-1)
				if f, ok := v.(*os.File); ok {
					fi, err := f.Stat()
					if err == nil {
						bodySize = fi.Size()
					}
				}

				currPos, err := seeker.Seek(0, io.SeekCurrent)
				if err == nil {
					if bodySize >= 0 {
						if currPos < bodySize {
							bodySize -= currPos
						} else {
							bodySize = 0
						}
					}
					req.getBody = func() (io.ReadCloser, int64, error) {
						_, err2 := seeker.Seek(currPos, io.SeekStart)
						if err2 != nil {
							return nil, bodySize, err2
						}
						// If the interface also implements a closer, let's replace it with a NopCloser just
						// in case the underlying http.Client or transport closes it.
						return io.NopCloser(io.LimitReader(v, bodySize)), bodySize, nil
					}
				} else {
					req.setErrorBodyReader(errors.New("unable to seek from reader"))
				}
			} else {
				firstUse := true
				req.getBody = func() (io.ReadCloser, int64, error) {
					if firstUse {
						firstUse = false
						// If the interface also implements a closer, let's replace it with a NopCloser just
						// in case the underlying http.Client or transport closes it.
						return io.NopCloser(v), -1, nil
					}
					return nil, -1, errors.New("unable to rewind body")
				}
			}
		}
	} else {
		// If a body was not given, getter will return nil.
		req.getBody = nil
	}

	// Done
	return req
}

// WithBodyBytes sets the request's body.
func (req *Request) WithBodyBytes(buf []byte) *Request {
	if buf != nil {
		req.getBody = func() (io.ReadCloser, int64, error) {
			r := bytes.NewReader(buf)
			return io.NopCloser(r), int64(len(buf)), nil
		}
	} else {
		req.getBody = nil
	}

	// Done
	return req
}

// WithBodyJSON sets the request's body.
func (req *Request) WithBodyJSON(body interface{}) *Request {
	if body != nil {
		payload, err := json.Marshal(body)
		if err == nil {
			return req.WithBodyBytes(payload)
		} else {
			req.setErrorBodyReader(err)
		}
	} else {
		req.setErrorBodyReader(errors.New("nil json body"))
	}

	// Done
	return req
}

// WithPreRequestCallback specifies a callback to call before the request is executed.
func (req *Request) WithPreRequestCallback(cb PreRequestCallback) *Request {
	req.preRequestCB = cb

	// Done
	return req
}

// Exec is used to execute the HTTP request. Unlike Golang's HTTP Client request, after the callback
// is called, the response body is closed.
func (req *Request) Exec(cb ExecCallback) error {
	if cb == nil {
		return errors.New("invalid callback")
	}

	// Create HTTP client handler.
	client := http.Client{
		Transport: req.transport,
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) >= req.maxRedirects {
				return tooManyRedirectsError
			}
			return nil
		},
	}
	if req.transport == nil {
		client.Transport = defaultTransport
	}

	// Setup context.
	ctx := req.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Parse url.
	parsedUrl, err := neturl.Parse(req.url)
	if err != nil {
		return err
	}
	// Add query parameters.
	if len(req.query) > 0 {
		qv := parsedUrl.Query()
		for k, v := range req.query {
			qv[k] = v
		}
		parsedUrl.RawQuery = req.query.Encode()
	}
	// Get the final URL.
	url := parsedUrl.String()

	// Main loop.
	for attempt := 1; ; attempt++ {
		var _resp *http.Response

		// Execute request.
		_resp, err = req.execOneRequest(ctx, &client, url)

		// Sanitization.
		if err == nil && _resp == nil {
			return errors.New("unexpected nil response")
		}

		// Wrap the response.
		resp := Response{
			Body: nb, // Set the null body as default.
		}
		if _resp != nil {
			resp.Status = _resp.Status
			resp.StatusCode = _resp.StatusCode
			resp.Proto = _resp.Proto
			resp.ProtoMajor = _resp.ProtoMajor
			resp.ProtoMinor = _resp.ProtoMinor
			resp.Header = _resp.Header
			if err == nil && _resp.Body != nil {
				resp.Body = _resp.Body
				resp.ContentLength = _resp.ContentLength
			} else {
				// Close the original body, if any.
				if _resp.Body != nil {
					_ = _resp.Body.Close()
				}
			}
		}

		// Check if we reached the maximum amount of attempts and if we should retry the operation.
		willRetry := true
		if attempt >= req.retry.count {
			// If we got an error, return it.
			if err != nil {
				return err
			}

			willRetry = false
		} else {
			willRetry, err = shouldRetry(ctx, req.retry.checkCB, &resp, err)

			// If we got an error, return it.
			if err != nil {
				return err
			}
		}
		if !willRetry {
			// Call the provided callback with the final result.
			err = cb(ctx, &resp)

			// Close the response body.
			resp.CloseBody()

			// Done.
			return err
		}

		// Close the response body.
		resp.CloseBody()

		// Check if the context was signalled.
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Calculate the time to wait until the next try.
		timeToWait := req.calculateWaitTime(attempt, &resp)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(timeToWait):
		}
	}
}

func (req *Request) execOneRequest(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	var ctxCancel context.CancelFunc

	// Setup context with timeout if anyone is given.
	if req.timeout > 0 {
		ctx, ctxCancel = context.WithTimeout(ctx, req.timeout)
		defer ctxCancel()
	}

	// Create a new request.
	_req, err := http.NewRequestWithContext(ctx, req.method, url, nil)
	if err != nil {
		return nil, err
	}

	// Set the body if any.
	if req.getBody != nil {
		_req.Body, _req.ContentLength, err = req.getBody()
		if err != nil {
			return nil, err
		}

		_req.GetBody = func() (io.ReadCloser, error) {
			bodyReader, _, err2 := req.getBody()
			if err2 != nil {
				return nil, err2
			}
			return bodyReader, nil
		}
	}

	// Add custom headers if provided.
	if len(req.headers) > 0 {
		_req.Header = req.headers
	}

	// Call the pre-request callback if anyone was specified.
	if req.preRequestCB != nil {
		err = req.preRequestCB(ctx, _req)
		if err != nil {
			return nil, err
		}
	}

	// Execute request
	return client.Do(_req)
}

func (req *Request) calculateWaitTime(attemptNum int, resp *Response) time.Duration {
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
		toWaitTime, ok := parseRetryAfterHeader(resp.Header.Get("Retry-After"))
		if ok {
			return toWaitTime
		}
	}

	// Compute fraction using the exponential formula
	den := math.Pow(2, float64(req.retry.count-1)) - 1
	if den <= 0.00000000001 {
		return req.retry.minWait
	}
	num := math.Pow(2, float64(attemptNum-1)) - 1

	// Scale between minWait and maxWait using fraction
	waitRange := float64(req.retry.maxWait - req.retry.minWait)
	scaled := float64(req.retry.minWait) + (waitRange * (num / den))

	// Done
	return time.Duration(scaled)
}

func (req *Request) setErrorBodyReader(err error) {
	req.getBody = func() (io.ReadCloser, int64, error) {
		return nil, 0, err
	}
}
