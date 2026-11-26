package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vmware/govmomi/vim25/types"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/tasks"
	"github.com/Necho-dev/govmc-cli/internal/wmks"
	"github.com/Necho-dev/govmc-cli/web/locales"
)

type consoleTicket struct {
	ID        string
	WSS       string
	ExpiresAt time.Time
}

type Server struct {
	WMKS            *wmks.Snapshot
	Journal         *journal.Store
	Profiles        *EnvironmentManager
	ProfileName     string
	Tasks           *tasks.Manager
	Language        string
	StartupOutput   io.Writer
	StartupLanguage string
	ESXi            *esxi.Client
	Token           string
	Listen          string
	WebDir          string
	WebFS           fs.FS
	Insecure        bool
	tickets         sync.Map
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", s.webFS())
	mux.HandleFunc("GET /v1/console/capabilities", s.handleConsoleCapabilities)
	mux.HandleFunc("GET /wmks/", s.handleWMKSAsset)
	mux.HandleFunc("GET /ui-preferences", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_ = writeJSON(w, map[string]string{"language": s.Language})
	})
	mux.HandleFunc("GET /ws/console", s.handleWS)
	mux.HandleFunc("GET /v1/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("GET /v1/settings/logs", s.handleSettingsLogs)
	mux.HandleFunc("GET /v1/settings/audits", s.handleSettingsAudits)
	mux.HandleFunc("GET /v1/settings/status", s.handleSettingsStatus)
	mux.HandleFunc("GET /v1/about", s.wrap(s.handleAbout))
	mux.HandleFunc("GET /v1/vms", s.wrap(s.handleVMList))
	mux.HandleFunc("GET /v1/vms/{id}", s.wrap(s.handleVMGet))
	mux.HandleFunc("GET /v1/vms/{id}/devices", s.wrap(s.handleVMDevices))
	mux.HandleFunc("GET /v1/vms/{id}/screenshot", s.wrap(s.handleScreenshot))
	mux.HandleFunc("GET /v1/vms/{id}/metrics", s.wrap(s.handleVMMetrics))
	mux.HandleFunc("POST /v1/vms/{id}/console", s.wrap(s.handleConsole))
	mux.HandleFunc("POST /v1/vms/{id}/power", s.wrap(s.handlePower))
	mux.HandleFunc("GET /v1/vms/{id}/question", s.wrap(s.handleQuestionGet))
	mux.HandleFunc("POST /v1/vms/{id}/question", s.wrap(s.handleQuestionPost))
	mux.HandleFunc("PATCH /v1/vms/{id}", s.wrap(s.handleVMPatch))
	mux.HandleFunc("GET /v1/vms/{id}/networks", s.wrap(s.handleVMNetworksList))
	mux.HandleFunc("POST /v1/vms/{id}/networks", s.wrap(s.handleVMNetworks))
	mux.HandleFunc("POST /v1/vms/{id}/unregister", s.wrap(s.handleUnregister))
	mux.HandleFunc("POST /v1/vms/register", s.wrap(s.handleRegister))
	mux.HandleFunc("GET /v1/host", s.wrap(s.handleHost))
	mux.HandleFunc("GET /v1/host/metrics", s.wrap(s.handleHostMetrics))
	mux.HandleFunc("GET /v1/networks", s.wrap(s.handleNetworks))
	mux.HandleFunc("GET /v1/datastores/entries", s.wrap(s.handleEntryList))
	mux.HandleFunc("GET /v1/datastores/file/preview", s.wrap(s.handleFilePreview))
	mux.HandleFunc("GET /v1/datastores", s.wrap(s.handleDatastores))
	mux.HandleFunc("POST /v1/datastores/refresh", s.wrap(s.handleDatastoreRefresh))
	mux.HandleFunc("GET /v1/datastores/folder", s.wrap(s.handleFolderList))
	mux.HandleFunc("POST /v1/datastores/folder", s.wrap(s.handleFolderCreate))
	mux.HandleFunc("DELETE /v1/datastores/folder", s.wrap(s.handleFolderDelete))
	mux.HandleFunc("POST /v1/datastores/folder/copy", s.wrap(s.handleFolderCopy))
	mux.HandleFunc("POST /v1/datastores/folder/move", s.wrap(s.handleFolderMove))
	mux.HandleFunc("POST /v1/datastores/folder/rename", s.wrap(s.handleFolderRename))
	mux.HandleFunc("GET /v1/datastores/file", s.wrap(s.handleFileList))
	mux.HandleFunc("GET /v1/datastores/file/content", s.wrap(s.handleFileDownload))
	mux.HandleFunc("PUT /v1/datastores/file", s.wrap(s.handleFileUpload))
	mux.HandleFunc("DELETE /v1/datastores/file", s.wrap(s.handleFileDelete))
	mux.HandleFunc("POST /v1/datastores/file/copy", s.wrap(s.handleFileCopy))
	mux.HandleFunc("POST /v1/datastores/file/move", s.wrap(s.handleFileMove))
	mux.HandleFunc("POST /v1/datastores/file/rename", s.wrap(s.handleFileRename))
	mux.HandleFunc("GET /v1/metrics", s.wrap(s.handleMetricsIndex))
	mux.HandleFunc("GET /v1/metrics/counters", s.wrap(s.handleCounters))
	mux.HandleFunc("POST /v1/custom/clone", s.wrap(s.handleCustomClone))
	mux.HandleFunc("GET /v1/custom/clone/tasks", s.wrap(s.handleCloneTasks))
	mux.HandleFunc("GET /v1/custom/clone/tasks/{taskId}", s.wrap(s.handleCloneTaskGet))
	mux.HandleFunc("POST /v1/custom/clone/tasks/{taskId}/cancel", s.wrap(s.handleCloneTaskCancel))
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "HTTP MCP removed; use govmc mcp serve (stdio)", http.StatusGone)
	})
	mux.HandleFunc("GET /v1/tasks", s.wrap(s.handleTasks))
	mux.HandleFunc("GET /v1/tasks/{taskId}", s.wrap(s.handleTaskStatus))
	mux.HandleFunc("POST /v1/tasks/{taskId}/cancel", s.wrap(s.handleTaskCancel))
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Tasks != nil {
			r = r.WithContext(tasks.With(r.Context(), s.Tasks))
		}
		mux.ServeHTTP(w, r)
	})
	if s.Profiles != nil {
		handler = s.Profiles.wrap(handler)
	}
	return requestsForListen(s.Listen, s.auth(s.instrument(handler)))
}

