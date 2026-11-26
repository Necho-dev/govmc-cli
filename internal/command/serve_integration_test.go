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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/target"
	"github.com/vmware/govmomi/simulator"
)

// Runs the built binary against a local simulator with disposable credentials.
func TestBuiltServeStartsFromSourceCheckout(t *testing.T) { testBuiltServe(t, false) }
func TestBuiltServeWithInstalledWMKS(t *testing.T)        { testBuiltServe(t, true) }
func testBuiltServe(t *testing.T, installed bool) {
	binary := os.Getenv("GOVMC_SERVE_BINARY")
	if binary == "" {
		t.Skip("set GOVMC_SERVE_BINARY to test the built binary with local listeners")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	model := simulator.ESX()
	if err := model.Create(); err != nil {
		t.Fatal(err)
	}
	defer model.Remove()
	model.Service.TLS = &tls.Config{}
	service := model.Service.NewServer()
	defer service.Close()
	endpoint := *service.URL
	username := endpoint.User.Username()
	password, _ := endpoint.User.Password()
	endpoint.User = nil
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := target.Edit(context.Background(), configPath, func(config *target.Config) error {
		p := target.Profile{URL: endpoint.String(), Username: username, Insecure: true}
		if err := target.SetPassword(configPath, "fixture", &p, password); err != nil {
			return err
		}
		config.Profiles["fixture"] = p
		config.Active = "fixture"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var sdkDigest string
	sdkCLI := func(args ...string) ([]byte, error) {
		c := exec.Command(binary, args...)
		c.Dir = filepath.Dir(configPath)
		c.Env = append(os.Environ(), "GOVMC_CONFIG="+configPath)
		return c.CombinedOutput()
	}
	if installed {
		archive := filepath.Join(t.TempDir(), "sdk.zip")
		f, e := os.Create(archive)
		if e != nil {
			t.Fatal(e)
		}
		z := zip.NewWriter(f)
		for name, body := range map[string]string{"wmks.min.js": "/* WMKS fixture */ window.wmks={};", "css/wmks-all.css": "body{}", "img/touch_sprite.png": "png", "img/touch_sprite_feedback.png": "png"} {
			w, e := z.Create(name)
			if e != nil {
				t.Fatal(e)
			}
			_, _ = w.Write([]byte(body))
		}
		z.Close()
		f.Close()
		if out, e := sdkCLI("wmks", "install", "--file", archive, "--yes", "--json"); e != nil {
			t.Fatal(string(out), e)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "serve", "--listen", address)
	command.Dir = t.TempDir()
	command.Env = append(os.Environ(), "GOVMC_CONFIG="+configPath, "GOVMC_WEB_DIR=")
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = command.Process.Signal(os.Interrupt)
		_ = command.Wait()
		if !strings.Contains(output.String(), "http://"+address) || !strings.Contains(output.String(), "Ctrl+C") {
			t.Error("missing readable startup URL", output.String())
		}
		if !strings.Contains(output.String(), "[SERVER]") || !strings.Contains(output.String(), "address="+address) || strings.Contains(output.String(), "\x1b[") || strings.Contains(output.String(), `"event":`) {
			t.Error("unexpected terminal log presentation", output.String())
		}
		if installed {
			if out, e := sdkCLI("wmks", "uninstall", "--yes"); e != nil {
				t.Error("SDK lease not released on shutdown", string(out), e)
			}
		}
		if t.Failed() {
			t.Log(output.String())
		}
	}()
	client := &http.Client{Timeout: time.Second}
	base := "http://" + address
	deadline := time.Now().Add(10 * time.Second)
	for {
		response, err := client.Get(base + "/settings.html")
		if err == nil {
			body, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if response.StatusCode != 200 || !strings.Contains(string(body), "settings.js") {
				t.Fatalf("settings page: %d %s", response.StatusCode, body)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("serve did not start:", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	responseCaps, e := client.Get(base + "/v1/console/capabilities")
	if e != nil {
		t.Fatal(e)
	}
	var caps struct {
		Available bool   `json:"available"`
		State     string `json:"state"`
		Digest    string `json:"digest"`
		BaseURL   string `json:"baseURL"`
	}
	e = json.NewDecoder(responseCaps.Body).Decode(&caps)
	responseCaps.Body.Close()
	if e != nil || caps.Available != installed {
		t.Fatal("incorrect SDK capability", caps, e)
	}
	if installed {
		sdkDigest = caps.Digest
		response, e := client.Get(base + caps.BaseURL + "wmks.min.js")
		if e != nil {
			t.Fatal(e)
		}
		b, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !strings.Contains(string(b), "WMKS fixture") {
			t.Fatal("installed SDK not served")
		}
		response, e = client.Get(base + caps.BaseURL + "receipt.json")
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatal("private SDK receipt exposed")
		}
		if out, e := sdkCLI("wmks", "uninstall", "--yes"); e == nil || !strings.Contains(string(out), "wmks_in_use") {
			t.Fatal("live SDK removed", string(out), e)
		}
	} else if caps.State != "missing" {
		t.Fatal("missing SDK did not preserve ordinary Web")
	}
	_ = sdkDigest
	for _, path := range []string{"/v1/console/capabilities", "/", "/console.html", "/datastores.html", "/datastores.js", "/datastores.css", "/settings.css", "/settings.js", "/v1/settings/logs", "/v1/settings/audits", "/v1/host"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(fmt.Sprintf("%s: %d", path, response.StatusCode))
		}
	}
	response, err := client.Get(base + "/v1/datastores")
	if err != nil {
		t.Fatal(err)
	}
	var stores []struct {
		Name string `json:"name"`
	}
	err = json.NewDecoder(response.Body).Decode(&stores)
	response.Body.Close()
	if err != nil || len(stores) == 0 {
		t.Fatal("missing simulator stores", err)
	}
	response, err = client.Get(base + "/v1/datastores/entries?ds=" + url.QueryEscape(stores[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("combined entries", response.StatusCode)
	}
	response, err = client.Get(base + "/v1/datastores/file/preview?ds=" + url.QueryEscape(stores[0].Name) + "&path=disk.vmdk")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 415 {
		t.Fatal("preview format guard", response.StatusCode)
	}

}
