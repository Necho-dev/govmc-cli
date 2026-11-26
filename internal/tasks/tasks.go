// Package tasks persists execution state; in-memory queues only schedule work.
package tasks

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Necho-dev/govmc-cli/internal/journal"
	"github.com/Necho-dev/govmc-cli/internal/profile/history"
	_ "modernc.org/sqlite"
)

type Step struct {
	Name      string     `json:"name"`
	Status    string     `json:"status"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}
type Task struct {
	RequestHash     string          `json:"requestHash,omitempty"`
	PID             int             `json:"pid"`
	AuditID         string          `json:"auditId,omitempty"`
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	Profile         string          `json:"profile"`
	ProfileID       string          `json:"profileId,omitempty"`
	OriginalProfile string          `json:"originalProfile,omitempty"`
	Endpoint        string          `json:"endpoint"`
	Source          string          `json:"source"`
	Resource        string          `json:"resource"`
	Status          string          `json:"status"`
	Owner           string          `json:"owner"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	StartedAt       *time.Time      `json:"startedAt,omitempty"`
	EndedAt         *time.Time      `json:"endedAt,omitempty"`
	Parameters      json.RawMessage `json:"parameters"`
	Result          any             `json:"result,omitempty"`
	ErrorMessage    string          `json:"errorMessage,omitempty"`
	Error           string          `json:"error,omitempty"`
	Steps           []Step          `json:"steps"`
	CurrentStep     string          `json:"currentStep,omitempty"`
	Percent         *int            `json:"percent,omitempty"`
	RemoteTasks     []string        `json:"remoteTasks,omitempty"`
	RemoteTask      string          `json:"remoteTask,omitempty"`
	CancelRequested bool            `json:"cancelRequested"`
}
type Query struct {
	Profile string
	Status  string
	Kind    string
	Source  string
	Search  string
	Since   time.Time
	Until   time.Time
	Limit   int
	Offset  int
}
type Page struct {
	Active  int    `json:"active"`
	Items   []Task `json:"items"`
	HasMore bool   `json:"hasMore"`
}
type Manager struct {
	configPath    string
	db            *sql.DB
	owner, source string
	ctx           context.Context
	cancel        context.CancelFunc
	queue         chan job
	wg            sync.WaitGroup
	mu            sync.Mutex
	closed        bool
}
type job struct {
	cleanup func()
	audit   journal.Event
	store   *journal.Store
	task    Task
	run     func(context.Context, *Reporter) (any, error)
}
type Reporter struct {
	stop    context.CancelFunc
	manager *Manager
	store   *journal.Store
	task    Task
	mu      sync.Mutex
	failure error
}
type key struct{}
type requestKey struct{}

