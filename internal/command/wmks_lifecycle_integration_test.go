package command

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/Necho-dev/govmc-cli/internal/wmks"
	"github.com/vmware/govmomi/simulator"
)

// Full setup uses encrypted temporary profiles, authenticated HTTP, real child
// processes and local ESXi fixtures. No real SDK or production ESXi is required.
func TestBuiltServeWMKSFullLifecycle(t *testing.T) {
	binary := os.Getenv("GOVMC_SERVE_BINARY")
	if binary == "" {
		t.Skip("set GOVMC_SERVE_BINARY for local-process acceptance")
	}
	binary, e := filepath.Abs(binary)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	config := filepath.Join(root, "configuration", "config.yaml")
	model := simulator.ESX()
	if e = model.Create(); e != nil {
		t.Fatal(e)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	service := model.Service.NewServer()
	defer service.Close()
	endpoint := *service.URL
	user := endpoint.User.Username()
	password, _ := endpoint.User.Password()
	endpoint.User = nil
	if e = target.Edit(context.Background(), config, func(c *target.Config) error {
		p := target.Profile{URL: endpoint.String(), Username: user, Insecure: true}
		if e := target.SetPassword(config, "lifecycle", &p, password); e != nil {
			return e
		}
		c.Profiles["lifecycle"] = p
		c.Active = "lifecycle"
		c.WMKS.Directory = "custom-sdk"
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	cli := func(wantError bool, args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, binary, append([]string{"--config", config}, args...)...)
		c.Dir = root
		c.Env = append(os.Environ(), "GOVMC_CONFIG=", "GOVMC_PROFILE=", "GOVMC_LANG=en")
		var out, stderr bytes.Buffer
		c.Stdout = &out
		c.Stderr = &stderr
		e := c.Run()
		if (e != nil) != wantError {
			t.Fatalf("%v: error=%v stdout=%s stderr=%s", args, e, out.String(), stderr.String())
		}
		if wantError {
			return out.String() + stderr.String()
		}
		return out.String()
	}
	status := func() wmks.Status {
		t.Helper()
		var v wmks.Status
		if e := json.Unmarshal([]byte(cli(false, "wmks", "status", "--json")), &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	sdk := func(suffix string) string {
		t.Helper()
		p := filepath.Join(root, "SDK"+suffix+".zip")
		f, e := os.Create(p)
		if e != nil {
			t.Fatal(e)
		}
		z := zip.NewWriter(f)
		for p, b := range map[string]string{"package/wmks.min.js": "/* WMKS fixture */ window.wmks={}; // " + suffix, "package/css/wmks-all.css": "body{}", "package/img/touch_sprite.png": "png", "package/img/touch_sprite_feedback.png": "png", "LICENSE.txt": "local test fixture notice"} {
			w, e := z.Create(p)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = w.Write([]byte(b)); e != nil {
				t.Fatal(e)
			}
		}
		if e = z.Close(); e != nil {
			t.Fatal(e)
		}
		if e = f.Close(); e != nil {
			t.Fatal(e)
		}
		return p
	}
	package1, package2 := sdk("1"), sdk("2")
	if v := status(); v.State != "missing" {
		t.Fatal(v)
	}
	cli(true, "wmks", "verify", "--json")
	cli(false, "wmks", "install", "--file", package1, "--dry-run", "--json")
	if _, e = os.Stat(filepath.Join(filepath.Dir(config), "wmks", "state.json")); !os.IsNotExist(e) {
		t.Fatal("preview changed installation state")
	}
	if out := cli(true, "wmks", "install", "--file", package1); !strings.Contains(out, "--yes") {
		t.Fatal("noninteractive confirmation was not enforced", out)
	}
	if v := status(); v.State != "missing" {
		t.Fatal("unconfirmed install changed state", v)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	base := ""
	var stop func()
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	request := func(p string, auth bool) (int, []byte) {
		t.Helper()
		r, e := http.NewRequest("GET", base+p, nil)
		if e != nil {
			t.Fatal(e)
		}
		if auth {
			r.Header.Set("Authorization", "Bearer lifecycle-token")
		}
		response, e := client.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		body, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, body
	}
	caps := func() wmks.Status {
		t.Helper()
		code, b := request("/v1/console/capabilities", true)
		var v wmks.Status
		if e := json.Unmarshal(b, &v); e != nil || code != 200 {
			t.Fatal(code, string(b), e)
		}
		return v
	}
	ordinary := func() {
		t.Helper()
		for _, p := range []string{"/", "/console.html", "/datastores.html", "/settings.html", "/v1/host", "/v1/vms", "/v1/datastores", "/v1/settings/logs", "/v1/settings/audits"} {
			if code, b := request(p, true); code != 200 {
				t.Fatalf("ordinary Web %s: %d %s", p, code, b)
			}
		}
		cli(false, "vm", "list", "--json")
	}
	override := ""
	start := func() {
		t.Helper()
		if stop != nil {
			t.Fatal("server already running")
		}
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		address := l.Addr().String()
		l.Close()
		base = "http://" + address
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		argv := []string{"--config", config, "serve", "--listen", address}
		if override != "" {
			argv = append(argv, "--wmks", override)
		}
		c := exec.CommandContext(ctx, binary, argv...)
		c.Dir = root
		c.Env = append(os.Environ(), "GOVMC_CONFIG=", "GOVMC_WEB_DIR=", "GOVMC_PROFILE=", "GOVMC_LANG=en", "API_TOKEN=lifecycle-token")
		var logs bytes.Buffer
		c.Stdout = &logs
		c.Stderr = &logs
		if e = c.Start(); e != nil {
			cancel()
			t.Fatal(e)
		}
		stop = func() {
			_ = c.Process.Signal(os.Interrupt)
			err := c.Wait()
			cancel()
			stop = nil
			if err != nil {
				t.Errorf("serve stop: %v %s", err, logs.String())
			}
		}
		deadline := time.Now().Add(10 * time.Second)
		for {
			r, e := client.Get(base + "/settings.html")
			if e == nil {
				r.Body.Close()
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("serve startup timeout")
			}
			time.Sleep(40 * time.Millisecond)
		}
	}

	start()
	ordinary()
	if v := caps(); v.Available || v.State != "missing" {
		t.Fatal(v)
	}
	if code, _ := request("/v1/console/capabilities", false); code != 401 {
		t.Fatal("capability endpoint bypassed configured authentication")
	}
	cli(false, "wmks", "install", "--file", package1, "--yes", "--json")
	first := status()
	if !first.Available {
		t.Fatal(first)
	}
	if v := caps(); v.Available || v.State != "missing" {
		t.Fatal("refresh hot-loaded SDK without restarting", v)
	}
	ordinary()
	stop()
	start()
	if v := caps(); !v.Available || v.Digest != first.Digest {
		t.Fatal("restart did not adopt installed SDK", v)
	}
	ordinary()
	cli(false, "wmks", "verify", "--json")
	if code, b := request(caps().BaseURL+"wmks.min.js", false); code != 200 || !strings.Contains(string(b), "fixture") {
		t.Fatal("runtime SDK not served")
	}
	for _, p := range []string{"receipt.json", "notices/LICENSE.txt", "../../config.key"} {
		if code, _ := request(caps().BaseURL+p, false); code == 200 {
			t.Fatal("private resource served", p)
		}
	}
	if out := cli(true, "wmks", "uninstall", "--yes"); !strings.Contains(out, "wmks_in_use") {
		t.Fatal(out)
	}
	cli(false, "wmks", "install", "--file", package2, "--yes", "--json")
	second := status()
	if second.Digest == first.Digest {
		t.Fatal("update did not change content digest")
	}
	if v := caps(); v.Digest != first.Digest {
		t.Fatal("running SDK changed mid-session", v)
	}
	stop()
	start()
	if v := caps(); v.Digest != second.Digest {
		t.Fatal("restart did not adopt update", v)
	}
	stop()
	override = filepath.Join(filepath.Dir(config), "custom-sdk", "sdk", second.Digest)
	start()
	if !caps().Available {
		t.Fatal("extracted SDK override was not loaded")
	}
	ordinary()
	stop()
	override = filepath.Join(root, "nonexistent-sdk")
	start()
	if caps().Available {
		t.Fatal("override failed to take precedence")
	}
	ordinary()
	stop()
	override = ""
	start()
	if !caps().Available {
		t.Fatal("override changed saved configuration")
	}
	stop()
	// A corrupt on-disk SDK must not prevent ordinary Web from starting.
	sdkPath := filepath.Join(filepath.Dir(config), "custom-sdk", "sdk", second.Digest, "wmks.min.js")
	if e = os.WriteFile(sdkPath, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if out := cli(true, "wmks", "verify", "--json"); !strings.Contains(out, "invalid") {
		t.Fatal(out)
	}
	start()
	ordinary()
	if v := caps(); v.Available || v.State != "invalid" {
		t.Fatal("corrupt SDK did not degrade safely", v)
	}
	stop()
	cli(false, "wmks", "uninstall", "--yes")
	cli(false, "wmks", "install", "--file", package2, "--yes", "--json")
	cli(false, "wmks", "verify", "--json")
	start()
	if !caps().Available {
		t.Fatal("repair did not restore component")
	}
	stop()
	cli(false, "wmks", "uninstall", "--yes")
	start()
	ordinary()
	if v := caps(); v.Available || v.State != "missing" {
		t.Fatal("uninstall did not restore missing state", v)
	}
	stop()
	t.Log(fmt.Sprintf("validated missing -> preview -> confirmed install -> restart -> update -> restart -> corruption -> repair -> uninstall, config=%s", filepath.Base(config)))
}
