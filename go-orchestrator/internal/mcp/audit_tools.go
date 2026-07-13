package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/audit"
)

// registerAuditTools exposes the audit trail as MCP tools: the raw log, a
// "what changed" timeline diff against stored pre-mutation snapshots, and a
// plain-language session digest for non-editors. Called after registerTools
// so it can also re-point premiere_get_event_history (whose ExtendScript
// backing never recorded MCP edits) at the audit log, which does.
func registerAuditTools(s *server.MCPServer, aud *audit.Auditor, snapshots *audit.SnapshotStore, logger *zap.Logger) {
	if aud == nil {
		return
	}

	s.AddTool(
		gomcp.NewTool("premiere_get_audit_log",
			gomcp.WithDescription("Return the persisted audit records of MCP tool calls (timestamp, correlation ID, tool, args, status, duration). This is the ground truth of everything the AI did to Premiere. Filter by session_tag, tool name, or correlation_id."),
			gomcp.WithNumber("limit", gomcp.Description("Maximum records to return, most recent kept (default 50)")),
			gomcp.WithString("session_tag", gomcp.Description("Only records for this session/thread tag (default: current session)")),
			gomcp.WithString("tool", gomcp.Description("Only records for this exact tool name")),
			gomcp.WithString("correlation_id", gomcp.Description("Only the record with this correlation ID")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			f := audit.Filter{
				Session: argString(req, "session_tag", aud.Session()),
				Tool:    argString(req, "tool", ""),
				CID:     argString(req, "correlation_id", ""),
				Limit:   argInt(req, "limit", 50),
			}
			if f.CID != "" {
				f.Session = "" // a cid is globally unique; don't over-filter
			}
			recs, err := aud.Query(f)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("failed to read audit log: %v", err)), nil
			}
			return toolResultJSON(map[string]any{"count": len(recs), "records": recs})
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_what_changed",
			gomcp.WithDescription("Describe, in plain language, how the timeline changed since a pre-edit snapshot: clips added, removed, moved, trimmed, enabled/disabled. With no arguments it compares the most recent pre-mutation snapshot of this session against the live timeline. Use after an edit to verify or explain what happened."),
			gomcp.WithString("correlation_id", gomcp.Description("Compare against the snapshot taken before this specific tool call (from the audit log). Default: latest snapshot in this session.")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			if snapshots == nil {
				return gomcp.NewToolResultError("snapshot store is disabled (PREMIERE_AUDIT_DIR unset)"), nil
			}
			var stored *audit.StoredSnapshot
			var err error
			if cid := argString(req, "correlation_id", ""); cid != "" {
				stored, err = snapshots.Load(cid)
			} else {
				stored, err = snapshots.LatestBefore(aud.Session())
			}
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("no baseline snapshot: %v", err)), nil
			}
			live, err := snapshots.Capture(ctx)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("failed to capture live timeline: %v", err)), nil
			}
			diff, err := audit.Diff(string(stored.Timeline), live)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("failed to diff timelines: %v", err)), nil
			}
			return toolResultJSON(map[string]any{
				"summary":     strings.Join(diff.HumanLines(), " "),
				"human_lines": diff.HumanLines(),
				"since":       stored.TS,
				"baseline_correlation_id": stored.CID,
				"details":     diff,
			})
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_diff_timeline",
			gomcp.WithDescription("Diff two stored timeline snapshots (by their correlation IDs from the audit log), or a stored snapshot against the live timeline. Returns added/removed/moved/trimmed clips plus plain-language lines."),
			gomcp.WithString("from_correlation_id", gomcp.Description("Correlation ID whose pre-call snapshot is the 'before' side"), gomcp.Required()),
			gomcp.WithString("to_correlation_id", gomcp.Description("Correlation ID for the 'after' side, or \"live\" (default) for the current timeline")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			if snapshots == nil {
				return gomcp.NewToolResultError("snapshot store is disabled (PREMIERE_AUDIT_DIR unset)"), nil
			}
			fromCID := argString(req, "from_correlation_id", "")
			if fromCID == "" {
				return gomcp.NewToolResultError("from_correlation_id is required"), nil
			}
			from, err := snapshots.Load(fromCID)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("load 'from' snapshot: %v", err)), nil
			}

			toCID := argString(req, "to_correlation_id", "live")
			var afterJSON string
			if toCID == "live" || toCID == "" {
				afterJSON, err = snapshots.Capture(ctx)
				if err != nil {
					return gomcp.NewToolResultError(fmt.Sprintf("capture live timeline: %v", err)), nil
				}
			} else {
				to, err := snapshots.Load(toCID)
				if err != nil {
					return gomcp.NewToolResultError(fmt.Sprintf("load 'to' snapshot: %v", err)), nil
				}
				afterJSON = string(to.Timeline)
			}

			diff, err := audit.Diff(string(from.Timeline), afterJSON)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("diff timelines: %v", err)), nil
			}
			return toolResultJSON(map[string]any{
				"human_lines": diff.HumanLines(),
				"details":     diff,
			})
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_get_session_digest",
			gomcp.WithDescription("Plain-language digest of what the AI did in this session: actions in order (collapsed when repeated), failures listed separately with causes, export paths included. Use this to answer 'what did you do?' for a non-technical user."),
			gomcp.WithString("session_tag", gomcp.Description("Session/thread tag to digest (default: current session)")),
			gomcp.WithString("since", gomcp.Description("Only actions at/after this RFC3339 time or \"today\"")),
			gomcp.WithNumber("limit", gomcp.Description("Maximum audit records to consider (default 200)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			f := audit.Filter{
				Session: argString(req, "session_tag", aud.Session()),
				Limit:   argInt(req, "limit", 200),
			}
			if since := argString(req, "since", ""); since != "" {
				if since == "today" {
					now := time.Now()
					f.Since = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
				} else if t, err := time.Parse(time.RFC3339, since); err == nil {
					f.Since = t
				}
			}
			recs, err := aud.Query(f)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("failed to read audit log: %v", err)), nil
			}
			actions, failures := digestRecords(recs)
			return toolResultJSON(map[string]any{
				"session":  f.Session,
				"actions":  actions,
				"failures": failures,
				"records":  len(recs),
			})
		},
	)

	// The ExtendScript _eventHistory never records MCP edits (only manually
	// registered panel listeners), so re-point the history tools at the audit
	// log — the record that actually captures what the AI did.
	overrideEventHistory := func(name, desc string) {
		s.AddTool(
			gomcp.NewTool(name,
				gomcp.WithDescription(desc),
				gomcp.WithNumber("count", gomcp.Description("Maximum entries to return (default 50)")),
			),
			func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
				recs, err := aud.Recent(argInt(req, "count", 50), aud.Session())
				if err != nil {
					return gomcp.NewToolResultError(fmt.Sprintf("failed to read audit log: %v", err)), nil
				}
				actions, failures := digestRecords(recs)
				return toolResultJSON(map[string]any{
					"source":   "audit_log",
					"actions":  actions,
					"failures": failures,
					"records":  recs,
				})
			},
		)
	}
	overrideEventHistory("premiere_get_event_history",
		"Recent tool-call history from the persistent audit log (what the AI actually did), most recent last. Backed by the audit trail, not Premiere's event listeners.")
	overrideEventHistory("premiere_get_recent_actions",
		"Recent actions performed via MCP tools, from the persistent audit log, rendered as plain-language lines plus raw records.")

	logger.Info("audit tools registered")
}

