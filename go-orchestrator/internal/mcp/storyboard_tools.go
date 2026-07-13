package mcp

import (
	"context"
	"fmt"
	"os"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/orchestrator"
	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/storyboard"
)

// registerStoryboardTools exposes the storyboard pipeline: schema for
// LLM-direct authoring, a dry-run validator, and the assembler.
func registerStoryboardTools(s *server.MCPServer, orch Orchestrator, logger *zap.Logger) {
	s.AddTool(
		gomcp.NewTool("premiere_storyboard_schema",
			gomcp.WithDescription("Return the JSON Schema for the canonical storyboard format (storyboard/v1). Emit a document matching this schema and pass it to premiere_assemble_storyboard. Clips are referenced by NAME or file path (never index); times are seconds; on-screen text renders as baked-PNG layers (the only text that works on Premiere 2026); per-shot captions become a native SRT caption track."),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			return gomcp.NewToolResultText(storyboard.SchemaJSON()), nil
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_storyboard_validate",
			gomcp.WithDescription("Dry-run a storyboard WITHOUT touching the timeline: checks the document and resolves every referenced clip against the open project (importing referenced files that exist on disk). Returns per-shot resolution — including candidate lists for ambiguous names — so problems can be shown to the user BEFORE assembling. Provide exactly one of storyboard_json or csv_path."),
			gomcp.WithString("storyboard_json", gomcp.Description("A storyboard/v1 JSON document (get the schema from premiere_storyboard_schema)")),
			gomcp.WithString("csv_path", gomcp.Description("Path to a shot-list CSV (columns: order, clip, duration, from, to, text, caption, transition, notes — only clip is required)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			sb, errResult := storyboardFromArgs(req)
			if errResult != nil {
				return errResult, nil
			}
			report, err := orch.ValidateStoryboard(ctx, sb)
			if err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("validation failed: %v", err)), nil
			}
			return toolResultJSON(report)
		},
	)

	s.AddTool(
		gomcp.NewTool("premiere_assemble_storyboard",
			gomcp.WithDescription("Assemble a storyboard into a sequence: places each shot in order (trims honored via subclips), applies transitions (applied-or-reported, never silent), renders on-screen text as baked-PNG layers, builds a native caption track from per-shot captions, and lays a music bed. Returns a per-shot AssemblyReport plus a plain-language summary. Run premiere_storyboard_validate first and show the user any unresolved clips. Provide exactly one of storyboard_json, csv_path, or script_text/script_path."),
			gomcp.WithString("storyboard_json", gomcp.Description("A storyboard/v1 JSON document")),
			gomcp.WithString("csv_path", gomcp.Description("Path to a shot-list CSV (columns: order, clip, duration, from, to, text, caption, transition, notes)")),
			gomcp.WithString("script_text", gomcp.Description("Free-form script/narration text — parsed by the intelligence service and matched against assets_directory")),
			gomcp.WithString("script_path", gomcp.Description("Path to a script file (alternative to script_text)")),
			gomcp.WithString("script_format", gomcp.Description("Script format hint: 'screenplay', 'youtube', 'podcast' (default: auto)")),
			gomcp.WithString("assets_directory", gomcp.Description("Directory of footage to match script segments against (script ingestion only)")),
			gomcp.WithString("sequence_name", gomcp.Description("Override the sequence name")),
			gomcp.WithBoolean("stop_on_error", gomcp.Description("Stop at the first failed shot instead of continuing (default false)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			var (
				sb       *storyboard.Storyboard
				warnings []string
			)
			scriptText := argString(req, "script_text", "")
			scriptPath := argString(req, "script_path", "")
			if scriptText != "" || scriptPath != "" {
				var err error
				sb, warnings, err = orch.StoryboardFromScript(ctx,
					scriptText, scriptPath,
					argString(req, "script_format", ""),
					argString(req, "assets_directory", ""))
				if err != nil {
					return gomcp.NewToolResultError(err.Error()), nil
				}
			} else {
				var errResult *gomcp.CallToolResult
				sb, errResult = storyboardFromArgs(req)
				if errResult != nil {
					return errResult, nil
				}
			}

			opts := orchestrator.AssembleStoryboardOptions{
				SequenceName: argString(req, "sequence_name", ""),
				StopOnError:  argBool(req, "stop_on_error"),
			}
			report, err := orch.AssembleStoryboard(ctx, sb, opts)
			if err != nil {
				if report != nil {
					// Partial progress + error: return both so nothing is hidden.
					partial, _ := toolResultJSON(map[string]any{"error": err.Error(), "partial_report": report})
					if partial != nil {
						partial.IsError = true
						return partial, nil
					}
				}
				return gomcp.NewToolResultError(err.Error()), nil
			}
			if len(warnings) > 0 {
				report.Warnings = append(warnings, report.Warnings...)
			}
			return toolResultJSON(report)
		},
	)

	logger.Debug("storyboard tools registered")
}

// storyboardFromArgs builds a storyboard from storyboard_json or csv_path.
func storyboardFromArgs(req gomcp.CallToolRequest) (*storyboard.Storyboard, *gomcp.CallToolResult) {
	doc := argString(req, "storyboard_json", "")
	csvPath := argString(req, "csv_path", "")
	switch {
	case doc != "" && csvPath != "":
		return nil, gomcp.NewToolResultError("provide either storyboard_json or csv_path, not both")
	case doc != "":
		sb, err := storyboard.Parse([]byte(doc))
		if err != nil {
			return nil, gomcp.NewToolResultError(err.Error())
		}
		return sb, nil
	case csvPath != "":
		f, err := os.Open(csvPath)
		if err != nil {
			return nil, gomcp.NewToolResultError(fmt.Sprintf("could not open the shot list: %v", err))
		}
		defer f.Close()
		sb, err := storyboard.FromShotListCSV(f)
		if err != nil {
			return nil, gomcp.NewToolResultError(err.Error())
		}
		return sb, nil
	default:
		return nil, gomcp.NewToolResultError("provide storyboard_json or csv_path (or script_text/script_path for assembly)")
	}
}

// argBool reads a boolean argument.
func argBool(req gomcp.CallToolRequest, key string) bool {
	v, _ := req.GetArguments()[key].(bool)
	return v
}
