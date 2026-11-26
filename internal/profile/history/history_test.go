package history

import (
	"database/sql"
	"encoding/json"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOwnershipSQLAndJSONLAgreeAcrossNameReuse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	config := []byte(`{"profiles":{"hongkong":{"id":"keep","nameSince":2000},"legacy":{"id":"new","nameSince":3000}},"profileHistory":[{"name":"legacy","id":"keep","until":3000}]}`)
	if e := os.WriteFile(path, config, 0600); e != nil {
		t.Fatal(e)
	}
	index := Load(path)
	if index.invalid {
		t.Fatal("JSON configuration rejected")
	}
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec("CREATE TABLE events(profile TEXT,time_ms INTEGER,record TEXT)"); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name, id, owner string
		at              int64
	}{{"legacy", "", "hongkong", 1000}, {"legacy", "", "hongkong", 2500}, {"legacy", "", "legacy", 4000}, {"legacy", "keep", "hongkong", 4000}, {"hongkong", "", "hongkong", 2200}}
	for _, c := range cases {
		raw, _ := json.Marshal(map[string]any{"profileId": c.id})
		if _, e = db.Exec("INSERT INTO events VALUES(?,?,?)", c.name, c.at, raw); e != nil {
			t.Fatal(e)
		}
		for _, selected := range []string{"hongkong", "legacy"} {
			if index.Match(selected, c.name, c.id, time.UnixMilli(c.at)) != (selected == c.owner) {
				t.Fatalf("incorrect ownership %+v for %s", c, selected)
			}
		}
	}
	for _, selected := range []string{"hongkong", "legacy"} {
		predicate, args := index.SQL(selected, "profile", "time_ms")
		var count int
		if e = db.QueryRow("SELECT count(*) FROM events WHERE "+predicate, args...).Scan(&count); e != nil {
			t.Fatal(e)
		}
		want := 4
		if selected == "legacy" {
			want = 1
		}
		if count != want {
			t.Fatalf("SQL %s count %d, want %d", selected, count, want)
		}
	}
}
func TestInvalidIdentityConfigurationFailsClosed(t *testing.T) {
	for _, data := range []string{"invalid: [", `profiles: {a: {id: duplicate}, b: {id: duplicate}}`, `profiles: {a: {id: current}}
profile_history: [{name: a, id: other}]`} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if e := os.WriteFile(path, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		index := Load(path)
		if index.Match("a", "a", "", time.Now()) {
			t.Fatal("invalid metadata allowed a match")
		}
		if p, _ := index.SQL("a", "profile", "time_ms"); p != "0=1" {
			t.Fatal("invalid metadata allowed SQL query")
		}
	}
}