// digestRecords turns audit records into ordered plain-language action lines
// (consecutive repeats collapsed) and a separate failure list.
func digestRecords(recs []audit.Record) (actions []string, failures []string) {
	var lastLine string
	var repeat int
	flush := func() {
		if lastLine == "" {
			return
		}
		if repeat > 1 {
			actions = append(actions, fmt.Sprintf("%s (x%d)", lastLine, repeat))
		} else {
			actions = append(actions, lastLine)
		}
		lastLine, repeat = "", 0
	}

	for _, r := range recs {
		if r.Status == "error" {
			flush()
			failures = append(failures, fmt.Sprintf("%s — %s failed: %s", shortTS(r.TS), humanizeAction(r), firstSentence(r.Error)))
			continue
		}
		if !r.Mutating {
			continue // reads don't belong in a "what did you do" digest
		}
		line := humanizeAction(r)
		if line == lastLine {
			repeat++
			continue
		}
		flush()
		lastLine, repeat = line, 1
	}
	flush()
	return actions, failures
}

// humanizeAction maps a tool call to a short plain-language phrase, pulling
// obvious details (file names, text content, output paths) from the args.
func humanizeAction(r audit.Record) string {
	name := strings.TrimPrefix(r.Tool, "premiere_")
	args := map[string]any{}
	_ = json.Unmarshal([]byte(r.Args), &args) // truncated args just yield fewer details

	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	base := func(p string) string {
		p = strings.ReplaceAll(p, "\\", "/")
		parts := strings.Split(p, "/")
		return parts[len(parts)-1]
	}

	switch {
	case strings.HasPrefix(name, "import_") || name == "fetch_slack_attachment":
		if f := str("file_name", "file_path", "path", "folder_path"); f != "" {
			return fmt.Sprintf("Imported %q", base(f))
		}
		return "Imported media"
	case strings.Contains(name, "insert_clip") || strings.Contains(name, "overwrite_clip") || strings.Contains(name, "place_clip"):
		return "Added a clip to the timeline"
	case strings.Contains(name, "remove_clip") || strings.Contains(name, "delete_all_clips"):
		return "Removed a clip from the timeline"
	case strings.HasPrefix(name, "razor"):
		return "Cut a clip"
	case strings.HasPrefix(name, "trim_") || strings.HasPrefix(name, "ripple_trim") || strings.HasPrefix(name, "roll_trim") || strings.HasPrefix(name, "slip_") || strings.HasPrefix(name, "slide_"):
		return "Trimmed a clip"
	case strings.Contains(name, "transition") || strings.Contains(name, "crossfade"):
		return "Added a transition"
	case strings.HasPrefix(name, "export") || strings.HasPrefix(name, "convert_to_"):
		if p := str("output_path", "outputPath"); p != "" {
			return fmt.Sprintf("Exported %q", base(p))
		}
		return "Exported video"
	case strings.Contains(name, "text") || strings.Contains(name, "title") || strings.Contains(name, "lower_third") || strings.Contains(name, "credits"):
		if t := str("text", "content", "title"); t != "" {
			return fmt.Sprintf("Added on-screen text %q", audit.Truncate(t, 60))
		}
		return "Added on-screen text"
	case strings.Contains(name, "caption") || strings.Contains(name, "subtitle"):
		return "Worked on captions/subtitles"
	case strings.HasPrefix(name, "lumetri") || strings.Contains(name, "color"):
		return "Adjusted color"
	case strings.Contains(name, "audio") || strings.Contains(name, "volume") || strings.Contains(name, "gain") || strings.HasPrefix(name, "fade_") || strings.Contains(name, "normalize"):
		return "Adjusted audio"
	case strings.HasPrefix(name, "create_sequence") || name == "create_sequence_from_clips":
		if n := str("name", "sequence_name", "sequenceName"); n != "" {
			return fmt.Sprintf("Created sequence %q", n)
		}
		return "Created a sequence"
	case strings.HasPrefix(name, "assemble") || name == "auto_edit":
		return "Assembled clips into the timeline"
	case strings.HasPrefix(name, "apply_"):
		return "Applied an effect (" + strings.ReplaceAll(strings.TrimPrefix(name, "apply_"), "_", " ") + ")"
	default:
		return strings.ToUpper(name[:1]) + strings.ReplaceAll(name[1:], "_", " ")
	}
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n"); i > 0 {
		s = s[:i]
	}
	return audit.Truncate(s, 200)
}

func shortTS(ts string) string {
	if t, err := time.Parse("2006-01-02T15:04:05.000Z07:00", ts); err == nil {
		return t.Local().Format("15:04")
	}
	if len(ts) >= 16 {
		return ts[11:16]
	}
	return ts
}

// argString reads a string argument with a default.
func argString(req gomcp.CallToolRequest, key, def string) string {
	if v, ok := req.GetArguments()[key].(string); ok && v != "" {
		return v
	}
	return def
}

// argInt reads a numeric argument with a default.
func argInt(req gomcp.CallToolRequest, key string, def int) int {
	if v, ok := req.GetArguments()[key].(float64); ok {
		return int(v)
	}
	return def
}