func WithRequest(ctx context.Context, input any) context.Context {
	b, _ := json.Marshal(input)
	sum := sha256.Sum256(b)
	return context.WithValue(ctx, requestKey{}, hex.EncodeToString(sum[:]))
}
func requestHash(ctx context.Context, input any) string {
	value, _ := ctx.Value(requestKey{}).(string)
	if value != "" {
		return value
	}
	b, _ := json.Marshal(input)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func (m *Manager) Replay(ctx context.Context, profile, endpoint, kind, key string) (Task, bool, error) {
	if key == "" {
		return Task{}, false, nil
	}
	var record string
	e := m.db.QueryRow("SELECT record FROM tasks WHERE request_key=?", endpoint+"|"+kind+"|"+key).Scan(&record)
	if errors.Is(e, sql.ErrNoRows) {
		return Task{}, false, nil
	}
	if e != nil {
		return Task{}, false, e
	}
	var t Task
	if e = json.Unmarshal([]byte(record), &t); e != nil {
		return t, false, e
	}
	t = m.projectTask(history.Load(m.configPath), t)
	hash, _ := ctx.Value(requestKey{}).(string)
	if t.Profile != profile || hash != "" && t.RequestHash != hash {
		return Task{}, false, errors.New("idempotency key reused with different parameters")
	}
	return t, true, nil
}

func With(ctx context.Context, m *Manager) context.Context { return context.WithValue(ctx, key{}, m) }
func From(ctx context.Context) *Manager                    { m, _ := ctx.Value(key{}).(*Manager); return m }
func terminal(s string) bool {
	return s == "succeeded" || s == "failed" || s == "cancelled" || s == "interrupted"
}
func Open(path, source string) (*Manager, error) {
	configPath := path
	path = filepath.Join(filepath.Dir(path), "data.db")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := journal.PrepareDatabase(path); err != nil {
		return nil, err
	}
	u, err := journal.DatabaseURL(path)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Add("_pragma", "busy_timeout(5000)")
	// Reserve the writer before checking active resource locks. A deferred WAL
	// transaction cannot upgrade its read snapshot after another owner reports progress.
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS tasks(id TEXT PRIMARY KEY, profile TEXT NOT NULL, status TEXT NOT NULL, kind TEXT NOT NULL, source TEXT NOT NULL, resource TEXT NOT NULL, owner TEXT NOT NULL, updated INTEGER NOT NULL, created INTEGER NOT NULL, lock_key TEXT NOT NULL, request_key TEXT UNIQUE, record TEXT NOT NULL); CREATE INDEX IF NOT EXISTS tasks_listing ON tasks(created DESC,id); CREATE UNIQUE INDEX IF NOT EXISTS tasks_active_destination ON tasks(lock_key) WHERE status IN ('queued','running','cancelling'); CREATE TABLE IF NOT EXISTS task_owners(id TEXT PRIMARY KEY, heartbeat INTEGER NOT NULL);`)
	if err != nil {
		db.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{configPath: configPath, db: db, source: source, owner: journal.ID(), ctx: ctx, cancel: cancel, queue: make(chan job, 32)}
	if _, err = db.Exec("INSERT INTO task_owners VALUES (?,?)", m.owner, time.Now().UnixMilli()); err != nil {
		db.Close()
		cancel()
		return nil, err
	}
	m.wg.Add(3)
	go m.worker()
	go m.worker()
	go m.heartbeat()
	return m, nil
}
func (m *Manager) persist(t Task) error {
	if t.ProfileID == "" {
		t.ProfileID = history.Load(m.configPath).RecordID(t.Profile, "", t.CreatedAt)
	}
	b, e := json.Marshal(t)
	if e != nil {
		return e
	}
	res, e := m.db.Exec("UPDATE tasks SET status=?,updated=?,record=json_set(?,'$.cancelRequested',json(CASE WHEN json_extract(record,'$.cancelRequested') THEN 'true' ELSE 'false' END)) WHERE id=? AND owner=?", t.Status, t.UpdatedAt.UnixMilli(), b, t.ID, m.owner)
	if e == nil {
		n, _ := res.RowsAffected()
		if n != 1 {
			return errors.New("task ownership lost")
		}
	}
	return e
}
func (m *Manager) Submit(ctx context.Context, profile, endpoint, kind, resource string, params any, lockKey, requestKey string, run func(context.Context, *Reporter) (any, error), cleanups ...func()) (Task, error) {
	cleanup := func() {}
	if len(cleanups) > 0 && cleanups[0] != nil {
		cleanup = cleanups[0]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Task{}, errors.New("task runner closed")
	}
	if len(m.queue) == cap(m.queue) {
		return Task{}, errors.New("task queue full")
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return Task{}, err
	}
	hash := requestHash(ctx, params)
	t := Task{PID: os.Getpid(), RequestHash: hash, ID: "task_" + journal.ID(), Profile: profile, Endpoint: endpoint, Kind: kind, Source: m.source, Resource: resource, Status: "queued", Owner: m.owner, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(), Parameters: raw, Steps: []Step{}}
	t.ProfileID = history.Load(m.configPath).WriterID(profile, t.CreatedAt)
	var request any
	if requestKey != "" {
		request = endpoint + "|" + kind + "|" + requestKey
		var record string
		e := m.db.QueryRow("SELECT record FROM tasks WHERE request_key=?", request).Scan(&record)
		if e == nil {
			var old Task
			json.Unmarshal([]byte(record), &old)
			old = m.projectTask(history.Load(m.configPath), old)
			if old.Profile != profile || old.RequestHash != hash {
				return Task{}, errors.New("idempotency key reused with different parameters")
			}
			cleanup()
			return old, nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return Task{}, e
		}
	}
	store := journal.From(ctx)
	var audit journal.Event
	if store != nil {
		audit, err = store.Begin(ctx, journal.Event{Source: m.source, Profile: profile, Operation: kind, Resource: t.ID})
		if err != nil {
			return Task{}, err
		}
	}
	t.AuditID = audit.ID
	keys := strings.Split(lockKey, "\x00")
	for i := range keys {
		keys[i] = endpoint + "|" + keys[i]
	}
	reserved := strings.Join(keys, "\x00")
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT lock_key FROM tasks WHERE status IN ('queued','running','cancelling')")
	if err != nil {
		return Task{}, err
	}
	conflict := false
	for rows.Next() {
		var active string
		if e := rows.Scan(&active); e != nil {
			rows.Close()
			return Task{}, e
		}
		for _, old := range strings.Split(active, "\x00") {
			for _, key := range keys {
				if old == key || strings.HasPrefix(old, key+"/") || strings.HasPrefix(key, old+"/") {
					conflict = true
				}
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return Task{}, err
	}
	if conflict {
		_ = tx.Rollback()
		if store != nil {
			_ = store.Finish(ctx, audit, errors.New("resource busy"))
		}
		return Task{}, errors.New("source or destination is busy with another task")
	}
	b, _ := json.Marshal(t)
	_, err = tx.Exec("INSERT INTO tasks VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", t.ID, profile, t.Status, kind, m.source, resource, m.owner, t.UpdatedAt.UnixMilli(), t.CreatedAt.UnixMilli(), reserved, request, b)
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		if store != nil {
			_ = store.Finish(ctx, audit, err)
		}
		return Task{}, fmt.Errorf("cannot queue task (destination may already be busy): %w", err)
	}
	m.queue <- job{task: t, run: run, audit: audit, store: store, cleanup: cleanup}
	return t, nil
}
func (m *Manager) Get(id string) (Task, error) {
	var b string
	err := m.db.QueryRow("SELECT record FROM tasks WHERE id=?", id).Scan(&b)
	if err != nil {
		return Task{}, err
	}
	var t Task
	err = json.Unmarshal([]byte(b), &t)
	return m.projectTask(history.Load(m.configPath), t), err
}
func (m *Manager) List(q Query) (Page, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if q.Offset < 0 {
		return Page{}, errors.New("invalid offset")
	}
	index := history.Load(m.configPath)
	predicate, args := index.SQL(q.Profile, "profile", "created")
	sqlq := "SELECT record FROM tasks WHERE " + predicate
	for _, f := range [][2]string{{"status", q.Status}, {"kind", q.Kind}, {"source", q.Source}} {
		if f[0] == "status" && f[1] == "active" {
			sqlq += " AND status IN ('queued','running','cancelling')"
			continue
		}
		if f[1] != "" {
			sqlq += " AND " + f[0] + "=?"
			args = append(args, f[1])
		}
	}
	if q.Search != "" {
		sqlq += " AND (resource LIKE ? OR id LIKE ?)"
		args = append(args, "%"+q.Search+"%", "%"+q.Search+"%")
	}
	if !q.Since.IsZero() {
		sqlq += " AND created>=?"
		args = append(args, q.Since.UnixMilli())
	}
	if !q.Until.IsZero() {
		sqlq += " AND created<=?"
		args = append(args, q.Until.UnixMilli())
	}
	sqlq += " ORDER BY created DESC,id DESC LIMIT ? OFFSET ?"
	args = append(args, limit+1, q.Offset)
	rows, err := m.db.Query(sqlq, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	p := Page{Items: []Task{}}
	for rows.Next() {
		var b string
		if err = rows.Scan(&b); err != nil {
			return p, err
		}
		var t Task
		if err = json.Unmarshal([]byte(b), &t); err != nil {
			return p, err
		}
		p.Items = append(p.Items, m.projectTask(index, t))
	}
	if len(p.Items) > limit {
		p.HasMore = true
		p.Items = p.Items[:limit]
	}
	e := rows.Err()
	rows.Close()
	countQuery := "SELECT count(*) FROM tasks WHERE status IN ('queued','running','cancelling')"
	countPredicate, countArgs := index.SQL(q.Profile, "profile", "created")
	countQuery += " AND " + countPredicate
	if e == nil {
		e = m.db.QueryRow(countQuery, countArgs...).Scan(&p.Active)
	}
	return p, e
}
func (m *Manager) Cancel(id string) error {
	res, e := m.db.Exec("UPDATE tasks SET record=json_set(record,'$.cancelRequested',json('true')) WHERE id=? AND status IN ('queued','running','cancelling')", id)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("task not found or already finished")
	}
	return nil
}
func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case j := <-m.queue:
			m.execute(j)
		}
	}
}
func (m *Manager) execute(j job) {
	defer j.cleanup()
	t := j.task
	current, err := m.Get(t.ID)
	if err != nil {
		return
	}
	if current.CancelRequested {
		t.Status = "cancelled"
		now := time.Now().UTC()
		t.EndedAt = &now
		t.UpdatedAt = now
		_ = m.persist(t)
		if j.store != nil {
			_ = j.store.Finish(context.Background(), j.audit, context.Canceled)
		}
		return
	}
	now := time.Now().UTC()
	t.StartedAt = &now
	t.UpdatedAt = now
	t.Status = "running"
	if m.persist(t) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(With(context.WithValue(m.ctx, activeTaskKey{}, t.ID), m), 45*time.Minute)
	r := &Reporter{manager: m, task: t, stop: cancel, store: j.store}
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(300 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				latest, e := m.Get(t.ID)
				if e == nil && latest.CancelRequested {
					r.mu.Lock()
					r.task.CancelRequested = true
					r.task.Status = "cancelling"
					r.save()
					r.mu.Unlock()
					cancel()
					return
				}
			}
		}
	}()
	result, err := j.run(ctx, r)
	r.mu.Lock()
	if err == nil && r.failure != nil {
		err = r.failure
	}
	r.mu.Unlock()
	if j.store != nil {
		finish, stop := context.WithTimeout(context.Background(), 3*time.Second)
		_ = j.store.Finish(finish, j.audit, err)
		stop()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t = r.task
	t.Result = result
	t.Percent = nil
	ended := time.Now().UTC()
	t.EndedAt = &ended
	t.UpdatedAt = ended
	t.Status = "succeeded"
	if err == nil && r.failure != nil {
		err = r.failure
	}
	if err != nil {
		t.Status = "failed"
		t.Error = journal.ErrorCode(err)
		var public interface{ TaskFailure() (string, string) }
		if errors.As(err, &public) {
			t.Error, t.ErrorMessage = public.TaskFailure()
		}
		if err.Error() == "remote_task_outcome_unknown" {
			t.Error = "remote_task_outcome_unknown"
			t.Status = "interrupted"
		}
		if errors.Is(err, context.Canceled) {
			t.Status = "cancelled"
		}
		if m.ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || t.RemoteTask != "" && ctx.Err() != nil {
			t.Status = "interrupted"
		}
	}
	_ = m.persist(t)
}
func (r *Reporter) save() {
	r.task.UpdatedAt = time.Now().UTC()
	if err := r.manager.persist(r.task); err != nil {
		r.failure = err
		if r.stop != nil {
			r.stop()
		}
	}
}
func (r *Reporter) Step(name, status string) {
	if r.store != nil {
		r.store.Log(journal.Event{Source: r.manager.source, Profile: r.task.Profile, Resource: r.task.ID, Event: "task.step", Operation: name, Outcome: status})
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.task.CurrentStep = name
	r.task.Percent = nil
	if status == "running" {
		r.task.Steps = append(r.task.Steps, Step{Name: name, Status: status, StartedAt: now})
	} else if n := len(r.task.Steps); n > 0 {
		r.task.Steps[n-1].Status = status
		r.task.Steps[n-1].EndedAt = &now
	}
	r.save()
}
func (r *Reporter) Remote(ref string, percent *int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.task.RemoteTask = ref
	if ref != "" {
		found := false
		for _, value := range r.task.RemoteTasks {
			if value == ref {
				found = true
			}
		}
		if !found {
			r.task.RemoteTasks = append(r.task.RemoteTasks, ref)
		}
	}
	r.task.Percent = percent
	r.save()
}
func (m *Manager) heartbeat() {
	defer m.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.db.Exec("UPDATE task_owners SET heartbeat=? WHERE id=?", time.Now().UnixMilli(), m.owner)
			// Only mark expired foreign owners; do not interrupt other live CLI/MCP/serve processes.
			now := time.Now().UTC()
			_, _ = m.db.Exec(`UPDATE tasks SET status='interrupted',updated=?,record=json_set(record,'$.status','interrupted','$.error','owner_unavailable','$.updatedAt',?,'$.endedAt',?,'$.percent',NULL) WHERE status IN ('queued','running','cancelling') AND owner IN (SELECT id FROM task_owners WHERE heartbeat<?)`, now.UnixMilli(), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Add(-30*time.Second).UnixMilli())
		}
	}
}
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	for len(m.queue) > 0 {
		j := <-m.queue
		j.cleanup()
		if j.store != nil {
			_ = j.store.Finish(context.Background(), j.audit, context.Canceled)
		}
	}
	now := time.Now().UTC()
	_, _ = m.db.Exec(`UPDATE tasks SET status='interrupted',updated=?,record=json_set(record,'$.status','interrupted','$.error','runner_stopped','$.updatedAt',?,'$.endedAt',?,'$.percent',NULL) WHERE owner=? AND status IN ('queued','running','cancelling')`, now.UnixMilli(), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), m.owner)
	_, _ = m.db.Exec("DELETE FROM task_owners WHERE id=?", m.owner)
	m.db.Close()
}
func (m *Manager) Wait(ctx context.Context, id string) (Task, error) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		t, e := m.Get(id)
		if e != nil {
			return t, e
		}
		if terminal(t.Status) {
			if t.Status != "succeeded" {
				return t, fmt.Errorf("task %s: %s (%s)", id, t.Status, t.Error)
			}
			return t, nil
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-ticker.C:
		}
	}
}

type activeTaskKey struct{}

// EnsureIdle is shared by all device mutations; a running job may access its own reservations.
func (m *Manager) EnsureIdle(ctx context.Context, endpoint string, resources ...string) error {
	own, _ := ctx.Value(activeTaskKey{}).(string)
	rows, e := m.db.QueryContext(ctx, "SELECT lock_key FROM tasks WHERE status IN ('queued','running','cancelling') AND id!=?", own)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var record string
		if e = rows.Scan(&record); e != nil {
			return e
		}
		for _, reserved := range strings.Split(record, "\x00") {
			for _, resource := range resources {
				key := endpoint + "|" + resource
				if reserved == key || strings.HasPrefix(reserved, key+"/") || strings.HasPrefix(key, reserved+"/") {
					return errors.New("resource busy with an active task")
				}
			}
		}
	}
	return rows.Err()
}

func (m *Manager) projectTask(index history.Index, t Task) Task {
	name, id := index.Display(t.Profile, t.ProfileID, t.CreatedAt)
	if name != t.Profile {
		if t.OriginalProfile == "" {
			t.OriginalProfile = t.Profile
		}
		t.Profile = name
	}
	t.ProfileID = id
	return t
}
