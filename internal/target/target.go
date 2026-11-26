package target

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/Necho-dev/govmc-cli/internal/profile/history"
	"github.com/gofrs/flock"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Profile struct {
	ID          string      `json:"id,omitempty" yaml:"id,omitempty"`
	NameSince   int64       `json:"nameSince,omitempty" yaml:"name_since,omitempty"`
	MCP         MCPSettings `json:"mcp,omitempty" yaml:"mcp,omitempty"`
	URL         string      `json:"url" yaml:"url"`
	Username    string      `json:"username" yaml:"username"`
	Password    string      `json:"-" yaml:"password,omitempty"`
	PasswordEnv string      `json:"passwordEnv,omitempty" yaml:"password_env,omitempty"`
	Insecure    bool        `json:"insecure" yaml:"insecure"`
}

type RuntimeLogSettings struct {
	Days   int `json:"days,omitempty" yaml:"days,omitempty"`
	MaxMiB int `json:"maxMiB,omitempty" yaml:"max_mib,omitempty"`
}
type AuditSettings struct {
	Days int `json:"days,omitempty" yaml:"days,omitempty"`
}

type WMKSSettings struct {
	Directory string `json:"directory,omitempty" yaml:"directory,omitempty"`
}

type Config struct {
	WMKS           WMKSSettings       `json:"wmks,omitempty" yaml:"wmks,omitempty"`
	ProfileHistory []history.Binding  `json:"profileHistory,omitempty" yaml:"profile_history,omitempty"`
	RuntimeLogs    RuntimeLogSettings `json:"runtimeLogs,omitempty" yaml:"runtime_logs,omitempty"`
	Audit          AuditSettings      `json:"audit,omitempty" yaml:"audit,omitempty"`
	Version        int                `json:"version,omitempty" yaml:"version"`
	Language       string             `json:"language,omitempty" yaml:"language,omitempty"`
	MCP            MCPSettings        `json:"mcp" yaml:"mcp"`
	Active         string             `json:"active" yaml:"active"`
	LegacyImported bool               `json:"legacyImported,omitempty" yaml:"legacy_imported,omitempty"`
	Profiles       map[string]Profile `json:"profiles" yaml:"profiles"`
}

type Target struct {
	Name string
	Profile
	password   string
	configPath string
}

// WithPassword supplies a password for this process without changing the profile
// file or the environment inherited by child processes.
func (t Target) WithPassword(password string) Target {
	t.password = password
	return t
}

func (t Target) HasPassword() bool { return t.password != "" }
func (t Target) Saved() bool       { return t.configPath != "" }

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
var envPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func Validate(name string, p Profile) error {
	if p.MCP.Mode != "" {
		if err := ValidateMCPMode(p.MCP.Mode); err != nil {
			return err
		}
	}
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q", name)
	}
	u, err := url.Parse(p.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("profile URL must be an HTTPS endpoint without credentials, query or fragment")
	}
	if strings.TrimSpace(p.Username) == "" {
		return errors.New("profile requires a username")
	}
	if p.PasswordEnv != "" && !envPattern.MatchString(p.PasswordEnv) {
		return errors.New("invalid legacy password environment variable")
	}
	if p.Password != "" && !strings.HasPrefix(p.Password, secretPrefix) {
		return errors.New("password must be encrypted; use govmc profiles add/update")
	}
	return nil
}

