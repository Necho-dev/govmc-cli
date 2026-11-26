package journal

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConsoleOutputIsReadableAndSkipsSuccessfulPolling(t *testing.T) {
	var buffer bytes.Buffer
	writer := ConsoleOutput(&buffer)
	event := Event{Time: time.Date(2026, 11, 25, 14, 0, 0, 123000000, time.UTC), Level: "INFO", Event: "request.completed", Operation: "GET host", Profile: "lab"}
	raw, _ := json.Marshal(event)
	if n, err := writer.Write(raw); err != nil || n != len(raw) || buffer.Len() != 0 {
		t.Fatal(n, err, buffer.String())
	}
	event.Level = "ERROR"
	event.Code = "timeout"
	event.DurationMS = 3000
	raw, _ = json.Marshal(event)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[ERROR]", "[ACCESS]", event.Time.Local().Format("2006-01-02 15:04:05.000"), "GET host", "profile=lab", "timeout", "duration=3000ms"} {
		if !strings.Contains(buffer.String(), want) {
			t.Fatal(buffer.String())
		}
	}
	if strings.Contains(buffer.String(), "\"event\"") {
		t.Fatal("JSON printed to console")
	}
	buffer.Reset()
	event.Level = "INFO"
	event.Event = "operation.completed"
	event.Operation = "vm.power"
	event.Outcome = "success"
	raw, _ = json.Marshal(event)
	_, _ = writer.Write(raw)
	if !strings.Contains(buffer.String(), "vm.power") || !strings.Contains(buffer.String(), "success") {
		t.Fatal(buffer.String())
	}
}

func TestConsoleColorIsPresentationOnly(t *testing.T) {
	var colored, plain bytes.Buffer
	event := Event{Time: time.Now(), Level: "WARNING", Event: "console.closed", Profile: "lab", Resource: "18", Outcome: "failed", Code: "cancelled"}
	raw, _ := json.Marshal(event)
	original := string(raw)
	_, err := (consoleOutput{output: &colored, color: true}).Write(raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ConsoleOutput(&plain).Write(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(colored.String(), "\x1b[") || strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("terminal color handling")
	}
	for _, value := range []string{"[WARN]", "[CONSOLE]", "resource=18", "result=failed", "code=cancelled"} {
		if !strings.Contains(plain.String(), value) {
			t.Fatal(plain.String())
		}
	}
	if string(raw) != original {
		t.Fatal("raw event was changed")
	}
}

func TestConsoleMirrorPreservesDurableEvent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	store, err := Open(path, "serve")
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	store.SetMirror(ConsoleOutput(&output))
	event := Event{ID: "original-id", Time: time.Date(2026, 11, 25, 14, 0, 0, 123456789, time.UTC), Source: "serve", Level: "INFO", Event: "operation", Operation: "POST vms.console", Resource: "18", Profile: "lab", Outcome: "success", RequestID: "original-request-id", Actor: "anonymous", DurationMS: 123}
	store.Log(event)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	found := false
	err = filepath.WalkDir(filepath.Join(filepath.Dir(path), "logs"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			var recorded Event
			if err := json.Unmarshal(line, &recorded); err != nil {
				return err
			}
			if recorded.ID != event.ID {
				continue
			}
			found = true
			if !recorded.Time.Equal(event.Time) || recorded.RequestID != event.RequestID || recorded.Actor != event.Actor || recorded.Resource != event.Resource || recorded.DurationMS != event.DurationMS {
				t.Fatalf("durable event changed: %+v", recorded)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("original structured event was not stored")
	}
	if !strings.Contains(output.String(), "[ACCESS]") || strings.Contains(output.String(), event.RequestID) {
		t.Fatal("console did not use compact presentation", output.String())
	}
}
