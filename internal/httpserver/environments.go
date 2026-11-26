package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/target"
)

// EnvironmentManager routes each request to an immutable, profile-specific
// server. Browser tabs select targets independently; the saved active is unchanged.
// Never swap Server.ESXi: in-flight actions and WebMKS tickets must stay pinned.
type EnvironmentManager struct {
	instance string
	path     string
	initial  target.Target
	base     *Server
	mu       sync.Mutex
	backends map[target.Target]*environmentBackend
	closed   bool
}

type environmentBackend struct {
	server  *Server
	handler http.Handler
}

func NewEnvironmentManager(path string, initial target.Target, base *Server) *EnvironmentManager {
	base.ProfileName = initial.Name
	return &EnvironmentManager{instance: fmt.Sprintf("%x", time.Now().UnixNano()), path: path, initial: initial, base: base, backends: make(map[target.Target]*environmentBackend)}
}

func (m *EnvironmentManager) Close() {
	m.mu.Lock()
	m.closed = true
	clients := make(map[*esxi.Client]bool)
	for _, backend := range m.backends {
		clients[backend.server.ESXi] = true
	}
	clients[m.base.ESXi] = true
	m.backends = make(map[target.Target]*environmentBackend)
	m.mu.Unlock()
	for client := range clients {
		if client != nil {
			target.Close(client)
		}
	}
}

func (m *EnvironmentManager) backend(ctx context.Context, name string) (*environmentBackend, error) {
	selected, err := target.Named(m.path, name)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, fmt.Errorf("environment server is closed")
	}
	if entry := m.backends[selected]; entry != nil {
		m.mu.Unlock()
		return entry, nil
	}
	m.mu.Unlock()
	dial, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := selected.Dial(dial)
	if err != nil {
		return nil, err
	}
	server := &Server{Journal: m.base.Journal, ESXi: client, ProfileName: name, Token: m.base.Token, Listen: m.base.Listen, WebDir: m.base.WebDir, WebFS: m.base.WebFS, Insecure: selected.Insecure, Language: m.base.Language,
		Tasks: m.base.Tasks, WMKS: m.base.WMKS,
	}
	entry := &environmentBackend{server: server, handler: server.Handler()}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		target.Close(client)
		return nil, fmt.Errorf("environment server is closed")
	}
	if existing := m.backends[selected]; existing != nil {
		m.mu.Unlock()
		target.Close(client)
		return existing, nil
	}
	m.backends[selected] = entry
	m.mu.Unlock()
	return entry, nil
}

func (m *EnvironmentManager) wrap(base http.Handler) http.Handler {
	m.mu.Lock()
	m.backends[m.initial] = &environmentBackend{server: m.base, handler: base}
	m.mu.Unlock()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/profiles" && r.Method == http.MethodGet {
			m.list(w, r)
			return
		}
		if r.URL.Path == "/v1/profiles/connect" && r.Method == http.MethodPost {
			m.connect(w, r)
			return
		}
		if r.URL.Path == "/mcp" {
			base.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/tasks") {
			copy := Server{Tasks: m.base.Tasks}
			copy.ProfileName = r.Header.Get("X-Govmc-Profile")
			if copy.ProfileName == "" {
				copy.ProfileName = m.initial.Name
			}
			var err error
			switch {
			case r.URL.Path == "/v1/tasks" && r.Method == http.MethodGet:
				err = copy.handleTasks(w, r)
			case strings.HasSuffix(r.URL.Path, "/cancel") && r.Method == http.MethodPost:
				r.SetPathValue("taskId", strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/cancel"))
				err = copy.handleTaskCancel(w, r)
			case strings.Count(r.URL.Path, "/") == 3 && r.Method == http.MethodGet:
				r.SetPathValue("taskId", strings.TrimPrefix(r.URL.Path, "/v1/tasks/"))
				err = copy.handleTaskStatus(w, r)
			default:
				http.NotFound(w, r)
				return
			}
			if err != nil {
				writeErr(w, err)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/settings/") || r.URL.Path == "/v1/console/capabilities" {
			base.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/mcp" && r.URL.Path != "/ws/console" {
			base.ServeHTTP(w, r)
			return
		}
		name := r.Header.Get("X-Govmc-Profile")
		if r.URL.Path == "/ws/console" {
			name = r.URL.Query().Get("profile")
		}
		if name == "" {
			name = m.initial.Name
		}
		entry, err := m.backend(r.Context(), name)
		if err != nil {
			profileError(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("X-Govmc-Profile", name)
		entry.handler.ServeHTTP(w, r)
	})
}

func profileError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": err.Error()})
}

func (m *EnvironmentManager) list(w http.ResponseWriter, _ *http.Request) {
	config, err := target.Load(m.path)
	if err != nil {
		profileError(w, http.StatusInternalServerError, err)
		return
	}
	type profile struct {
		Name     string `json:"name"`
		URL      string `json:"url"`
		Username string `json:"username"`
	}
	names := make([]string, 0, len(config.Profiles))
	for name := range config.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	profiles := make([]profile, 0, len(names))
	for _, name := range names {
		p := config.Profiles[name]
		profiles = append(profiles, profile{Name: name, URL: p.URL, Username: p.Username})
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = writeJSON(w, map[string]any{"instance": m.instance, "current": m.initial.Name, "active": config.Active, "profiles": profiles})
}

func (m *EnvironmentManager) connect(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Name string `json:"name"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&request); err != nil {
		profileError(w, http.StatusBadRequest, fmt.Errorf("invalid environment selection"))
		return
	}
	if request.Name == "" {
		profileError(w, http.StatusBadRequest, fmt.Errorf("profile name is required"))
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		profileError(w, http.StatusBadRequest, fmt.Errorf("expected one environment selection"))
		return
	}
	if err := journal.Run(r.Context(), journal.Event{Source: "serve", Actor: "anonymous", Profile: request.Name, Operation: "profile.connect"}, func() error { _, err := m.backend(r.Context(), request.Name); return err }); err != nil {
		profileError(w, http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	_ = writeJSON(w, map[string]string{"name": request.Name})
}
