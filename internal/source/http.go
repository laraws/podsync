package source

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Some platform SDKs omit context parameters. Join the fetch and request
// contexts so cancellation does not discard http.Client's shorter deadline.
type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(req.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	if t.ctx.Err() != nil {
		cleanup()
		return nil, t.ctx.Err()
	}
	response, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		cleanup()
		return nil, err
	}
	response.Body = &contextBody{ReadCloser: response.Body, cleanup: cleanup}
	return response, nil
}

type contextBody struct {
	io.ReadCloser
	cleanup func()
}

func (b *contextBody) Close() error { defer b.cleanup(); return b.ReadCloser.Close() }
func httpClient(ctx context.Context) *http.Client {
	return &http.Client{Timeout: 30 * time.Second, Transport: contextTransport{ctx: ctx, base: http.DefaultTransport}}
}
