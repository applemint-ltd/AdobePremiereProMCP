package mcp

// Drift guard: the tool pipeline spans three hand-synced name spaces — MCP
// tool names (Go), ExtendScript function-name string literals in the
// orchestrator's EvalCommand calls, and actual function definitions in the
// CEP host (premiere.jsx/core.jsx) — plus panel.js's typed ACTION_MAP.
// Nothing at runtime validates they match, which is how tools like
// premiere_generate_rough_cut shipped calling host functions that do not
// exist. These tests make that class of bug a build failure.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// knownMissingHostFns are EvalCommand targets that are KNOWN to be absent
// from the CEP host today (their tools are denied in brokenTools). Each
// entry must stay missing: if someone implements one, the test fails so the
// entry is removed and the tool can be re-evaluated for the core set. The
// list must only ever shrink.
var knownMissingHostFns = map[string]string{
	"generateRoughCut":          "premiere_generate_rough_cut (brokenTools)",
	"createSocialCuts":          "premiere_create_social_cuts (brokenTools)",
	"refineEdit":                "premiere_refine_edit (brokenTools)",
	"smartCut":                  "premiere_smart_cut (brokenTools)",
	"smartTrim":                 "premiere_smart_trim (brokenTools)",
	"autoColorMatch":            "premiere_auto_color_match (brokenTools)",
	"autoAudioLevels":           "premiere_auto_audio_levels (brokenTools)",
	"suggestTransitions":        "premiere_suggest_transitions (brokenTools)",
	"generateTrailer":           "premiere_generate_trailer (brokenTools)",
	"estimateRenderTime":        "premiere_estimate_render_time (brokenTools)",
	"analyzeClip":               "premiere_analyze_clip (brokenTools)",
	"analyzeSequence":           "premiere_analyze_sequence (brokenTools)",
	"generateEditSummary":       "premiere_generate_edit_summary (brokenTools)",
	"suggestMusic":              "premiere_suggest_music (brokenTools)",
	"suggestReplacements":       "premiere_suggest_replacements (brokenTools)",
	"detectAudioIssues":         "premiere_detect_audio_issues (brokenTools)",
	"findSimilarClips":          "premiere_find_similar_clips (brokenTools)",
	"autoOrganizeProject":       "premiere_auto_organize_project (brokenTools)",
	"createReviewMarkers":       "premiere_create_review_markers (brokenTools)",
	"checkDeliverySpecs":        "premiere_check_delivery_specs (brokenTools)",
	"createProjectReport":       "premiere_create_project_report (brokenTools)",
	"addBRollSuggestions":       "premiere_add_broll_suggestions (brokenTools)",
	"aITagClips":                "premiere_tag_clips (brokenTools)",
	"exportEDLFile":             "premiere_export_edl_file (brokenTools)",
	"importOMFFile":             "premiere_import_omf (brokenTools)",
	"listExportPresetsFromDisk": "premiere_list_export_presets_disk (brokenTools)",
	"selectAll":                 "premiere_select_all (brokenTools)",
	"executeMenuItemByID":       "premiere_execute_menu_item (escape hatch; ES fn missing)",
	"applyVignetteEffect":       "effect_chain_ops variant; premiere_apply_vignette uses applyVignette",
	"getSequenceStatistics":     "premiere_get_sequence_statistics (brokenTools)",
	"getExportHistory2":         "duplicate registration variant in delivery_ops",
	"getEstimatedRenderTime2":   "duplicate registration variant",
	"getInstalledEffects2":      "duplicate registration variant",
	"getInstalledPlugins2":      "duplicate registration variant",
	"getInstalledTransitions2":  "duplicate registration variant",
	"getPerformanceReport2":     "duplicate registration variant",
}

const hostDir = "../../../cep-panel/src/host"

// hostFunctionNames extracts every top-level `function name(` definition
// from the CEP host scripts.
func hostFunctionNames(t *testing.T) map[string]bool {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*function\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)
	fns := map[string]bool{}
	for _, f := range []string{"premiere.jsx", "core.jsx"} {
		data, err := os.ReadFile(filepath.Join(hostDir, f))
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range re.FindAllStringSubmatch(string(data), -1) {
			fns[m[1]] = true
		}
	}
	if len(fns) < 500 {
		t.Fatalf("suspiciously few host functions found (%d) — extraction broken?", len(fns))
	}
	return fns
}

// evalCommandTargets AST-parses the orchestrator package and returns every
// string literal passed as the function-name argument of an EvalCommand
// call. Fails the test if any EvalCommand name argument is NOT a literal —
// dynamic names would silently escape this guard.
func evalCommandTargets(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, "../orchestrator", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatalf("parse orchestrator package: %v", err)
	}

	// Functions allowed to forward a dynamic name (the generic dispatcher
	// behind the execute_script escape hatches). Everything else must pass a
	// string literal so the guard stays sound.
	dynamicForwarders := map[string]bool{"EvalCommand": true}

	targets := map[string][]string{} // fn name -> file positions
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "EvalCommand" || len(call.Args) < 2 {
						return true
					}
					pos := fset.Position(call.Pos()).String()
					lit, ok := call.Args[1].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						if !dynamicForwarders[fn.Name.Name] {
							t.Errorf("EvalCommand at %s (in %s): function-name argument is not a string literal; the drift guard cannot verify it", pos, fn.Name.Name)
						}
						return true
					}
					name, err := strconv.Unquote(lit.Value)
					if err != nil {
						t.Errorf("EvalCommand at %s: unquote %s: %v", pos, lit.Value, err)
						return true
					}
					targets[name] = append(targets[name], pos)
					return true
				})
			}
		}
	}
	if len(targets) < 500 {
		t.Fatalf("suspiciously few EvalCommand targets found (%d) — extraction broken?", len(targets))
	}
	return targets
}