// ListenAddress requires an explicit IP (or localhost) and a numeric TCP port.
// Wildcard binding is opt-in; callers keep 127.0.0.1:8765 as their default.
func ListenAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid listen address: %w", err)
	}
	if strings.EqualFold(host, "localhost") {
		host = "127.0.0.1"
	}
	if net.ParseIP(host) == nil {
		return "", fmt.Errorf("listen host must be an explicit IP address or localhost")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 {
		return "", fmt.Errorf("listen port must be between 0 and 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(number)), nil
}

func localHost(value string) bool {
	host := value
	if parsed, _, err := net.SplitHostPort(value); err == nil {
		host = parsed
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}

func sameOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) &&
		u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == ""
}

// requestsForListen retains the loopback Host boundary for local listeners.
// Explicit LAN/wildcard listeners accept IP-literal hosts, while preserving
// same-origin checks and rejecting arbitrary DNS Host names (DNS rebinding).
func requestsForListen(listen string, next http.Handler) http.Handler {
	loopback := true
	var bindIP net.IP
	if listen != "" {
		normalized, err := ListenAddress(listen)
		if err != nil {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "invalid server listen configuration", http.StatusServiceUnavailable)
			})
		}
		host, _, _ := net.SplitHostPort(normalized)
		bindIP = net.ParseIP(host)
		loopback = bindIP.IsLoopback()
	}
	allowedHost := func(value string) bool {
		if loopback {
			return localHost(value)
		}
		host := value
		if parsed, _, err := net.SplitHostPort(value); err == nil {
			host = parsed
		}
		if strings.EqualFold(host, "localhost") {
			return bindIP.IsUnspecified()
		}
		ip := net.ParseIP(strings.Trim(host, "[]"))
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return false
		}
		return bindIP.IsUnspecified() || bindIP.Equal(ip)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host) || (r.Header.Get("Origin") != "" && !sameOrigin(r)) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			http.Error(w, "same-origin requests to the server IP only", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) ListenAndServe() error {
	return s.Serve(context.Background())
}

