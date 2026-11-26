package tui

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/vmware/govmomi/simulator"
)

// A refresh or page change during a slow connection must keep the requested
// profile, rather than canceling it and silently reconnecting to the old one.
func TestTUIProfileSwitchSurvivesNavigation(t *testing.T) {
	if os.Getenv("GOVMC_TUI_SIM_TEST") != "1" {
		t.Skip("set GOVMC_TUI_SIM_TEST=1 to run the local simulator")
	}
	model := simulator.ESX()
	model.Machine = 1
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	backend := model.Service.NewServer()
	defer backend.Close()
	endpoint := *backend.URL
	user := endpoint.User.Username()
	password, _ := endpoint.User.Password()
	endpoint.User = nil
	proxyEndpoint := endpoint
	proxyEndpoint.Path = ""
	proxy := httputil.NewSingleHostReverseProxy(&proxyEndpoint)
	proxy.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	delayed := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			proxy.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	}))
	defer delayed.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), configPath, func(c *target.Config) error {
		for name, address := range map[string]string{"a": endpoint.String(), "b": delayed.URL + "/sdk"} {
			p := target.Profile{URL: address, Username: user, Insecure: true}
			if err := target.SetPassword(configPath, name, &p, password); err != nil {
				return err
			}
			c.Profiles[name] = p
		}
		c.Active = "a"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	initial, err := target.Named(configPath, "a")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	screen := tcell.NewSimulationScreen("UTF-8")
	app := tview.NewApplication().SetScreen(screen)
	done := make(chan error, 1)
	go func() { done <- runTUIApplication(ctx, configPath, initial, "en", app) }()
	defer func() {
		cancel()
		app.Stop()
		unblock()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("TUI did not stop")
		}
	}()
	read := func() string {
		var out strings.Builder
		app.QueueUpdateDraw(func() {
			cells, _, _ := screen.GetContents()
			for _, c := range cells {
				out.WriteString(string(c.Runes))
			}
		})
		return out.String()
	}
	wait := func(needle string) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if strings.Contains(read(), needle) {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("screen missing %q", needle)
	}
	key := func(key tcell.Key, r rune) { screen.PostEventWait(tcell.NewEventKey(key, r, tcell.ModNone)) }
	wait("Overview")
	app.QueueUpdateDraw(func() { screen.SetSize(128, 40) })
	key(tcell.KeyRune, '2')
	wait("ha-host_VM0")
	key(tcell.KeyRune, 'u')
	wait("Switch environment")
	key(tcell.KeyDown, 0)
	key(tcell.KeyEnter, 0)
	wait("Switch profile from a to b?")
	key(tcell.KeyRune, 'y')
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("switch did not dial selected profile")
	}
	// Both actions used to call load(current), discarding the chosen profile.
	key(tcell.KeyRune, '1')
	key(tcell.KeyRune, 'r')
	_ = read() // Process both keys before releasing the connection.
	unblock()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		saved, _ := target.Load(configPath)
		if saved.Active == "b" {
			wait(strings.TrimPrefix(delayed.URL, "https://"))
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("navigation canceled profile switch: active profile is still a; screen: %s", read())
}
