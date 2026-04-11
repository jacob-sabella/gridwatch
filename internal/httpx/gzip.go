package httpx

import (
	"compress/gzip"
	"io"
)

// gzipBodyCloser ensures both the gzip reader and the original response
// body are closed when the caller closes the returned Body.
type gzipBodyCloser struct {
	rc   io.ReadCloser
	orig io.Closer
}

func (g gzipBodyCloser) Read(p []byte) (int, error) { return g.rc.Read(p) }
func (g gzipBodyCloser) Close() error {
	err := g.rc.Close()
	if cerr := g.orig.Close(); cerr != nil && err == nil {
		err = cerr
	}
	return err
}

func newGzipReader(r io.ReadCloser) (io.ReadCloser, error) {
	return gzip.NewReader(r)
}
