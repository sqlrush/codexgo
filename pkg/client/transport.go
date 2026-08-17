package client

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
)

// ByteChunk is a single item from a streaming response body: either a slice of
// bytes or a transport error.
type ByteChunk struct {
	Data []byte
	Err  *TransportError
}

// ByteStream is the streaming-body abstraction, mirroring the Rust `ByteStream`.
// Consumers range over the channel; the producer closes it when the stream ends.
// A trailing chunk with a non-nil Err signals a transport failure.
type ByteStream <-chan ByteChunk

// StreamResponse is the result of a streaming request before its body has been
// fully read. It mirrors the Rust `StreamResponse`.
type StreamResponse struct {
	Status  int
	Headers http.Header
	Bytes   ByteStream
}

// HTTPTransport sends prepared requests and returns either a buffered response
// or a streaming response. It mirrors the Rust `HttpTransport` trait. Methods
// take a context for cancellation/timeouts.
type HTTPTransport interface {
	Execute(ctx context.Context, req Request) (Response, error)
	Stream(ctx context.Context, req Request) (StreamResponse, error)
}

// HTTPClientTransport is the default HTTPTransport implementation backed by a
// *http.Client. It is the Go analogue of the Rust `ReqwestTransport`.
type HTTPClientTransport struct {
	client *http.Client
}

// NewHTTPClientTransport wraps an *http.Client as an HTTPTransport. The client
// must not be nil.
func NewHTTPClientTransport(client *http.Client) *HTTPClientTransport {
	return &HTTPClientTransport{client: client}
}

// build converts a Request into an *http.Request, applying prepared headers,
// body, and per-request timeout (via a derived context). It never mutates the
// input request.
func (t *HTTPClientTransport) build(ctx context.Context, req Request) (*http.Request, context.CancelFunc, error) {
	prepared, err := req.PrepareBodyForSend()
	if err != nil {
		return nil, nil, NewBuildError(err.Error())
	}

	if _, perr := url.Parse(req.URL); perr != nil {
		return nil, nil, NewBuildError(perr.Error())
	}

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	reqCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		reqCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}

	var bodyReader io.Reader
	if prepared.Body != nil {
		bodyReader = bytes.NewReader(prepared.Body)
	}

	httpReq, herr := http.NewRequestWithContext(reqCtx, method, req.URL, bodyReader)
	if herr != nil {
		if cancel != nil {
			cancel()
		}
		return nil, nil, NewBuildError(herr.Error())
	}
	for name, values := range prepared.Headers {
		for _, v := range values {
			httpReq.Header.Add(name, v)
		}
	}
	return httpReq, cancel, nil
}

// mapError converts an http.Client error into a TransportError, distinguishing
// timeouts from generic network errors. It mirrors the Rust
// `ReqwestTransport::map_error`.
func mapError(err error) *TransportError {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return NewTimeoutError()
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return NewTimeoutError()
	}
	return NewNetworkError(err.Error())
}

// Execute sends a request and buffers the full response. Non-success statuses
// produce a TransportErrorHTTP error carrying the status, URL, headers, and body.
func (t *HTTPClientTransport) Execute(ctx context.Context, req Request) (Response, error) {
	urlStr := req.URL
	httpReq, cancel, err := t.build(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if cancel != nil {
		defer cancel()
	}

	resp, derr := t.client.Do(httpReq)
	if derr != nil {
		return Response{}, mapError(derr)
	}
	defer resp.Body.Close()

	body, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return Response{}, mapError(rerr)
	}
	if !isSuccess(resp.StatusCode) {
		return Response{}, NewHTTPError(resp.StatusCode, urlStr, resp.Header.Clone(), string(body))
	}
	return Response{
		Status:  resp.StatusCode,
		Headers: resp.Header.Clone(),
		Body:    body,
	}, nil
}

// Stream sends a request and returns a streaming response. Non-success statuses
// produce a TransportErrorHTTP error (the body is buffered for the error).
func (t *HTTPClientTransport) Stream(ctx context.Context, req Request) (StreamResponse, error) {
	urlStr := req.URL
	httpReq, cancel, err := t.build(ctx, req)
	if err != nil {
		return StreamResponse{}, err
	}

	resp, derr := t.client.Do(httpReq)
	if derr != nil {
		if cancel != nil {
			cancel()
		}
		return StreamResponse{}, mapError(derr)
	}

	if !isSuccess(resp.StatusCode) {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if cancel != nil {
			cancel()
		}
		return StreamResponse{}, NewHTTPError(resp.StatusCode, urlStr, resp.Header.Clone(), string(body))
	}

	ch := make(chan ByteChunk, 16)
	go pumpBody(resp.Body, ch, cancel)

	return StreamResponse{
		Status:  resp.StatusCode,
		Headers: resp.Header.Clone(),
		Bytes:   ch,
	}, nil
}

// pumpBody reads the response body in chunks and forwards them to ch, closing ch
// when the body ends or an error occurs. It owns closing the body and (if set)
// the per-request cancel func.
func pumpBody(body io.ReadCloser, ch chan<- ByteChunk, cancel context.CancelFunc) {
	defer close(ch)
	defer body.Close()
	if cancel != nil {
		defer cancel()
	}

	buf := make([]byte, 16*1024)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			ch <- ByteChunk{Data: chunk}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			ch <- ByteChunk{Err: mapError(err)}
			return
		}
	}
}

// isSuccess reports whether status is a 2xx HTTP status code.
func isSuccess(status int) bool {
	return status >= 200 && status <= 299
}
