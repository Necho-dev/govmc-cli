package httpserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
)

type dsBody struct {
	DS                   string `json:"ds"`
	DestinationDatastore string `json:"dstDatastore"`
	Path                 string `json:"path"`
	Src                  string `json:"src"`
	Dst                  string `json:"dst"`
	Name                 string `json:"name"`
	Confirm              bool   `json:"confirm"`
	DryRun               *bool  `json:"dryRun"`
}

func (s *Server) dsQuery(r *http.Request, body dsBody) (ds, pth string) {
	ds = r.URL.Query().Get("ds")
	if ds == "" {
		ds = body.DS
	}
	pth = r.URL.Query().Get("path")
	if pth == "" {
		pth = body.Path
	}
	return ds, pth
}

func decodeDS(r *http.Request) dsBody {
	var body dsBody
	if r.Body != nil && r.Header.Get("Content-Type") != "" && r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	return body
}

func (s *Server) handleFolderList(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := s.ctx(r)
	defer cancel()
	ds, pth := s.dsQuery(r, dsBody{})
	out, err := s.ESXi.FSList(ctx, ds, pth, "folder")
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) handleFolderCreate(w http.ResponseWriter, r *http.Request) error {
	var body dsBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	ds, pth := s.dsQuery(r, body)
	ctx, cancel := s.ctx(r)
	defer cancel()
	out, err := s.ESXi.FSMkdir(ctx, ds, pth, confirmed(r, body.Confirm))
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) handleFolderDelete(w http.ResponseWriter, r *http.Request) error {
	body := decodeDS(r)
	ds, pth := s.dsQuery(r, body)
	qConfirm, _ := strconv.ParseBool(r.URL.Query().Get("confirm"))
	ctx, cancel := s.ctxCopy(r)
	defer cancel()
	out, err := s.ESXi.FSDelete(ctx, ds, pth, confirmed(r, body.Confirm || qConfirm))
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) handleFolderCopy(w http.ResponseWriter, r *http.Request) error {
	return s.handleFSCopyMove(w, r, true, true)
}

func (s *Server) handleFolderMove(w http.ResponseWriter, r *http.Request) error {
	return s.handleFSCopyMove(w, r, false, true)
}

func (s *Server) handleFolderRename(w http.ResponseWriter, r *http.Request) error {
	return s.handleFSRename(w, r)
}

func (s *Server) handleFileList(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := s.ctx(r)
	defer cancel()
	ds, pth := s.dsQuery(r, dsBody{})
	out, err := s.ESXi.FSList(ctx, ds, pth, "file")
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) error {
	return s.handleFolderDelete(w, r)
}

func (s *Server) handleFileCopy(w http.ResponseWriter, r *http.Request) error {
	return s.handleFSCopyMove(w, r, true, false)
}

func (s *Server) handleFileMove(w http.ResponseWriter, r *http.Request) error {
	return s.handleFSCopyMove(w, r, false, false)
}

func (s *Server) handleFileRename(w http.ResponseWriter, r *http.Request) error {
	return s.handleFSRename(w, r)
}

func (s *Server) handleFSCopyMove(w http.ResponseWriter, r *http.Request, copy, _ bool) error {
	var body dsBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	ds := body.DS
	if ds == "" {
		ds = r.URL.Query().Get("ds")
	}
	dry := true
	if body.DryRun != nil {
		dry = *body.DryRun
	}
	ctx, cancel := s.ctxCopy(r)
	defer cancel()
	out, err := workflow.CopyMove(ctx, s.Tasks, s.ESXi, s.ProfileName, s.ESXi.Endpoint(), ds, body.Src, body.Dst, copy, dry, confirmed(r, body.Confirm), r.Header.Get("Idempotency-Key"), nil, body.DestinationDatastore)
	if err == nil && !dry {
		w.WriteHeader(http.StatusAccepted)
	}
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) handleFSRename(w http.ResponseWriter, r *http.Request) error {
	var body dsBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	ds := body.DS
	if ds == "" {
		ds = r.URL.Query().Get("ds")
	}
	dst, err := esxi.RenameDest(body.Path, body.Name)
	if err != nil {
		return err
	}
	dry := true
	if body.DryRun != nil {
		dry = *body.DryRun
	}
	ctx, cancel := s.ctxCopy(r)
	defer cancel()
	out, err := s.ESXi.FSMove(ctx, ds, body.Path, dst, confirmed(r, body.Confirm), dry)
	if err != nil {
		return err
	}
	return writeJSON(w, out)
}

func (s *Server) handleFileDownload(w http.ResponseWriter, r *http.Request) error {
	ds, pth := s.dsQuery(r, dsBody{})
	ctx, cancel := s.ctxCopy(r)
	defer cancel()
	rc, n, err := s.ESXi.FSDownload(ctx, ds, pth)
	if err != nil {
		return err
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(path.Base(pth)))
	if n > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(n, 10))
	}
	_, err = io.Copy(w, rc)
	return err
}

func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) error {
	ds := r.URL.Query().Get("ds")
	pth := r.URL.Query().Get("path")
	ctx, cancel := s.ctxCopy(r)
	defer cancel()
	if err := s.ESXi.FSUpload(ctx, ds, pth, r.Body, confirmed(r, false)); err != nil {
		return err
	}
	return writeJSON(w, map[string]string{"status": "uploaded", "path": pth})
}

func (s *Server) handleEntryList(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := s.ctx(r)
	defer cancel()
	ds, pth := s.dsQuery(r, dsBody{})
	out, err := s.ESXi.DatastoreFiles(ctx, ds, pth)
	if err != nil {
		return err
	}
	if out == nil {
		out = []esxi.DSFile{}
	}
	return writeJSON(w, out)
}

func previewFormat(name string) (string, int64) {
	switch strings.ToLower(path.Ext(name)) {
	case ".txt", ".log", ".json", ".yaml", ".yml", ".xml", ".vmx", ".vmxf", ".vmsd", ".ini", ".cfg", ".conf", ".md", ".csv":
		return "text/plain; charset=utf-8", 1 << 20
	case ".png":
		return "image/png", 16 << 20
	case ".jpg", ".jpeg":
		return "image/jpeg", 16 << 20
	case ".gif":
		return "image/gif", 16 << 20
	case ".webp":
		return "image/webp", 16 << 20
	case ".pdf":
		return "application/pdf", 16 << 20
	}
	return "", 0
}

func (s *Server) handleFilePreview(w http.ResponseWriter, r *http.Request) error {
	ds, pth := s.dsQuery(r, dsBody{})
	mime, limit := previewFormat(pth)
	if limit == 0 {
		http.Error(w, "preview_unsupported", http.StatusUnsupportedMediaType)
		return nil
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	reader, n, err := s.ESXi.FSDownload(ctx, ds, pth)
	if err != nil {
		return err
	}
	defer reader.Close()
	return writePreview(w, reader, n, mime, limit)
}

func writePreview(w http.ResponseWriter, reader io.Reader, n int64, mime string, limit int64) error {
	if n > limit {
		http.Error(w, "preview_too_large", http.StatusRequestEntityTooLarge)
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		http.Error(w, "preview_too_large", http.StatusRequestEntityTooLarge)
		return nil
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "no-store")
	_, err = w.Write(data)
	return err
}
