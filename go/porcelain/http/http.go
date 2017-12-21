package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Azure/go-autorest/autorest"
	"github.com/go-openapi/runtime"
)

type RetryableTransport struct {
	tr       runtime.ClientTransport
	attempts int
}

type retryableRoundTripper struct {
	tr       http.RoundTripper
	attempts int
}

func NewRetryableTransport(tr runtime.ClientTransport, attempts int) *RetryableTransport {
	return &RetryableTransport{
		tr:       tr,
		attempts: attempts,
	}
}

func (t *RetryableTransport) Submit(op *runtime.ClientOperation) (interface{}, error) {
	client := op.Client

	if client == nil {
		client = http.DefaultClient
	}

	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = &retryableRoundTripper{
		tr:       transport,
		attempts: t.attempts,
	}

	op.Client = client

	return t.tr.Submit(op)
}

func (tr *retryableRoundTripper) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	rr := autorest.NewRetriableRequest(req)

	// Increment to add the first call (attempts denotes number of retries)
	attempts := tr.attempts
	attempts++
	for attempt := 0; attempt < attempts; attempt++ {
		err = rr.Prepare()
		if err != nil {
			return resp, err
		}

		resp, err = tr.tr.RoundTrip(rr.Request())

		if err != nil || resp.StatusCode != http.StatusTooManyRequests {
			return resp, err
		}

		if !delayWithRateLimit(resp, req.Cancel) {
			return resp, err
		}
	}

	return resp, err
}

func delayWithRateLimit(resp *http.Response, cancel <-chan struct{}) bool {
	r := resp.Header.Get("X-RateLimit-Reset")
	if r == "" {
		return false
	}
	retryReset, err := strconv.ParseInt(r, 10, 0)
	if err != nil {
		return false
	}

	t := time.Unix(retryReset, 0)
	select {
	case <-time.After(t.Sub(time.Now())):
		return true
	case <-cancel:
		return false
	}
}
