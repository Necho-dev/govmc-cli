package httpserver

import (
	"bytes"
	"github.com/Necho-dev/govmc-cli/internal/wmks"
	"mime"
	"net/http"
	"path"
	"time"
)

func (s *Server) handleConsoleCapabilities(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	sdk := s.WMKS
	if sdk == nil {
		sdk = wmks.Missing()
	}
	_ = writeJSON(w, sdk.Status)
}
func (s *Server) handleWMKSAsset(w http.ResponseWriter, r *http.Request) {
	if s.WMKS == nil {
		http.NotFound(w, r)
		return
	}
	b, ok := s.WMKS.Asset(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	ct := mime.TypeByExtension(path.Ext(r.URL.Path))
	if ct == "" {
		ct = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ct)
	http.ServeContent(w, r, path.Base(r.URL.Path), time.Time{}, bytes.NewReader(b))
}
