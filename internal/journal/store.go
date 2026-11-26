// Package journal records allowlisted process events and durable operation metadata.
package journal

import (
	"bufio"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/Necho-dev/govmc-cli/internal/profile/history"
	"github.com/gofrs/flock"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

type Event struct {
	Session         string    `json:"session,omitempty"`
	PID             int       `json:"pid,omitempty"`
	Count           uint64    `json:"count,omitempty"`
	ID              string    `json:"id"`
	Time            time.Time `json:"time"`
	Source          string    `json:"source"`
	Level           string    `json:"level"`
	Event           string    `json:"event"`
	Profile         string    `json:"profile,omitempty"`
	ProfileID       string    `json:"profileId,omitempty"`
	OriginalProfile string    `json:"originalProfile,omitempty"`
	Actor           string    `json:"actor,omitempty"`
	Operation       string    `json:"operation,omitempty"`
	Resource        string    `json:"resource,omitempty"`
	Outcome         string    `json:"outcome,omitempty"`
	Code            string    `json:"code,omitempty"`
	RequestID       string    `json:"requestId,omitempty"`
	Mode            string    `json:"mode,omitempty"`
	DurationMS      int64     `json:"durationMs,omitempty"`
}

type Query struct {
	Profile, Source, Level, Outcome, Search string
	Since, Until                            time.Time
	Limit, Offset                           int
}
type Result struct {
	Items   []Event `json:"items"`
	HasMore bool    `json:"hasMore"`
	Offset  int     `json:"offset"`
	Limit   int     `json:"limit"`
}
type Policy struct {
	Days   int `yaml:"days" json:"days"`
	MaxMiB int `yaml:"max_mib" json:"maxMiB"`
}
type Status struct {
	Root      string `json:"root"`
	Runtime   Policy `json:"runtime"`
	AuditDays int    `json:"auditDays"`
	Dropped   uint64 `json:"dropped"`
	Problem   string `json:"problem,omitempty"`
}
type queued struct {
	event   *Event
	barrier chan struct{}
}
type Store struct {
	configPath            string
	mirror                io.Writer
	root, source, session string
	db                    *sql.DB
	policy                Policy
	auditDays             int
	queue                 chan queued
	done                  chan struct{}
	mu                    sync.Mutex
	closed                bool
	problem               string
	dropped               atomic.Uint64
}
type contextKey struct{}

func With(ctx context.Context, s *Store) context.Context {
	return context.WithValue(ctx, contextKey{}, s)
}
func From(ctx context.Context) *Store { s, _ := ctx.Value(contextKey{}).(*Store); return s }
func ID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("random ID unavailable")
	}
	return hex.EncodeToString(b[:])
}
func clean(v string) string {
	r := []rune(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, v))
	return string(r[:min(len(r), 160)])
}
func prepare(e Event, source string) Event {
	if e.ID == "" {
		e.ID = ID()
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if e.Source == "" {
		e.Source = source
	}
	if e.Level == "" {
		e.Level = "INFO"
	}
	values := []*string{&e.Source, &e.Level, &e.Event, &e.Profile, &e.Actor, &e.Operation, &e.Resource, &e.Outcome, &e.Code, &e.RequestID, &e.Mode}
	for _, v := range values {
		*v = clean(*v)
	}
	return e
}
func Open(configPath, source string) (*Store, error) {
	if source == "" {
		source = "cli"
	}
	switch source {
	case "cli", "tui", "serve", "mcp":
	default:
		return nil, errors.New("invalid journal source")
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	configPath = absolute
	root := filepath.Dir(configPath)
	if info, err := os.Lstat(filepath.Join(root, "logs")); err == nil && !info.IsDir() {
		return nil, errors.New("logs must be a regular directory")
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	var cfg struct {
		Runtime Policy `yaml:"runtime_logs"`
		Audit   struct {
			Days int `yaml:"days"`
		} `yaml:"audit"`
	}
	if raw, err := os.ReadFile(configPath); err == nil {
		if err = yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, errors.New("cannot read journal policy")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if cfg.Runtime.Days == 0 {
		cfg.Runtime.Days = 7
	}
	if cfg.Runtime.MaxMiB == 0 {
		cfg.Runtime.MaxMiB = 50
	}
	if cfg.Audit.Days == 0 {
		cfg.Audit.Days = 90
	}
	if cfg.Runtime.Days < 1 || cfg.Runtime.Days > 3650 || cfg.Runtime.MaxMiB < 1 || cfg.Runtime.MaxMiB > 10240 || cfg.Audit.Days < 1 || cfg.Audit.Days > 3650 {
		return nil, errors.New("invalid log or audit retention policy")
	}
	dbPath := filepath.Join(root, "data.db")
	if err := PrepareDatabase(dbPath); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: dbPath}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(3000)")
	q.Add("_pragma", "foreign_keys(1)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = db.ExecContext(ctx, `PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY, time_ms INTEGER NOT NULL, profile TEXT NOT NULL, source TEXT NOT NULL, outcome TEXT NOT NULL, record TEXT NOT NULL);
 CREATE INDEX IF NOT EXISTS audit_events_time ON audit_events(time_ms DESC,id);
 CREATE INDEX IF NOT EXISTS audit_events_profile ON audit_events(profile,time_ms DESC);
 CREATE TABLE IF NOT EXISTS journal_schema (version INTEGER PRIMARY KEY);
 INSERT OR IGNORE INTO journal_schema VALUES (1);`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize local journal: %w", err)
	}
	s := &Store{configPath: configPath, root: root, source: clean(source), session: clean(source) + "-" + ID(), db: db, policy: cfg.Runtime, auditDays: cfg.Audit.Days, queue: make(chan queued, 1024), done: make(chan struct{})}
	if s.source == "" {
		s.source = "cli"
	}
	go s.writer()
	s.Log(Event{Event: "process.started"})
	if _, err = db.ExecContext(ctx, "DELETE FROM audit_events WHERE time_ms < ? AND outcome != 'pending'", time.Now().Add(-time.Duration(s.auditDays)*24*time.Hour).UnixMilli()); err != nil {
		s.fail("audit_retention_failed")
	}
	return s, nil
}
func (s *Store) SetMirror(w io.Writer) { s.mu.Lock(); s.mirror = w; s.mu.Unlock() }
func (s *Store) fail(code string)      { s.mu.Lock(); s.problem = code; s.mu.Unlock() }
func (s *Store) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Status{s.root, s.policy, s.auditDays, s.dropped.Load(), s.problem}
}
func (s *Store) Log(e Event) {
	if s == nil {
		return
	}
	e = prepare(e, s.source)
	if e.ProfileID == "" {
		e.ProfileID = history.Load(s.configPath).WriterID(e.Profile, e.Time)
	}
	e.Session = s.session
	e.PID = os.Getpid()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.queue <- queued{event: &e}:
	default:
		s.dropped.Add(1)
	}
}
func (s *Store) Flush(ctx context.Context) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("journal closed")
	}
	b := make(chan struct{})
	select {
	case s.queue <- queued{barrier: b}:
		s.mu.Unlock()
	case <-ctx.Done():
		s.mu.Unlock()
		return ctx.Err()
	}
	select {
	case <-b:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	s.Log(Event{Event: "process.stopped"})
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.queue)
	s.mu.Unlock()
	<-s.done
	return s.db.Close()
}
func (s *Store) writer() {
	defer close(s.done)
	dir := filepath.Join(s.root, "logs", s.session)
	if err := os.MkdirAll(dir, 0700); err != nil {
		s.fail("runtime_directory_failed")
		for item := range s.queue {
			if item.barrier != nil {
				close(item.barrier)
			}
		}
		return
	}
	lock := flock.New(filepath.Join(dir, "writer.lock"))
	if err := lock.Lock(); err != nil {
		s.fail("runtime_lock_failed")
		for item := range s.queue {
			if item.barrier != nil {
				close(item.barrier)
			}
		}
		return
	}
	defer lock.Close()
	var f *os.File
	var size int64
	index := 0
	var path string
	reported := uint64(0)
	archive := func() {
		if f != nil {
			f.Close()
			_ = os.Rename(path, strings.TrimSuffix(path, ".active")+".jsonl")
			f = nil
		}
	}
	defer archive()
	lastPrune := time.Time{}
	for item := range s.queue {
		if item.barrier != nil {
			close(item.barrier)
			continue
		}
		events := []Event{*item.event}
		if n := s.dropped.Load(); n > reported {
			events = append([]Event{prepare(Event{Event: "runtime.queue_dropped", Level: "WARNING", Code: "queue_full", Count: n - reported}, s.source)}, events...)
			reported = n
		}
		for _, e := range events {
			raw, err := json.Marshal(e)
			if err != nil {
				continue
			}
			raw = append(raw, '\n')
			s.mu.Lock()
			mirror := s.mirror
			s.mu.Unlock()
			if mirror != nil {
				if _, err := mirror.Write(raw); err != nil {
					s.fail("runtime_mirror_failed")
				}
			}
			if f == nil || size+int64(len(raw)) > 5*1024*1024 {
				archive()
				index++
				path = filepath.Join(dir, fmt.Sprintf("%06d.active", index))
				f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				size = 0
				if err != nil {
					s.fail("runtime_open_failed")
					continue
				}
				if index > 4 {
					_ = os.Remove(filepath.Join(dir, fmt.Sprintf("%06d.jsonl", index-4)))
				}
			}
			if f != nil {
				n, err := f.Write(raw)
				size += int64(n)
				if err != nil {
					s.fail("runtime_write_failed")
				}
			}
		}
		if time.Since(lastPrune) > time.Minute {
			s.prune()
			lastPrune = time.Now()
		}
	}
}
func (s *Store) prune() {
	s.recoverSegments()
	paths, _ := filepath.Glob(filepath.Join(s.root, "logs", "*", "*.jsonl"))
	type entry struct {
		path string
		at   time.Time
		size int64
	}
	var files []entry
	var size int64
	for _, p := range paths {
		if st, err := os.Lstat(p); err == nil && validSegment(p) && st.Mode().IsRegular() {
			files = append(files, entry{p, st.ModTime(), st.Size()})
			size += st.Size()
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	for _, f := range files {
		if time.Since(f.at) > time.Duration(s.policy.Days)*24*time.Hour || size > int64(s.policy.MaxMiB)*1024*1024 {
			if os.Remove(f.path) == nil {
				size -= f.size
			}
		}
	}
	s.removeEmptySessions()
}
func bounds(q Query) Query {
	if q.Limit <= 0 {
		q.Limit = 100
	}
	q.Limit = min(q.Limit, 500)
	q.Offset = max(0, min(q.Offset, 100000))
	q.Search = clean(q.Search)
	return q
}
func matches(e Event, q Query) bool {
	if q.Profile != "" && e.Profile != q.Profile || q.Source != "" && e.Source != q.Source || q.Level != "" && e.Level != q.Level || q.Outcome != "" && e.Outcome != q.Outcome || !q.Since.IsZero() && e.Time.Before(q.Since) || !q.Until.IsZero() && e.Time.After(q.Until) {
		return false
	}
	if q.Search != "" {
		raw, _ := json.Marshal(e)
		return strings.Contains(strings.ToLower(string(raw)), strings.ToLower(q.Search))
	}
	return true
}
func (s *Store) Logs(ctx context.Context, q Query) (Result, error) {
	q = bounds(q)
	result := Result{Items: []Event{}, Offset: q.Offset, Limit: q.Limit}
	if err := s.Flush(ctx); err != nil {
		return result, err
	}
	index := history.Load(s.configPath)
	paths, _ := filepath.Glob(filepath.Join(s.root, "logs", "*", "*"))
	var rows []Event
	for _, p := range paths {
		if filepath.Ext(p) != ".jsonl" && filepath.Ext(p) != ".active" {
			continue
		}
		st, err := os.Lstat(p)
		if err != nil || !validSegment(p) || !st.Mode().IsRegular() || st.Size() > 6*1024*1024 {
			continue
		}
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 4096), 8192)
		for scan.Scan() {
			if ctx.Err() != nil {
				f.Close()
				return result, ctx.Err()
			}
			var e Event
			if json.Unmarshal(scan.Bytes(), &e) == nil && index.Match(q.Profile, e.Profile, e.ProfileID, e.Time) {
				e = s.projectEvent(index, e)
				if matches(e, q) {
					rows = append(rows, e)
				}
			}
		}
		f.Close()
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Time.Equal(rows[j].Time) {
			return rows[i].ID > rows[j].ID
		}
		return rows[i].Time.After(rows[j].Time)
	})
	if q.Offset < len(rows) {
		end := min(len(rows), q.Offset+q.Limit)
		result.Items = rows[q.Offset:end]
		result.HasMore = end < len(rows)
	}
	return result, nil
}
func (s *Store) Begin(ctx context.Context, e Event) (Event, error) {
	if s == nil {
		return e, nil
	}
	e = prepare(e, s.source)
	if e.ProfileID == "" {
		e.ProfileID = history.Load(s.configPath).WriterID(e.Profile, e.Time)
	}
	e.Session = s.session
	e.PID = os.Getpid()
	e.Event = "operation"
	e.Outcome = "pending"
	if e.Actor == "" {
		e.Actor = fmt.Sprintf("uid:%d", os.Getuid())
		if s.source == "serve" {
			e.Actor = "anonymous"
		}
	}
	raw, _ := json.Marshal(e)
	_, err := s.db.ExecContext(ctx, "INSERT INTO audit_events(id,time_ms,profile,source,outcome,record) VALUES(?,?,?,?,?,?)", e.ID, e.Time.UnixMilli(), e.Profile, e.Source, e.Outcome, string(raw))
	if err != nil {
		s.fail("audit_write_failed")
	}
	return e, err
}
func (s *Store) Finish(ctx context.Context, e Event, err error) error {
	if s == nil {
		return nil
	}
	e.DurationMS = time.Since(e.Time).Milliseconds()
	e.Code = ErrorCode(err)
	e.Outcome = "success"
	if err != nil {
		e.Outcome = "failed"
		e.Level = "ERROR"
		if e.Code == "permission_denied" {
			e.Outcome = "denied"
			e.Level = "WARNING"
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			e.Outcome = "unknown"
		}
	}
	raw, _ := json.Marshal(e)
	_, writeErr := s.db.ExecContext(ctx, "UPDATE audit_events SET outcome=?,record=? WHERE id=?", e.Outcome, string(raw), e.ID)
	s.Log(e)
	if writeErr != nil {
		s.fail("audit_write_failed")
	}
	return writeErr
}
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	v := strings.ToLower(err.Error())
	switch {
	case strings.Contains(v, "decrypt") || strings.Contains(v, "credential key"):
		return "credential_unavailable"
	case strings.Contains(v, "incorrect user") || strings.Contains(v, "authenticate esxi"):
		return "authentication_failed"
	case strings.Contains(v, "readonly") || strings.Contains(v, "permission"):
		return "permission_denied"
	}
	return "operation_failed"
}
func Run(ctx context.Context, e Event, fn func() error) error {
	s := From(ctx)
	if s == nil {
		return fn()
	}
	start, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	row, err := s.Begin(start, e)
	cancel()
	if err != nil {
		return fmt.Errorf("record audit before operation: %w", err)
	}
	err = fn()
	finish, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	return errors.Join(err, s.Finish(finish, row, err))
}
func (s *Store) Audits(ctx context.Context, q Query) (Result, error) {
	q = bounds(q)
	result := Result{Items: []Event{}, Offset: q.Offset, Limit: q.Limit}
	index := history.Load(s.configPath)
	predicate, args := index.SQL(q.Profile, "profile", "time_ms")
	where := []string{predicate}
	for _, v := range [][2]string{{"source", q.Source}, {"outcome", q.Outcome}} {
		if v[1] != "" {
			where = append(where, v[0]+"=?")
			args = append(args, v[1])
		}
	}
	if !q.Since.IsZero() {
		where = append(where, "time_ms>=?")
		args = append(args, q.Since.UnixMilli())
	}
	if !q.Until.IsZero() {
		where = append(where, "time_ms<=?")
		args = append(args, q.Until.UnixMilli())
	}
	if q.Search != "" {
		where = append(where, "instr(lower(record),lower(?))>0")
		args = append(args, q.Search)
	}
	if q.Level != "" {
		where = append(where, "json_extract(record,'$.level')=?")
		args = append(args, q.Level)
	}
	args = append(args, q.Limit+1, q.Offset)
	rows, err := s.db.QueryContext(ctx, "SELECT record FROM audit_events WHERE "+strings.Join(where, " AND ")+" ORDER BY time_ms DESC,id DESC LIMIT ? OFFSET ?", args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return result, err
		}
		var e Event
		if json.Unmarshal([]byte(raw), &e) == nil {
			result.Items = append(result.Items, s.projectEvent(index, e))
		}
	}
	if len(result.Items) > q.Limit {
		result.HasMore = true
		result.Items = result.Items[:q.Limit]
	}
	return result, rows.Err()
}

// projectEvent only changes the query view; original JSONL and audit records remain immutable.
func (s *Store) projectEvent(index history.Index, e Event) Event {
	name, id := index.Display(e.Profile, e.ProfileID, e.Time)
	if name != e.Profile {
		if e.OriginalProfile == "" {
			e.OriginalProfile = e.Profile
		}
		e.Profile = name
	}
	e.ProfileID = id
	return e
}
