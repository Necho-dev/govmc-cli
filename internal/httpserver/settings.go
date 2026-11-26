package httpserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/journal"
)

type recordedKey struct{}

func (s *Server) instrument(next http.Handler) http.Handler {
	if s.Journal == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(recordedKey{}) != nil || strings.HasPrefix(r.URL.Path, "/v1/settings/") {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/mcp" && r.URL.Path != "/ws/console" {
			next.ServeHTTP(w, r)
			return
		}
		ctx := journal.With(context.WithValue(r.Context(), recordedKey{}, true), s.Journal)
		profile := r.Header.Get("X-Govmc-Profile")
		if profile == "" {
			profile = s.ProfileName
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		operation := r.Method + " api"
		resource := ""
		if len(parts) > 1 {
			operation = r.Method + " " + parts[1]
			if len(parts) > 2 && (parts[1] == "vms" || parts[1] == "tasks") {
				resource = parts[2]
				if len(parts) > 3 {
					operation += "." + parts[3]
				}
			}
		}
		event := journal.Event{Source: "serve", Actor: "anonymous", Profile: profile, Operation: operation, Resource: resource, RequestID: journal.ID()}
		status := http.StatusOK
		response := &observedResponse{ResponseWriter: w, status: &status}
		execute := func() error {
			next.ServeHTTP(response, r.WithContext(ctx))
			if r.Context().Err() != nil {
				return r.Context().Err()
			}
			if status >= 400 {
				if status == 401 || status == 403 {
					return fmt.Errorf("permission denied (HTTP %d)", status)
				}
				return fmt.Errorf("HTTP status %d", status)
			}
			return nil
		}
		if r.URL.Path == "/mcp" {
			_ = execute()
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if err := journal.Run(ctx, event, execute); err != nil && status < 400 {
				if response.written {
					return
				}
				http.Error(w, "audit storage unavailable", http.StatusServiceUnavailable)
			}
		} else {
			begin := time.Now()
			err := execute()
			event.Event = "request.completed"
			event.DurationMS = time.Since(begin).Milliseconds()
			event.Code = journal.ErrorCode(err)
			if err != nil {
				event.Level = "ERROR"
			}
			s.Journal.Log(event)
		}
	})
}

// Unwrap lets net/http's ResponseController preserve streaming and WebSocket hijacking.
type observedResponse struct {
	http.ResponseWriter
	status  *int
	written bool
}

func (w *observedResponse) WriteHeader(code int) {
	if w.written {
		return
	}
	*w.status = code
	w.written = true
	w.ResponseWriter.WriteHeader(code)
}
func (w *observedResponse) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func (w *observedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *observedResponse) Flush()                      { _ = http.NewResponseController(w.ResponseWriter).Flush() }
func (w *observedResponse) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}
func parseJournalQuery(r *http.Request) (journal.Query, error) {
	values := r.URL.Query()
	q := journal.Query{Profile: values.Get("profile"), Source: values.Get("source"), Level: values.Get("level"), Outcome: values.Get("outcome"), Search: values.Get("search"), Limit: 100}
	for key, dest := range map[string]*int{"limit": &q.Limit, "offset": &q.Offset} {
		if v := values.Get(key); v != "" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 0 || key == "limit" && (n == 0 || n > 500) || key == "offset" && n > 100000 {
				return q, errors.New("invalid pagination")
			}
			*dest = n
		}
	}
	for key, dest := range map[string]*time.Time{"since": &q.Since, "until": &q.Until} {
		if v := values.Get(key); v != "" {
			t, e := time.Parse(time.RFC3339, v)
			if e != nil {
				return q, errors.New("invalid time range")
			}
			*dest = t
		}
	}
	if !q.Since.IsZero() && !q.Until.IsZero() && q.Since.After(q.Until) {
		return q, errors.New("invalid time range")
	}
	return q, nil
}
func (s *Server) settingsQuery(w http.ResponseWriter, r *http.Request, audit bool) {
	w.Header().Set("Cache-Control", "no-store")
	if s.Journal == nil {
		http.Error(w, "local journal unavailable", 503)
		return
	}
	q, err := parseJournalQuery(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var result journal.Result
	if audit {
		result, err = s.Journal.Audits(ctx, q)
	} else {
		result, err = s.Journal.Logs(ctx, q)
	}
	if err != nil {
		http.Error(w, "local journal query failed", 503)
		return
	}
	_ = writeJSON(w, result)
}
func (s *Server) handleSettingsLogs(w http.ResponseWriter, r *http.Request) {
	s.settingsQuery(w, r, false)
}
func (s *Server) handleSettingsAudits(w http.ResponseWriter, r *http.Request) {
	s.settingsQuery(w, r, true)
}
func (s *Server) handleSettingsStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.Journal == nil {
		http.Error(w, "local journal unavailable", 503)
		return
	}
	_ = writeJSON(w, s.Journal.Status())
}
