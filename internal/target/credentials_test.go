package target

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEncryptedProfilesSurviveEnvironmentAndEndpointChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "govmc", "config.yaml")
	secret := "credential-with-$-and-newline\nvalue"
	save := func(name, url, password string) {
		t.Helper()
		err := Edit(context.Background(), path, func(c *Config) error {
			p := Profile{URL: url, Username: "root"}
			if err := SetPassword(path, name, &p, password); err != nil {
				return err
			}
			c.Profiles[name] = p
			if c.Active == "" {
				c.Active = name
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	save("lab", "https://lab.example", secret)
	save("prod", "https://prod.example", "other-secret")
	t.Setenv("GOVMC_PROFILE", "")
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(secret)) || !bytes.Contains(stored, []byte("enc:v1:")) || !bytes.HasPrefix(stored, []byte("version: 1")) {
		t.Fatal("configuration is not encrypted YAML")
	}
	for _, name := range []string{path, keyPath(path)} {
		info, err := os.Stat(name)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private file permissions not enforced")
		}
	}
	active, err := Resolve(path, "")
	if err != nil || active.password != secret || !active.Saved() {
		t.Fatal("saved credential did not resolve")
	}
	temporary, err := Resolve(path, "prod")
	if err != nil || temporary.Name != "prod" {
		t.Fatal("temporary profile did not resolve")
	}
	config, _ := Load(path)
	if config.Active != "lab" {
		t.Fatal("--profile changed default")
	}
	raw, _ := json.Marshal(config)
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte(secretPrefix)) {
		t.Fatal("profile inspection exposes credentials")
	}
	err = Edit(context.Background(), path, func(c *Config) error {
		p := c.Profiles["lab"]
		p.URL = "https://new.example"
		p.Username = "operator"
		p.Insecure = true
		c.Profiles["lab"] = p
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := Named(path, "lab")
	if err != nil || updated.password != secret {
		t.Fatal("endpoint edit lost encrypted credential")
	}
	// Moving ciphertext to another profile must fail authentication.
	err = Edit(context.Background(), path, func(c *Config) error {
		copy := c.Profiles["lab"]
		copy.ID, copy.NameSince = "", 0 // New environment; only the ciphertext is intentionally copied.
		c.Profiles["wrong"] = copy
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Named(path, "wrong"); err == nil {
		t.Fatal("ciphertext was transferable to a different identity")
	}
}

func TestMissingOrInsecureKeyDoesNotRegenerate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Edit(context.Background(), path, func(c *Config) error {
		p := Profile{URL: "https://lab.example", Username: "root"}
		if err := SetPassword(path, "lab", &p, "secret"); err != nil {
			return err
		}
		c.Profiles["lab"] = p
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(keyPath(path), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Named(path, "lab"); err == nil {
		t.Fatal("insecure key accepted")
	}
	if err := os.Remove(keyPath(path)); err != nil {
		t.Fatal(err)
	}
	if _, err := Named(path, "lab"); err == nil {
		t.Fatal("missing key accepted")
	}
	p := Profile{URL: "https://lab.example", Username: "root"}
	if err := SetPassword(path, "lab", &p, "new-secret"); err == nil {
		t.Fatal("missing key was silently regenerated")
	}
	if _, err := os.Stat(keyPath(path)); !os.IsNotExist(err) {
		t.Fatal("key regenerated")
	}
	if err := os.WriteFile(keyPath(path), key, 0600); err != nil {
		t.Fatal(err)
	}
	original, err := Named(path, "lab")
	if err != nil || original.password != "secret" {
		t.Fatal("restored key did not recover credential")
	}
}

func TestLegacyConfigMigrationEncryptsAndKeepsOriginal(t *testing.T) {
	directory := t.TempDir()
	old := filepath.Join(directory, "profiles.json")
	path := filepath.Join(directory, "new", "config.yaml")
	t.Setenv("TEST_GOVMC_MIGRATE_PASSWORD", "migration-secret")
	data := `{"active":"lab","language":"zh","profiles":{"lab":{"url":"https://lab.example","username":"root","passwordEnv":"TEST_GOVMC_MIGRATE_PASSWORD","insecure":true}}}`
	if err := os.WriteFile(old, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateLegacyConfig(path, old); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_GOVMC_MIGRATE_PASSWORD", "")
	selected, err := Named(path, "lab")
	if err != nil || selected.password != "migration-secret" {
		t.Fatal("migration still depends on environment")
	}
	config, _ := Load(path)
	if config.Active != "lab" || config.Language != "zh" || config.Profiles["lab"].PasswordEnv != "" {
		t.Fatal("migration lost settings")
	}
	original, _ := os.ReadFile(old)
	if string(original) != data {
		t.Fatal("migration changed source")
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "migration-secret") {
		t.Fatal("migration stored plaintext")
	}
}

func TestMalformedYAMLDoesNotExposePlaintext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("profiles: [secret-value"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || strings.Contains(err.Error(), "secret-value") {
		t.Fatal("YAML error leaked input")
	}
}

func TestUpdateProfileRenamesWithoutLosingPasswordAndRejectsStaleEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var original Profile
	if err := Edit(context.Background(), path, func(config *Config) error {
		original = Profile{URL: "https://old.example", Username: "root"}
		if err := SetPassword(path, "lab", &original, "original-secret"); err != nil {
			return err
		}
		config.Profiles["lab"] = original
		config.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	replacement := Profile{URL: "https://new.example", Username: "operator", Insecure: true}
	if err := UpdateProfile(context.Background(), path, "lab", "renamed", original, replacement, ""); err != nil {
		t.Fatal(err)
	}
	renamed, err := Named(path, "renamed")
	if err != nil || renamed.password != "original-secret" || renamed.URL != replacement.URL || !renamed.Insecure {
		t.Fatal("rename lost connection settings or credential")
	}
	config, _ := Load(path)
	if config.Active != "renamed" {
		t.Fatal("active profile did not follow rename")
	}
	if _, exists := config.Profiles["lab"]; exists {
		t.Fatal("old profile retained")
	}
	stale := config.Profiles["renamed"]
	if err := UpdateProfile(context.Background(), path, "renamed", "renamed", stale, replacement, "rotated-secret"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateProfile(context.Background(), path, "renamed", "renamed", stale, replacement, "overwrite"); err == nil {
		t.Fatal("concurrent credential edit was overwritten")
	}
	latest, err := Named(path, "renamed")
	if err != nil || latest.password != "rotated-secret" {
		t.Fatal("password replacement failed")
	}
}

func TestDeletedProfileDoesNotReimportLegacyEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	var original Profile
	if err := Edit(context.Background(), path, func(config *Config) error {
		original = Profile{URL: "https://lab.example", Username: "root"}
		if err := SetPassword(path, "lab", &original, "secret"); err != nil {
			return err
		}
		config.Profiles["lab"] = original
		config.Active = "lab"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Insecure = true
	if err := DeleteProfile(context.Background(), path, "lab", changed); err == nil {
		t.Fatal("stale confirmation was accepted")
	}
	if err := DeleteProfile(context.Background(), path, "lab", original); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOVMC_PROFILE", "")
	t.Setenv("ESXI_URL", "https://legacy.example")
	t.Setenv("ESXI_USERNAME", "root")
	t.Setenv("ESXI_PASSWORD", "secret")
	if _, err := Resolve(path, ""); err == nil {
		t.Fatal("legacy credentials were reimported after explicit deletion")
	}
}