func Path() (string, error) {
	if p := os.Getenv("GOVMC_CONFIG"); p != "" {
		return filepath.Abs(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".govmc", "config.yaml")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		oldDir, dirErr := os.UserConfigDir()
		if dirErr != nil {
			return "", dirErr
		}
		if err := migrateLegacyConfig(path, filepath.Join(oldDir, "govmc", "profiles.json")); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	return path, nil
}

func Load(path string) (Config, error) {
	c := Config{Version: 1, MCP: MCPSettings{Mode: MCPReadonly}, Profiles: map[string]Profile{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
		err = json.Unmarshal(data, &c)
	} else {
		decoder := yaml.NewDecoder(bytes.NewReader(data))
		decoder.KnownFields(true)
		err = decoder.Decode(&c)
		if err == nil {
			if next := decoder.Decode(new(any)); next != io.EOF {
				err = errors.New("expected one YAML configuration document")
			}
		}
	}
	if err != nil {
		return c, errors.New("invalid profile configuration; expected version 1 YAML")
	}
	if c.Version != 0 && c.Version != 1 {
		return c, errors.New("unsupported profile configuration version")
	}
	c.Version = 1
	if c.MCP.Mode == "" {
		c.MCP.Mode = MCPReadonly
	}
	if err := ValidateMCPMode(c.MCP.Mode); err != nil {
		return c, err
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	for name, p := range c.Profiles {
		if p.ID == "" {
			p.ID = history.LegacyID(name, p.URL, p.Username)
			c.Profiles[name] = p
		}
		if p.MCP.Mode != "" {
			if err := ValidateMCPMode(p.MCP.Mode); err != nil {
				return c, err
			}
		}
	}
	if err := validateProfileHistory(c); err != nil {
		return c, err
	}
	return c, nil
}

// Migration copies metadata and encrypts available legacy credentials. The old
// file is retained; subsequent writes use only the new YAML configuration.
func migrateLegacyConfig(path, legacyPath string) error {
	if _, err := os.Stat(legacyPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	old, err := Load(legacyPath)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return Edit(ctx, path, func(c *Config) error {
		if len(c.Profiles) > 0 {
			return nil
		}
		for name, p := range old.Profiles {
			c.Profiles[name] = p
		}
		c.Active, c.Language = old.Active, old.Language
		c.LegacyImported = true
		return nil
	})
}

func Edit(ctx context.Context, path string, change func(*Config) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	lock := flock.New(path + ".lock")
	defer lock.Close()
	locked, err := lock.TryLockContext(ctx, 30*time.Millisecond)
	if err != nil {
		return fmt.Errorf("lock configuration: %w", err)
	}
	if !locked {
		return errors.New("could not lock configuration")
	}
	c, err := Load(path)
	if err != nil {
		return err
	}
	original := make(map[string]Profile, len(c.Profiles))
	for name, p := range c.Profiles {
		original[name] = p
	}
	if err := change(&c); err != nil {
		return err
	}
	if c.MCP.Mode == "" {
		c.MCP.Mode = MCPReadonly
	}
	if err := ValidateMCPMode(c.MCP.Mode); err != nil {
		return err
	}
	for name, p := range c.Profiles {
		if p.ID == "" {
			if old, ok := original[name]; ok {
				p.ID, p.NameSince = old.ID, old.NameSince
			} else {
				p.NameSince = time.Now().UnixMilli()
				var e error
				p.ID, e = history.NewID()
				if e != nil {
					return e
				}
			}
		}
		for n, b := range c.ProfileHistory {
			if b.Name == name && b.ID != p.ID && b.Until == 0 {
				c.ProfileHistory[n].Until = p.NameSince
			}
		}
		if err := Validate(name, p); err != nil {
			return err
		}
		if p.Password == "" && p.PasswordEnv != "" {
			if secret := LegacyEnv(p.PasswordEnv); secret != "" {
				if err := SetPassword(path, name, &p, secret); err != nil {
					return err
				}
			}
		} else if prior, ok := original[name]; ok && p.Password != "" && prior.Password == p.Password && (prior.URL != p.URL || prior.Username != p.Username || prior.Insecure != p.Insecure) {
			secret, err := decryptPassword(path, name, prior)
			if err != nil {
				return err
			}
			if err := SetPassword(path, name, &p, secret); err != nil {
				return err
			}
		}
		c.Profiles[name] = p
	}
	if err := validateProfileHistory(c); err != nil {
		return err
	}
	c.Version = 1
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return atomicPrivateWrite(path, data)
}

func Resolve(path, explicit string) (Target, error) {
	name := explicit
	if name == "" {
		name = os.Getenv("GOVMC_PROFILE")
	}
	if name != "" {
		return Named(path, name)
	}
	c, err := Load(path)
	if err != nil {
		return Target{}, err
	}
	if c.Active != "" {
		return fromConfig(path, c, c.Active)
	}
	if len(c.Profiles) > 0 || c.LegacyImported {
		return Target{}, errors.New("no default environment selected: run govmc profiles use NAME")
	}
	rawURL, user := LegacyEnv("ESXI_URL"), LegacyEnv("ESXI_USERNAME")
	if rawURL == "" || user == "" {
		return Target{}, errors.New("no target selected: use govmc profiles add and govmc profiles use")
	}
	insecure := true
	if v := LegacyEnv("ESXI_INSECURE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			insecure = b
		}
	}
	secret := LegacyEnv("ESXI_PASSWORD")
	if secret == "" {
		return Target{}, errors.New("no saved password; add an environment with govmc profiles add")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = Edit(ctx, path, func(config *Config) error {
		if _, exists := config.Profiles["legacy"]; exists {
			return errors.New("select a saved environment with govmc profiles use")
		}
		p := Profile{URL: rawURL, Username: user, Insecure: insecure}
		if err := SetPassword(path, "legacy", &p, secret); err != nil {
			return err
		}
		config.Profiles["legacy"], config.Active = p, "legacy"
		config.LegacyImported = true
		return nil
	})
	if err != nil {
		return Target{}, err
	}
	return Named(path, "legacy")
}

func Named(path, name string) (Target, error) {
	c, err := Load(path)
	if err != nil {
		return Target{}, err
	}
	return fromConfig(path, c, name)
}

func fromConfig(path string, c Config, name string) (Target, error) {
	p, ok := c.Profiles[name]
	if !ok {
		return Target{}, fmt.Errorf("unknown profile %q", name)
	}
	if err := Validate(name, p); err != nil {
		return Target{}, err
	}
	password := ""
	if p.Password != "" {
		var err error
		password, err = decryptPassword(path, name, p)
		if err != nil {
			return Target{}, err
		}
	} else if p.PasswordEnv != "" {
		password = LegacyEnv(p.PasswordEnv)
		if password != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := Edit(ctx, path, func(c *Config) error { return nil })
			if err != nil {
				return Target{}, err
			}
			return Named(path, name)
		}
	}
	return Target{Name: name, Profile: p, password: password, configPath: path}, nil
}

func (t Target) Dial(ctx context.Context) (*esxi.Client, error) {
	if t.password == "" {
		return nil, fmt.Errorf("password is not saved for profile %q; run govmc profiles update %s --password-stdin", t.Name, t.Name)
	}
	c, err := esxi.Dial(ctx, t.URL, t.Username, t.password, t.Insecure)
	if err != nil {
		msg := err.Error()
		if t.password != "" {
			msg = strings.NewReplacer(t.password, "[redacted]", url.QueryEscape(t.password), "[redacted]", url.PathEscape(t.password), "[redacted]").Replace(msg)
		}
		return nil, fmt.Errorf("connect to profile %q: %s", t.Name, msg)
	}
	return c, nil
}

func LegacyEnv(key string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	env, _ := godotenv.Read()
	return env[key]
}

func Close(client *esxi.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = client.Gov.Logout(ctx)
}
