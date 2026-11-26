package httpserver

import (
	"encoding/json"
	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/workflow"
	"net/http"
	"strconv"
	"time"
)

func (s *Server) handleCustomClone(w http.ResponseWriter, r *http.Request) error {
	var spec esxi.CloneSpec
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&spec); e != nil {
		return esxi.ErrBad("invalid clone request: %v", e)
	}
	ctx, cancel := s.ctx(r)
	defer cancel()
	out, e := workflow.Clone(ctx, s.Tasks, s.ESXi, s.ProfileName, s.ESXi.Endpoint(), spec, r.Header.Get("Idempotency-Key"), nil)
	if e != nil {
		return e
	}
	if !spec.DryRunValue() {
		w.WriteHeader(http.StatusAccepted)
	}
	return writeJSON(w, out)
}
func (s *Server) taskService() error {
	if s.Tasks == nil {
		return &esxi.Error{Status: 503, Code: "tasks_unavailable", Message: "task service unavailable"}
	}
	return nil
}
func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) error {
	if e := s.taskService(); e != nil {
		return e
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	profile := s.ProfileName
	if q.Get("allProfiles") == "true" {
		profile = ""
	}
	since, until := time.Time{}, time.Time{}
	var parseErr error
	if q.Get("since") != "" {
		since, parseErr = time.Parse(time.RFC3339, q.Get("since"))
		if parseErr != nil {
			return esxi.ErrBad("invalid since")
		}
	}
	if q.Get("until") != "" {
		until, parseErr = time.Parse(time.RFC3339, q.Get("until"))
		if parseErr != nil {
			return esxi.ErrBad("invalid until")
		}
	}
	if !since.IsZero() && !until.IsZero() && until.Before(since) {
		return esxi.ErrBad("until must follow since")
	}
	out, e := s.Tasks.List(tasks.Query{Since: since, Until: until, Profile: profile, Status: q.Get("status"), Kind: q.Get("kind"), Source: q.Get("source"), Search: q.Get("search"), Limit: limit, Offset: offset})
	if e != nil {
		return e
	}
	return writeJSON(w, out)
}
func (s *Server) handleTaskStatus(w http.ResponseWriter, r *http.Request) error {
	if e := s.taskService(); e != nil {
		return e
	}
	out, e := s.Tasks.Get(r.PathValue("taskId"))
	if e != nil {
		return esxi.ErrNotFound("task not found")
	}
	if out.Profile != s.ProfileName && r.URL.Query().Get("allProfiles") != "true" {
		return esxi.ErrNotFound("task not found in selected profile")
	}
	return writeJSON(w, out)
}
func (s *Server) handleTaskCancel(w http.ResponseWriter, r *http.Request) error {
	if e := s.taskService(); e != nil {
		return e
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil || !confirmed(r, body.Confirm) {
		return esxi.RequireConfirm(false)
	}
	t, e := s.Tasks.Get(r.PathValue("taskId"))
	if e != nil || t.Profile != s.ProfileName {
		return esxi.ErrNotFound("task not found for selected endpoint")
	}
	if e = s.Tasks.Cancel(t.ID); e != nil {
		return esxi.ErrBad("%s", e)
	}
	return writeJSON(w, map[string]string{"taskId": t.ID, "status": "cancel_requested"})
}

// Keep existing REST task URLs as aliases; there is only one task store.
func (s *Server) handleCloneTasks(w http.ResponseWriter, r *http.Request) error {
	return s.handleTasks(w, r)
}
func (s *Server) handleCloneTaskGet(w http.ResponseWriter, r *http.Request) error {
	return s.handleTaskStatus(w, r)
}
func (s *Server) handleCloneTaskCancel(w http.ResponseWriter, r *http.Request) error {
	return s.handleTaskCancel(w, r)
}
