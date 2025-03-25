package simplehttp

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// -----------------------------------------------------------------------------

func shouldRetry(ctx context.Context, checkCB CheckRetryCallback, resp *Response, err error) (bool, error) {
	if err != nil {
		var urlError *url.Error
		var tlsCertError *tls.CertificateVerificationError
		var ne net.Error
		var netOpErr *net.OpError
		var netDnsErr *net.DNSError

		// The following errors are fatal
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return false, err
		}
		if errors.Is(err, tooManyRedirectsError) {
			return false, err
		}
		if errors.As(err, &urlError) {
			errMsg := urlError.Error()
			if strings.Contains(errMsg, "unsupported protocol scheme") {
				return false, err
			}
			if strings.Contains(errMsg, "invalid header") {
				return false, err
			}
			if strings.Contains(errMsg, "certificate is not trusted") {
				return false, err
			}
		}
		if errors.As(err, &tlsCertError) {
			return false, err
		}

		// Is it a connection/network issue?
		if errors.As(err, &ne) || errors.As(err, &netOpErr) || errors.As(err, &netDnsErr) {
			// The error is likely recoverable so retry.
			return true, nil
		}

		return false, err
	}

	if resp != nil {
		// On status 429, we can wait a bit (check Retry-After response header) and retry.
		if resp.StatusCode == http.StatusTooManyRequests {
			return true, nil
		}

		// On status zero, we can try again assuming the server can eventually recover.
		if resp.StatusCode == 0 {
			return true, nil
		}

		// On server errors, we can try again assuming the server can eventually recover but let's
		// call the user callback
		if resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented {
			canRetry := true
			if checkCB != nil {
				canRetry, err = checkCB(ctx, resp)
				if err != nil {
					return false, err
				}
			}
			if canRetry {
				return true, nil
			}
		}
	}

	// Done
	return false, err
}

func parseRetryAfterHeader(value string) (time.Duration, bool) {
	if len(value) == 0 {
		return 0, false
	}

	// Retry-After: 120
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 { // A negative sleep doesn't make sense
			return 0, false
		}
		return time.Second * time.Duration(seconds), true
	}

	// Retry-After: Fri, 31 Dec 1999 23:59:59 GMT
	if retryAbsTime, err := time.Parse(time.RFC1123, value); err == nil {
		toWait := retryAbsTime.UTC().Sub(time.Now().UTC())
		if toWait < 0 {
			// Due time
			return 0, true
		}
		return toWait, true
	}

	// Done
	return 0, false
}
