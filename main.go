package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

const pattern = `"poweredBy": "– powered by \u003ca\u003egiscus\u003c/a\u003e"`
const replacement = `"poweredBy": ""` // adjust as needed

// streamReplaceWriter: streaming literal replace with a tiny tail buffer.
type streamReplaceWriter struct {
	w   io.Writer
	pat []byte
	rep []byte
	buf []byte
}

func newStreamReplaceWriter(w io.Writer, pat, rep string) *streamReplaceWriter {
	return &streamReplaceWriter{w: w, pat: []byte(pat), rep: []byte(rep)}
}

func (srw *streamReplaceWriter) Write(p []byte) (int, error) {
	srw.buf = append(srw.buf, p...)

	// Flush all but len(pattern)-1 bytes so boundary matches are preserved.
	safe := len(srw.buf) - len(srw.pat) + 1
	if safe <= 0 {
		return len(p), nil
	}
	out := bytes.ReplaceAll(srw.buf[:safe], srw.pat, srw.rep)
	if _, err := srw.w.Write(out); err != nil {
		return 0, err
	}
	srw.buf = srw.buf[safe:]
	return len(p), nil
}

func (srw *streamReplaceWriter) Flush() error {
	if len(srw.buf) == 0 {
		return nil
	}
	out := bytes.ReplaceAll(srw.buf, srw.pat, srw.rep)
	_, err := srw.w.Write(out)
	srw.buf = nil
	return err
}

func main() {
	// Backend is fixed: giscus.app
	targetURL, _ := url.Parse("https://giscus.app")

	// Listen address from $PORT (default 8080)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port

	proxy := httputil.NewSingleHostReverseProxy(targetURL)

	// Ensure we can edit bodies (request upstream identity, not gzip)
	origDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		origDirector(r)
		r.Header.Set("Accept-Encoding", "identity")
		// Make sure Host header matches target host
		r.Host = targetURL.Host
	}

	// Wrap text/JSON responses to stream-replace our exact fragment
	proxy.ModifyResponse = func(resp *http.Response) error {
		ct := resp.Header.Get("Content-Type")
		if !(strings.Contains(ct, "json") || strings.Contains(ct, "text") || strings.Contains(ct, "javascript")) {
			return nil // don't touch non-text
		}

		// Length may change; remove headers that would lie
		resp.Header.Del("Content-Length")
		resp.Header.Del("Content-Encoding")

		pr, pw := io.Pipe()
		srw := newStreamReplaceWriter(pw, pattern, replacement)

		go func(src io.ReadCloser) {
			defer src.Close()
			defer pw.Close()
			_, err := io.Copy(srw, src)
			if err2 := srw.Flush(); err == nil {
				err = err2
			}
			if err != nil {
				_ = pw.CloseWithError(err)
			}
		}(resp.Body)

		resp.Body = pr
		return nil
	}

	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "proxy error: "+err.Error(), http.StatusBadGateway)
	}

	log.Printf("Reverse proxy on %s -> %s", addr, targetURL)
	log.Fatal(http.ListenAndServe(addr, proxy))
}
