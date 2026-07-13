package mcp

import (
	"context"
	"encoding/json"
	"time"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/audit"
)

const (
	auditArgsCap    = 2048
	auditSummaryCap = 500
)

// Snapshotter captures the active sequence's timeline as JSON so a mutating
// tool call can be diffed afterwards. Implemented by the snapshot store; nil
// disables pre-call snapshots.
type Snapshotter interface {
	// SnapshotBefore persists a pre-call snapshot for the given correlation
	// ID and returns a reference (file path) recorded in the audit line.
	SnapshotBefore(ctx context.Context, cid string) (string, error)
}

// newAuditMiddleware wraps every tool handler: it generates the correlation
// ID, threads a Span through the context (the gRPC layer appends per-hop
// timings to it), optionally takes a pre-mutation snapshot, and persists one
// audit record per call. Audit failures never fail the tool call.
func newAuditMiddleware(aud *audit.Auditor, snap Snapshotter, logger *zap.Logger) server.ToolHandlerMiddleware {
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			tool := req.Params.Name
			span := audit.NewSpan(tool)
			ctx = audit.WithSpan(ctx, span)

			argsJSON := ""
			if args := req.GetArguments(); len(args) > 0 {
				if b, err := json.Marshal(args); err == nil {
					argsJSON = audit.Truncate(string(b), auditArgsCap)
				}
			}

			class := classifyTool(tool)

			var snapshotRef, snapshotErr string
			if class.Snapshot && snap != nil {
				snapCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				ref, err := snap.SnapshotBefore(snapCtx, span.CID)
				cancel()
				if err != nil {
					snapshotErr = err.Error()
				} else {
					snapshotRef = ref
				}
			}

			start := time.Now()
			result, err := next(ctx, req)
			durationMs := time.Since(start).Milliseconds()

			status := "ok"
			errMsg := ""
			summary := ""
			switch {
			case err != nil:
				status = "error"
				errMsg = audit.Truncate(err.Error(), auditSummaryCap)
			case result != nil && result.IsError:
				status = "error"
				errMsg = audit.Truncate(firstText(result), auditSummaryCap)
			default:
				summary = audit.Truncate(firstText(result), auditSummaryCap)
			}

			rec := audit.Record{
				CID:            span.CID,
				Tool:           tool,
				Args:           argsJSON,
				Status:         status,
				Error:          errMsg,
				DurationMs:     durationMs,
				ESCalls:        span.ESCalls(),
				Mutating:       class.Mutating,
				SnapshotBefore: snapshotRef,
				SnapshotError:  snapshotErr,
				ResultSummary:  summary,
			}
			if recErr := aud.Record(rec); recErr != nil {
				logger.Warn("audit record failed", zap.String("tool", tool), zap.Error(recErr))
			}

			logger.Info("tool call",
				zap.String("cid", span.CID),
				zap.String("tool", tool),
				zap.String("status", status),
				zap.Int64("duration_ms", durationMs),
			)

			return result, err
		}
	}
}

// firstText extracts the first text content block from a tool result.
func firstText(result *gomcp.CallToolResult) string {
	if result == nil {
		return ""
	}
	for _, c := range result.Content {
		if tc, ok := c.(gomcp.TextContent); ok {
			return tc.Text
		}
		if tc, ok := c.(*gomcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}
