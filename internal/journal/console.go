package journal

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// ConsoleOutput formats a copy of event metadata for people. Durable JSONL
// retains the original event; normal read polling remains queryable there.
func ConsoleOutput(output io.Writer) io.Writer {
	color := false
	if file, ok := output.(interface{ Fd() uintptr }); ok {
		_, noColor := os.LookupEnv("NO_COLOR")
		color = term.IsTerminal(int(file.Fd())) && !noColor && os.Getenv("TERM") != "dumb"
	}
	return consoleOutput{output: output, color: color}
}

type consoleOutput struct {
	output io.Writer
	color  bool
}

func (w consoleOutput) paint(value, color string) string {
	if !w.color {
		return value
	}
	return "\x1b[" + color + "m" + value + "\x1b[0m"
}

func consoleCategory(event Event) string {
	// HTTP operation metadata has a verb prefix, including audited writes.
	if verb, _, ok := strings.Cut(event.Operation, " "); ok {
		switch verb {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
			return "ACCESS"
		}
	}
	if strings.HasPrefix(event.Event, "console.") {
		return "CONSOLE"
	}
	if strings.HasPrefix(event.Event, "server.") {
		return "SERVER"
	}
	if strings.HasPrefix(event.Event, "process.") {
		return "PROCESS"
	}
	if event.Source == "mcp" {
		return "MCP"
	}
	if event.Operation != "" {
		return "ACTION"
	}
	return "RUNTIME"
}

func (w consoleOutput) Write(raw []byte) (int, error) {
	var event Event
	if err := json.Unmarshal(raw, &event); err != nil {
		return 0, err
	}
	if event.Level == "INFO" && event.Event == "request.completed" && (strings.HasPrefix(event.Operation, "GET ") || strings.HasPrefix(event.Operation, "HEAD ")) {
		return len(raw), nil
	}
	action := event.Operation
	if action == "" {
		action = event.Event
	}
	level, color := event.Level, "36"
	switch level {
	case "WARNING":
		level, color = "WARN", "33"
	case "ERROR":
		color = "31"
	}
	parts := []string{
		w.paint(event.Time.In(time.Local).Format("2006-01-02 15:04:05.000"), "2"),
		w.paint(fmt.Sprintf("%-7s", "["+clean(level)+"]"), color),
		w.paint(fmt.Sprintf("%-9s", "["+consoleCategory(event)+"]"), "2"),
		clean(action),
	}
	if event.Profile != "" {
		parts = append(parts, w.paint("profile="+clean(event.Profile), "36"))
	}
	if event.Resource != "" {
		key := "resource"
		if event.Event == "server.listening" {
			key = "address"
		}
		parts = append(parts, key+"="+clean(event.Resource))
	}
	if event.Outcome != "" {
		color := "33"
		switch event.Outcome {
		case "success":
			color = "32"
		case "failed", "denied":
			color = "31"
		}
		parts = append(parts, w.paint("result="+clean(event.Outcome), color))
	}
	if event.Code != "" {
		parts = append(parts, w.paint("code="+clean(event.Code), "33"))
	}
	if event.DurationMS != 0 || event.Event == "request.completed" {
		parts = append(parts, w.paint(fmt.Sprintf("duration=%dms", event.DurationMS), "2"))
	}
	_, err := io.WriteString(w.output, strings.Join(parts, " ")+"\n")
	if err != nil {
		return 0, err
	}
	return len(raw), nil
}
