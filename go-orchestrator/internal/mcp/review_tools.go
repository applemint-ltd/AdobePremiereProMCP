package mcp

import (
	"context"
	"fmt"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

// registerReviewTools exposes the remote review loop: preview export,
// ffmpeg contact sheet over the exported file, and the Slack upload that
// gets both back into the user's thread.
func registerReviewTools(s *server.MCPServer, orch Orchestrator, logger *zap.Logger) {
	s.AddTool(
		gomcp.NewTool("premiere_warm_encoder",
			gomcp.WithDescription("Bring Adobe Media Encoder up and confirm it can accept render jobs. Call this ONCE early in a session (before the user is waiting on a preview or export) — a cold AME can take 1-2 minutes to launch, and this pays that cost up front so the first real export/preview/frame-capture is fast. Returns quickly if AME is already warm."),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			res, err := orch.WarmEncoder(ctx)
			if err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			return toolResultJSON(res)
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_export_preview",
			gomcp.WithDescription("Export the active sequence as a low-bitrate MP4 into the project's Previews folder and wait (bounded) for the file to finish rendering. Status \"completed\" means the file exists with a stable size; \"queued_not_confirmed\" means the render may still be running in Adobe Media Encoder — never assume it finished. Pair with premiere_post_file_to_slack so a remote user can watch the cut."),
			gomcp.WithString("output_name", gomcp.Description("File name for the preview (default: preview_<timestamp>.mp4)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			res, err := orch.ExportPreview(ctx, argString(req, "output_name", ""))
			if err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			return toolResultJSON(res)
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_generate_contact_sheet",
			gomcp.WithDescription("Render a grid of evenly spaced frames from a video FILE (typically the preview from premiere_export_preview) into one PNG, via ffmpeg on the media engine. This is the working storyboard-style overview on Premiere 2026 — the old sequence-based frame export used an API Adobe removed. Pair with premiere_post_file_to_slack."),
			gomcp.WithString("video_path", gomcp.Required(), gomcp.Description("Absolute path to a video file on disk (e.g. the preview export)")),
			gomcp.WithNumber("columns", gomcp.Description("Grid columns (default 4)")),
			gomcp.WithNumber("rows", gomcp.Description("Grid rows (default 3)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			res, err := orch.GenerateReviewContactSheet(ctx,
				argString(req, "video_path", ""),
				argInt(req, "columns", 4),
				argInt(req, "rows", 3))
			if err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}
			return toolResultJSON(res)
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_post_file_to_slack",
			gomcp.WithDescription("Upload a file produced by this pipeline (preview export, contact sheet, frame capture, final export) into a Slack channel/thread so the remote user can see it. Use the channel and thread_ts from the [Slack context] line in the conversation. Only files under the project folder or the temp dir can be sent. Requires the bot's files:write scope."),
			gomcp.WithString("file_path", gomcp.Required(), gomcp.Description("Absolute path of the file to upload")),
			gomcp.WithString("channel_id", gomcp.Required(), gomcp.Description("Slack channel ID (from [Slack context])")),
			gomcp.WithString("thread_ts", gomcp.Description("Thread timestamp to post into (from [Slack context]); omit for a top-level post")),
			gomcp.WithString("title", gomcp.Description("Display title for the file")),
			gomcp.WithString("comment", gomcp.Description("Short message to post with the file")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			res, err := orch.PostFileToSlack(ctx,
				argString(req, "file_path", ""),
				argString(req, "channel_id", ""),
				argString(req, "thread_ts", ""),
				argString(req, "title", ""),
				argString(req, "comment", ""))
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("%v", err)), nil
			}
			return toolResultJSON(res)
		},
	)

	logger.Debug("review tools registered")
}