func (s *Server) Serve(ctx context.Context) error {
	address, err := ListenAddress(s.Listen)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	shutdownDone := make(chan struct{})
	if s.StartupOutput != nil {
		_, _ = io.WriteString(s.StartupOutput, startupNotice(ln.Addr().String(), s.StartupLanguage))
	}
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	if s.Journal != nil {
		s.Journal.Log(journal.Event{Event: "server.listening", Resource: ln.Addr().String()})
	} else if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		log.Printf("Listening on %s; access with this machine's IP and port %s", ln.Addr(), port)
	} else {
		log.Printf("Open http://%s", ln.Addr())
	}
	stop := context.AfterFunc(ctx, func() {
		defer close(shutdownDone)
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	})
	defer stop()
	err = server.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		<-shutdownDone
		return nil
	}
	return err
}

func startupNotice(address, language string) string {
	host, port, _ := net.SplitHostPort(address)
	wildcard := false
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		wildcard = true
		host = "127.0.0.1"
		if ip.To4() == nil {
			host = "::1"
		}
	}
	url := "http://" + net.JoinHostPort(host, port)
	message := "\n" + locales.Text(language, "serve.started") + "\n" + fmt.Sprintf(locales.Text(language, "serve.open"), url)
	if wildcard {
		message += fmt.Sprintf(locales.Text(language, "serve.listening"), address) + fmt.Sprintf(locales.Text(language, "serve.lan"), port)
	}
	return message + locales.Text(language, "serve.stop") + "\n\n"
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Token == "" || !needsToken(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.Header.Get("X-API-Token")
		}
		if got != s.Token {
			if s.Journal != nil {
				_ = journal.Run(journal.With(r.Context(), s.Journal), journal.Event{Source: "serve", Actor: "anonymous", Operation: "api.authenticate"}, func() error { return errors.New("permission denied") })
			}
			writeErr(w, &esxi.Error{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "invalid or missing API token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func needsToken(path string) bool {
	return strings.HasPrefix(path, "/v1/") || path == "/mcp"
}

type handlerFunc func(http.ResponseWriter, *http.Request) error

func (s *Server) wrap(h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			writeErr(w, err)
		}
	}
}

func writeErr(w http.ResponseWriter, err error) {
	var api *esxi.Error
	if errors.As(err, &api) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(api.Status)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":     api.Code,
			"message":  api.Message,
			"vimFault": emptyNil(api.VimFault),
		})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": "error", "message": err.Error()})
}

func emptyNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func writeJSON(w http.ResponseWriter, v any) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(v)
}

func (s *Server) ctx(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 120*time.Second)
}

func (s *Server) ctxCopy(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 30*time.Minute)
}

func confirmed(r *http.Request, bodyConfirm bool) bool {
	if bodyConfirm {
		return true
	}
	v := strings.TrimSpace(r.Header.Get("X-Confirm"))
	b, _ := strconv.ParseBool(v)
	return b
}

func (s *Server) issueConsole(w http.ResponseWriter, ticket *types.VirtualMachineTicket) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	wss := esxi.ConsoleURL(ticket, s.ESXi.Reach)
	s.storeTicket(id, wss)
	return writeJSON(w, map[string]string{
		"ws":   s.consolePath(id),
		"note": "open in a browser; this URL is one-shot and expires in 2 minutes. ESXi WSS is never exposed. Without USB Tablet/Tools the mouse may not work (409 is not a ticket failure).",
	})
}

