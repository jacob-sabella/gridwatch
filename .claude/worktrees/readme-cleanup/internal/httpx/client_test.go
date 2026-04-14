package httpx

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientSendsUAAndAcceptEncoding(t *testing.T) {
	var gotUA, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept-Encoding")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := New("gridwatch/test (+test@example.com)", 5*time.Second)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if gotUA != "gridwatch/test (+test@example.com)" {
		t.Errorf("User-Agent not set: %q", gotUA)
	}
	if gotAccept != "gzip" {
		t.Errorf("Accept-Encoding not gzip: %q", gotAccept)
	}
}

func TestClientDecompressesGzip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte("compressed body hello"))
		_ = gz.Close()
	}))
	defer srv.Close()

	c := New("gridwatch/test (+t@x)", 5*time.Second)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "compressed body hello") {
		t.Errorf("body not decompressed: %q", string(body))
	}
	// After decompression we should have stripped the Content-Encoding header
	// so downstream code doesn't try to double-decode.
	if resp.Header.Get("Content-Encoding") != "" {
		t.Errorf("Content-Encoding still present: %q", resp.Header.Get("Content-Encoding"))
	}
}

func TestClientPassesThroughPlainBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer srv.Close()

	c := New("ua", time.Second)
	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	body, _ := ReadAllAndClose(resp)
	if string(body) != "plain" {
		t.Errorf("plain body: %q", string(body))
	}
}
