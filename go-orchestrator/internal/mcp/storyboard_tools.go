package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

	s.AddTool(
		gomcp.NewTool("premiere_storyboard_expand_variants",
			gomcp.WithDescription("Expand ONE base storyboard into N A/B variants without touching the timeline: each variant applies field overrides (e.g. a different hook line), gets a deterministic sequence name stamped ({experiment_id}-{variant_id}), and must pass validation. The one-variable gate diffs each variant against the base and rejects arms that change more than one field — the discipline that keeps experiment results attributable. Assemble each returned storyboard with premiere_assemble_storyboard."),
			gomcp.WithString("storyboard_json", gomcp.Description("The BASE storyboard/v1 JSON document")),
			gomcp.WithString("csv_path", gomcp.Description("Path to a shot-list CSV as the base (alternative to storyboard_json)")),
			gomcp.WithString("experiment_id", gomcp.Required(), gomcp.Description("Experiment identifier, e.g. EXP014 — becomes part of every sequence name")),
			gomcp.WithString("variants_json", gomcp.Required(), gomcp.Description(`JSON array of variants: [{"variant_id":"hookA","label":"question hook","overrides":{"scenes[0].shots[0].text[0].content":"Still doing this manually?"}}]. Override paths address fields as key.key[N].key; null deletes a field; array indexes must already exist.`)),
			gomcp.WithString("naming_template", gomcp.Description("Sequence-name template; placeholders {experiment_id}, {variant_id}, {label}. Default \"{experiment_id}-{variant_id}\"")),
			gomcp.WithBoolean("enforce_single_variable", gomcp.Description("Require each variant to differ from base in exactly one field path (default true)")),
			gomcp.WithArray("allowed_extra_paths", gomcp.Description("Field paths exempt from the single-variable gate (sequence.name is always exempt)"), gomcp.WithStringItems()),
			gomcp.WithBoolean("check_collisions", gomcp.Description("Check rendered sequence names against the open project's sequences (default true; skipped with a warning when no project is reachable)")),
		),
		func(ctx context.Context, req gomcp.CallToolRequest) (*gomcp.CallToolResult, error) {
			base, errResult := storyboardFromArgs(req)
			if errResult != nil {
				return errResult, nil
			}
			var variants []storyboard.VariantSpec
			if err := json.Unmarshal([]byte(argString(req, "variants_json", "")), &variants); err != nil {
				return gomcp.NewToolResultError(fmt.Sprintf("variants_json is not a valid variant array: %v", err)), nil
			}
			extraPaths, _ := extractStringSlice(req, "allowed_extra_paths")

			expanded, err := storyboard.ExpandVariants(storyboard.ExpandVariantsInput{
				Base:                  base,
				ExperimentID:          argString(req, "experiment_id", ""),
				Variants:              variants,
				NamingTemplate:        argString(req, "naming_template", ""),
				EnforceSingleVariable: argBoolDefault(req, "enforce_single_variable", true),
				AllowedExtraPaths:     extraPaths,
			})
			if err != nil {
				return gomcp.NewToolResultError(err.Error()), nil
			}

			// Collision check against the open project is best-effort: the
			// expansion itself is a pure transform and must work offline.
			var warnings []string
			if argBoolDefault(req, "check_collisions", true) {
				if list, lerr := orch.GetSequenceList(ctx); lerr != nil {
					warnings = append(warnings, fmt.Sprintf("could not check sequence-name collisions (no project reachable?): %v", lerr))
				} else {
					existing := map[string]bool{}
					for _, entry := range list.Sequences {
						existing[entry.Name] = true
					}
					var collisions []string
					for _, v := range expanded {
						if existing[v.SequenceName] {
							collisions = append(collisions, v.SequenceName)
						}
					}
					if len(collisions) > 0 {
						return gomcp.NewToolResultError(fmt.Sprintf("sequence names already exist in the project: %s — delete those sequences, change the experiment_id, or rename", strings.Join(collisions, ", "))), nil
					}
				}
			}

			return toolResultJSON(map[string]any{
				"experiment_id": argString(req, "experiment_id", ""),
				"count":         len(expanded),
				"variants":      expanded,
				"warnings":      warnings,
			})
		},
	)

	logger.Debug("storyboard tools registered")
}

// argBoolDefault reads a boolean argument, defaulting when absent (argBool
// cannot express default-true).
func argBoolDefault(req gomcp.CallToolRequest, key string, def bool) bool {
	if v, ok := req.GetArguments()[key].(bool); ok {
		return v
	}
	return def
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
