package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

// Export-watching tools: on Premiere 2026 exports queue in Adobe Media
// Encoder and the scripting API exposes no real queue state, so a file on
// disk that has stopped growing (and probes as readable) is the ONLY honest
// completion signal. These tools expose that pattern directly.
func registerExportWatchTools(s *server.MCPServer, orch Orchestrator, logger *zap.Logger) {
	s.AddTool(
		gomcp.NewTool("premiere_await_exports",
			gomcp.WithDescription("Block until every given output file exists, has stopped growing, and (for video files) probes as readable — the only honest export-completion signal on Premiere 2026. On timeout it returns the partial per-file state (exists/bytes/growing/stable), never an error and never a false success. Use after premiere_export / premiere_export_via_ame / batch exports."),
			gomcp.WithArray("paths", gomcp.Required(), gomcp.Description("Absolute output file paths to wait for"), gomcp.WithStringItems()),
			gomcp.WithNumber("stable_for_s", gomcp.Description("Seconds the file size must hold steady before it counts as stable (default 3)")),
			gomcp.WithNumber("timeout_s", gomcp.Description("Overall wait bound in seconds (default 600). Expiry returns partial state with status=timeout")),
			gomcp.WithNumber("poll_interval_s", gomcp.Description("Seconds between polls (default 2, minimum 0.2)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			paths, err := extractStringSlice(req, "paths")
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("paths: %v", err)), nil
			}
			res, err := orch.AwaitExports(ctx, paths,
				gomcp.ParseFloat64(req, "stable_for_s", 3),
				gomcp.ParseFloat64(req, "timeout_s", 600),
				gomcp.ParseFloat64(req, "poll_interval_s", 2))
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("%v", err)), nil
			}
			return toolResultJSON(res)
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_export_status",
			gomcp.WithDescription("Non-blocking (~1s) snapshot of export output files: bytes on disk plus growth rate — honest progress without a fake percentage. A snapshot never certifies completion; use premiere_await_exports for that."),
			gomcp.WithArray("paths", gomcp.Required(), gomcp.Description("Absolute output file paths to inspect"), gomcp.WithStringItems()),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			paths, err := extractStringSlice(req, "paths")
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("paths: %v", err)), nil
			}
			res, err := orch.ExportStatus(ctx, paths)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("%v", err)), nil
			}
			return toolResultJSON(res)
		},
	)
}
