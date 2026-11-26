package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewRejectsActiveAndUnsupportedFormatsBeforeDownload(t *testing.T) {
	server := &Server{Token: "fixture"}
	for _, name := range []string{"index.html", "script.svg", "virtual.vmdk", "boot.iso", "unknown"} {
		request := httptest.NewRequest("GET", "http://127.0.0.1/v1/datastores/file/preview?ds=fixture&path="+name, nil)
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatal("preview bypassed authentication")
		}
		request.Header.Set("Authorization", "Bearer fixture")
		recorder = httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnsupportedMediaType {
			t.Fatal(name, recorder.Code, recorder.Body.String())
		}
	}
	for _, tc := range []struct {
		name, mime string
		limit      int64
	}{{"config.VMX", "text/plain", 1 << 20}, {"photo.png", "image/png", 16 << 20}, {"manual.pdf", "application/pdf", 16 << 20}} {
		mime, limit := previewFormat(tc.name)
		if !strings.HasPrefix(mime, tc.mime) || limit != tc.limit {
			t.Fatal(tc, mime, limit)
		}
	}
}

func TestPreviewBoundedReadAndSafeText(t *testing.T) {
	w := httptest.NewRecorder()
	if err := writePreview(w, strings.NewReader("<script>unsafe()</script>"), -1, "text/plain; charset=utf-8", 1024); err != nil {
		t.Fatal(err)
	}
	if w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatal(w.Header())
	}
	for _, size := range []int64{-1, 2048} {
		w = httptest.NewRecorder()
		if err := writePreview(w, strings.NewReader(strings.Repeat("x", 2048)), size, "image/png", 1024); err != nil {
			t.Fatal(err)
		}
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatal(w.Code)
		}
	}
}
