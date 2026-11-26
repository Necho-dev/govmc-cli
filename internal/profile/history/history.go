// Package history resolves durable environment ownership independently of display names.
package history

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Binding struct {
	Name  string `json:"name" yaml:"name"`
	ID    string `json:"id" yaml:"id"`
	From  int64  `json:"from,omitempty" yaml:"from,omitempty"`
	Until int64  `json:"until,omitempty" yaml:"until,omitempty"`
}
type Profile struct {
	ID        string `json:"id" yaml:"id"`
	NameSince int64  `json:"nameSince" yaml:"name_since"`
	URL       string `json:"url" yaml:"url"`
	Username  string `json:"username" yaml:"username"`
}
type Index struct {
	invalid  bool
	Profiles map[string]Profile `yaml:"profiles"`
	History  []Binding          `json:"profileHistory" yaml:"profile_history"`
}

func LegacyID(name, endpoint, username string) string {
	return fmt.Sprintf("env_%x", sha256.Sum256([]byte(name+"\x00"+endpoint+"\x00"+username)))
}
func Load(path string) Index {
	var i Index
	if data, e := os.ReadFile(path); e == nil {
		if bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")) {
			e = json.Unmarshal(data, &i)
		} else {
			e = yaml.Unmarshal(data, &i)
		}
		if e != nil {
			return Index{invalid: true}
		}
	} else if !os.IsNotExist(e) {
		return Index{invalid: true}
	}
	for name, p := range i.Profiles {
		if p.ID == "" {
			p.ID = LegacyID(name, p.URL, p.Username)
			i.Profiles[name] = p
		}
	}
	seen := map[string]bool{}
	for _, p := range i.Profiles {
		if p.ID == "" || p.NameSince < 0 || seen[p.ID] {
			return Index{invalid: true}
		}
		seen[p.ID] = true
	}
	for _, b := range i.History {
		if b.Name == "" || b.ID == "" || b.From < 0 || (b.Until != 0 && b.Until <= b.From) {
			return Index{invalid: true}
		}
		if p, ok := i.Profiles[b.Name]; ok && p.ID != b.ID && (b.Until == 0 || b.Until > p.NameSince) {
			return Index{invalid: true}
		}
	}
	return i
}
func (i Index) RecordID(name, id string, at time.Time) string {
	if id != "" {
		return id
	}
	ms := at.UnixMilli()
	if p, ok := i.Profiles[name]; ok && (p.NameSince == 0 || ms >= p.NameSince) {
		return p.ID
	}
	for _, b := range i.History {
		if b.Name == name && (b.From == 0 || ms >= b.From) && (b.Until == 0 || ms < b.Until) {
			return b.ID
		}
	}
	return ""
}
func (i Index) WriterID(name string, at time.Time) string {
	if id := i.RecordID(name, "", at); id != "" {
		return id
	}
	// An in-flight writer may still carry the old name after the rename.
	for n := len(i.History) - 1; n >= 0; n-- {
		if b := i.History[n]; b.Name == name {
			return b.ID
		}
	}
	return ""
}
func (i Index) Display(name, id string, at time.Time) (string, string) {
	id = i.RecordID(name, id, at)
	for current, p := range i.Profiles {
		if id != "" && p.ID == id {
			return current, id
		}
	}
	return name, id
}
func (i Index) Match(selected, name, id string, at time.Time) bool {
	if i.invalid {
		return false
	}
	if selected == "" {
		return true
	}
	if p, ok := i.Profiles[selected]; ok {
		return i.RecordID(name, id, at) == p.ID
	}
	return name == selected
}

// SQL returns an ownership predicate for indexed original names and stable JSON IDs.
// Times use millisecond precision, matching persisted task/audit indexes.
func (i Index) SQL(selected, profileColumn, timeColumn string) (string, []any) {
	if i.invalid {
		return "0=1", nil
	}
	if selected == "" {
		return "1=1", nil
	}
	p, ok := i.Profiles[selected]
	if !ok {
		return profileColumn + "=?", []any{selected}
	}
	names := []Binding{{Name: selected, ID: p.ID, From: p.NameSince}}
	for _, b := range i.History {
		if b.ID == p.ID {
			names = append(names, b)
		}
	}
	var parts []string
	args := []any{p.ID}
	for _, b := range names {
		part := profileColumn + "=?"
		args = append(args, b.Name)
		if b.From != 0 {
			part += " AND " + timeColumn + ">=?"
			args = append(args, b.From)
		}
		if b.Until != 0 {
			part += " AND " + timeColumn + "<?"
			args = append(args, b.Until)
		}
		parts = append(parts, "("+part+")")
	}
	return "(json_extract(record,'$.profileId')=? OR (coalesce(json_extract(record,'$.profileId'),'')='' AND (" + strings.Join(parts, " OR ") + ")))", args
}

func NewID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return "env_" + hex.EncodeToString(b[:]), nil
}
