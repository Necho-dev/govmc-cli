package target

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestProfileResolutionAndConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	t.Setenv("GOVMC_PROFILE", "")
	t.Setenv("ESXI_URL", "https://legacy.invalid")
	t.Setenv("ESXI_USERNAME", "legacy")
	t.Setenv("TEST_ESXI_SECRET", "must-not-appear-in-config")
	p := Profile{URL: "https://test.invalid", Username: "test", PasswordEnv: "TEST_ESXI_SECRET"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := Edit(ctx, path, func(c *Config) error {
				name := fmt.Sprintf("test%d", i)
				c.Profiles[name] = p
				if i == 0 {
					c.Active = name
				}
				return nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	stored, err := os.ReadFile(path)
	if err != nil || bytes.Contains(stored, []byte("must-not-appear-in-config")) {
		t.Fatal("configuration is unreadable or contains a secret")
	}
	c, err := Load(path)
	if err != nil || len(c.Profiles) != 8 {
		t.Fatalf("lost profiles: %+v, %v", c, err)
	}
	active, err := Resolve(path, "")
	if err != nil || active.Name != "test0" || active.password != "must-not-appear-in-config" {
		t.Fatal("active profile or environment credential missing")
	}
	t.Setenv("GOVMC_PROFILE", "missing")
	if _, err := Resolve(path, ""); err == nil {
		t.Fatal("unknown environment profile must not fall back to legacy")
	}
	explicit, err := Resolve(path, "test1")
	if err != nil || explicit.Name != "test1" {
		t.Fatal("explicit profile must win over environment")
	}
}
