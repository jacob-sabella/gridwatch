// Package httpx wraps the stdlib HTTP client with transports that
// enforce gzip + User-Agent on every outbound request. Used by sources
// so we can't accidentally send a plain request to Liquipedia and get
// 406-Not-Acceptable'd.
package httpx

import (
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a thin wrapper that injects headers and enforces a timeout.
// Use New to construct one.
type Client struct {
	http      *http.Client
	userAgent string
}

// New returns a Client that sets the given User-Agent on every request
// and asks for gzip encoding. The timeout applies end-to-end per request.
//
// The underlying http.Client uses http.DefaultTransport, which
// transparently decompresses gzip responses as long as we DON'T set
// Accept-Encoding ourselves — except we must set it for Liquipedia (they
// return 406 without it). We solve that by setting Accept-Encoding on the
// request but also manually decompressing in Do.
func New(userAgent string, timeout time.Duration) *Client {
	return &Client{
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				// DisableCompression: true tells the transport NOT to
				// auto-decompress, because we're setting Accept-Encoding
				// ourselves and handling decompression explicitly.
				DisableCompression: true,
				// Sensible defaults; override if a use case needs them.
				MaxIdleConns:        20,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
		userAgent: userAgent,
	}
}

// Do sends the request with gzip and UA headers applied, and returns the
// response with the body wrapped in a gzip reader if the server sent one.
// Caller must close the body as usual.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept-Encoding", "gzip")
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json, text/html;q=0.8, */*;q=0.1")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := newGzipReader(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("gzip: %w", err)
		}
		resp.Body = gzipBodyCloser{rc: gz, orig: resp.Body}
		resp.Header.Del("Content-Encoding")
		resp.Header.Del("Content-Length")
	}
	return resp, nil
}

// ReadAllAndClose is a convenience that reads the body and closes it.
func ReadAllAndClose(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