func (s *Server) consolePath(id string) string {
	path := "/ws/console?id=" + url.QueryEscape(id)
	if s.ProfileName != "" {
		path += "&profile=" + url.QueryEscape(s.ProfileName)
	}
	return path
}

func (s *Server) storeTicket(id, wss string) {
	const lifetime = 2 * time.Minute
	s.tickets.Store(id, consoleTicket{ID: id, WSS: wss, ExpiresAt: time.Now().Add(lifetime)})
	time.AfterFunc(lifetime, func() { s.tickets.Delete(id) })
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) || !websocket.IsWebSocketUpgrade(r) {
		http.Error(w, "same-origin WebSocket required", http.StatusForbidden)
		return
	}
	raw, ok := s.tickets.LoadAndDelete(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, "ticket not found or already used", http.StatusGone)
		return
	}
	t := raw.(consoleTicket)
	if time.Now().After(t.ExpiresAt) {
		http.Error(w, "ticket expired", http.StatusGone)
		return
	}
	requested := websocket.Subprotocols(r)
	if len(requested) == 0 {
		requested = []string{"binary", "vmware-vvc"}
	}
	upgrader := websocket.Upgrader{CheckOrigin: sameOrigin, Subprotocols: requested}
	client, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade: %v", err)
		return
	}
	defer client.Close()
	dialer := websocket.Dialer{TLSClientConfig: &tls.Config{InsecureSkipVerify: s.Insecure}, Subprotocols: requested, HandshakeTimeout: 15 * time.Second} //nolint:gosec
	remote, _, err := dialer.DialContext(r.Context(), t.WSS, nil)
	if err != nil {
		log.Printf("ESXi WebMKS dial failed")
		_ = client.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "ESXi WebMKS dial failed"))
		return
	}
	defer remote.Close()
	stop := context.AfterFunc(r.Context(), func() {
		_ = client.Close()
		_ = remote.Close()
	})
	defer stop()
	errc := make(chan error, 2)
	go pump(client, remote, errc)
	go pump(remote, client, errc)
	if err := <-errc; err != nil && !isBenignWS(err) {
		log.Printf("proxy end: %v", err)
	}
}

func pump(src, dst *websocket.Conn, errc chan<- error) {
	for {
		typ, data, err := src.ReadMessage()
		if err != nil {
			errc <- err
			return
		}
		if err := dst.WriteMessage(typ, data); err != nil {
			errc <- err
			return
		}
	}
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func isBenignWS(err error) bool {
	return err == io.EOF || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway)
}

func (s *Server) webFS() http.Handler {
	assets := s.WebFS
	if assets == nil && s.WebDir != "" {
		assets = os.DirFS(s.WebDir)
	}
	if assets == nil {
		return http.NotFoundHandler()
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/v1" || strings.HasPrefix(p, "/v1/") || p == "/mcp" || strings.HasPrefix(p, "/ws/") {
			http.NotFound(w, r)
			return
		}
		if p == "/" || p == "/index.html" || p == "/app.js" || p == "/i18n.js" || p == "/app.css" || p == "/theme.css" || p == "/select.js" || p == "/cache.js" || strings.HasPrefix(p, "/locales/") || strings.HasPrefix(p, "/tasks.") || strings.HasPrefix(p, "/settings.") || strings.HasPrefix(p, "/datastores.") || strings.HasPrefix(p, "/console.") {
			w.Header().Set("Cache-Control", "no-store")
		}
		files.ServeHTTP(w, r)
	})
}

func FindWeb() string {
	candidates := []string{"web"}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "web"), filepath.Join(dir, "..", "web"))
	}
	for _, d := range candidates {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return d
		}
	}
	return ""
}
