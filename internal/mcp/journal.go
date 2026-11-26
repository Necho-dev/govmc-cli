package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/Necho-dev/govmc-cli/internal/journal"
	"time"
)

func journalTools() []map[string]any {
	var out []map[string]any
	for _, name := range []string{"esxi_logs_list", "esxi_audits_list"} {
		props := map[string]any{}
		for _, field := range []string{"profile", "source", "level", "outcome", "search", "since", "until"} {
			props[field] = map[string]string{"type": "string"}
		}
		for _, field := range []string{"limit", "offset"} {
			props[field] = map[string]string{"type": "integer"}
		}
		out = append(out, map[string]any{"name": name, "description": "Query local runtime logs or operation audits with profile ownership preserved across renames. No ESXi connection. Original profile name is retained in originalProfile.", "inputSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": props}})
	}
	return out
}
func journalTool(ctx context.Context, name string, args map[string]any, profile string) (any, error) {
	s := journal.From(ctx)
	if s == nil {
		return nil, fmt.Errorf("journal service unavailable")
	}
	var r struct {
		Profile, Source, Level, Outcome, Search, Since, Until string
		Limit, Offset                                         int
	}
	b, _ := json.Marshal(args)
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(&r); e != nil {
		return nil, e
	}
	if profile == "" {
		profile, _ = ctx.Value(profileKey{}).(string)
	}
	q := journal.Query{Profile: profile, Source: r.Source, Level: r.Level, Outcome: r.Outcome, Search: r.Search, Limit: r.Limit, Offset: r.Offset}
	var e error
	if r.Since != "" {
		q.Since, e = time.Parse(time.RFC3339, r.Since)
		if e != nil {
			return nil, e
		}
	}
	if r.Until != "" {
		q.Until, e = time.Parse(time.RFC3339, r.Until)
		if e != nil {
			return nil, e
		}
	}
	if name == "esxi_logs_list" {
		return s.Logs(ctx, q)
	}
	return s.Audits(ctx, q)
}