// TestEvalCommandTargetsExist is the generateRoughCut-class guard: every
// ExtendScript function the orchestrator invokes must exist in the host.
func TestEvalCommandTargetsExist(t *testing.T) {
	host := hostFunctionNames(t)
	targets := evalCommandTargets(t)

	var missing []string
	for name, positions := range targets {
		if host[name] {
			continue
		}
		if _, known := knownMissingHostFns[name]; known {
			continue
		}
		missing = append(missing, name+" (called at "+positions[0]+")")
	}
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("EvalCommand target has no host function: %s", m)
	}

	// Stale exceptions: a known-missing function that now exists means it
	// was implemented — remove the exception (and revisit brokenTools).
	for name := range knownMissingHostFns {
		if host[name] {
			t.Errorf("knownMissingHostFns entry %q is now defined in the host — remove the exception and re-evaluate its tool", name)
		}
		if _, called := targets[name]; !called {
			t.Errorf("knownMissingHostFns entry %q is no longer called by any EvalCommand — remove the stale exception", name)
		}
	}
}

// TestTypedActionMapTargetsExist verifies the host functions panel.js's
// typed ACTION_MAP builders invoke are actually defined.
func TestTypedActionMapTargetsExist(t *testing.T) {
	data, err := os.ReadFile("../../../cep-panel/src/panel.js")
	if err != nil {
		t.Fatalf("read panel.js: %v", err)
	}
	src := string(data)

	start := strings.Index(src, "var ACTION_MAP")
	if start < 0 {
		t.Fatalf("ACTION_MAP not found in panel.js")
	}
	end := strings.Index(src[start:], "};")
	if end < 0 {
		t.Fatalf("ACTION_MAP block end not found")
	}
	block := src[start : start+end]

	re := regexp.MustCompile(`return\s+"([A-Za-z_$][A-Za-z0-9_$]*)\(`)
	host := hostFunctionNames(t)
	found := 0
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		fn := m[1]
		found++
		if !host[fn] {
			t.Errorf("panel.js ACTION_MAP builds a call to %q, which is not defined in the CEP host", fn)
		}
	}
	if found < 8 {
		t.Fatalf("only %d ACTION_MAP builders extracted — extraction broken?", found)
	}
}

// TestCurationListsAreValid checks every curation-list entry names a real
// registered tool, no tool appears in two lists, and the env flags gate the
// exposed surface as intended.
func TestCurationListsAreValid(t *testing.T) {
	// Build an uncurated server to see the full registered surface.
	t.Setenv("MCP_EXPOSE_ALL_TOOLS", "1")
	t.Setenv("MCP_ENABLE_ESCAPE_HATCHES", "1")
	s := NewMCPServer(nil, "drift-test", zap.NewNop(), nil, nil, false)
	all := s.ListTools()

	seen := map[string]string{}
	for listName, list := range map[string][]string{
		"coreTools":        coreTools,
		"brokenTools":      brokenTools,
		"escapeHatchTools": escapeHatchTools,
	} {
		for _, tool := range list {
			if prev, dup := seen[tool]; dup {
				t.Errorf("%s appears in both %s and %s", tool, prev, listName)
			}
			seen[tool] = listName
		}
	}

	// brokenTools are deleted even in expose-all mode, so check core/escape
	// against the surface and broken against "not exposed".
	for _, tool := range coreTools {
		// Audit tools only register with a live auditor; skip them here —
		// TestAuditToolNamesStable below pins their names.
		if isAuditToolName(tool) {
			continue
		}
		if _, ok := all[tool]; !ok {
			t.Errorf("coreTools entry %q is not a registered tool", tool)
		}
	}
	for _, tool := range escapeHatchTools {
		if _, ok := all[tool]; !ok {
			t.Errorf("escapeHatchTools entry %q is not a registered tool", tool)
		}
	}
	for _, tool := range brokenTools {
		if _, ok := all[tool]; ok {
			t.Errorf("brokenTools entry %q is still exposed despite curation", tool)
		}
	}
}

func TestCuratedSurfaceSize(t *testing.T) {
	t.Setenv("MCP_EXPOSE_ALL_TOOLS", "")
	t.Setenv("MCP_ENABLE_ESCAPE_HATCHES", "")
	s := NewMCPServer(nil, "drift-test", zap.NewNop(), nil, nil, false)
	n := len(s.ListTools())
	if n < 140 || n > 260 {
		t.Errorf("curated default surface is %d tools; expected 140-260 — did curation break?", n)
	}

	t.Setenv("MCP_EXPOSE_ALL_TOOLS", "1")
	sAll := NewMCPServer(nil, "drift-test", zap.NewNop(), nil, nil, false)
	nAll := len(sAll.ListTools())
	if nAll < 900 {
		t.Errorf("expose-all surface is %d tools; expected >900", nAll)
	}
	for _, tool := range escapeHatchTools {
		if _, ok := sAll.ListTools()[tool]; ok {
			t.Errorf("escape hatch %q exposed without MCP_ENABLE_ESCAPE_HATCHES", tool)
		}
	}
}

// isAuditToolName mirrors the names registerAuditTools adds (they need a
// live auditor, absent in these constructor tests).
func isAuditToolName(name string) bool {
	switch name {
	case "premiere_get_audit_log", "premiere_what_changed", "premiere_diff_timeline",
		"premiere_get_session_digest":
		return true
	}
	return false
}
