package mcp

import "strings"

// ToolClass describes how the audit layer should treat a tool.
//
// Mutating tools get a full audit record flagged as a state change; Snapshot
// additionally takes a timeline snapshot before the call so the change can be
// diffed afterwards. Unknown tools default to mutating+snapshot — safe (never
// misses a change) at the cost of one extra bridge round trip, and the golden
// -list test keeps the classifier explicit as tools are added.
type ToolClass struct {
	Mutating bool
	Snapshot bool
}

// readOnlyPrefixes match tools that only observe state. Names are matched
// after stripping the "premiere_" prefix.
var readOnlyPrefixes = []string{
	"get_", "list_", "is_", "check_", "find_", "compare_", "detect_",
	"analyze_", "browse_", "search_", "dump_", "read_", "has_", "if_",
	"estimate_", "watch_", "filter_by_", "validate_",
}

// readOnlyExact are read-only tools whose names don't share a prefix.
var readOnlyExact = map[string]bool{
	"ping":                  true,
	"health_check":          true,
	"health_monitor":        true,
	"test_bridge_connection": true,
	"convert_timecode":      true,
	"clipboard_has_content": true,
	"snapshot_timeline":     true, // reads state; persisting it is the caller's business
	"generate_edit_summary": true,
	"suggest_music":         true,
	"suggest_replacements":  true,
	"storyboard_schema":     true,
	"storyboard_validate":   true,
	"what_changed":          true,
	"diff_timeline":         true,
	"get_session_digest":    true,
}

// noSnapshotPrefixes match tools that do change something (or have side
// effects worth auditing) but where a pre-call timeline snapshot is useless
// or wasteful: playback/navigation, UI/panel/window chrome, exports and
// project-file plumbing, and app lifecycle.
var noSnapshotPrefixes = []string{
	"play", "pause", "stop", "step_", "shuttle_", "go_to_", "toggle_",
	"zoom_", "scroll_", "set_playhead", "set_zoom", "set_timeline_zoom",
	"loop_", "enter_", "exit_",
	"show_", "hide_", "open_panel", "close_panel", "maximize_", "minimize_",
	"undock_", "reset_panel", "set_workspace", "save_workspace",
	"expand_", "collapse_", "set_track_height",
	"export", "convert_to_", "queue_", "launch_", "start_ame",
	"render_", "add_to_render_queue", "pause_render", "resume_render",
	"clear_render", "clear_media_encoder",
	"save_", "open", "close", "new_project", "auto_save",
	"reveal_", "capture_", "fetch_", "post_", "download_", "write_",
	"append_", "copy_media_file", "clean_", "clear_media_cache",
	"archive_", "create_project_backup", "create_project_report",
	"select_", "deselect_", "invert_selection", "clear_selection",
	"set_active_track", "set_audio_track_target", "set_video_track_target",
	"set_in_point", "set_out_point", "clear_in_out",
	"log_to_", "write_to_console", "enable_debug", "set_error_logging",
	"clear_errors", "clear_event_history", "clear_debug",
	"set_global_variable", "clear_global_variables",
	"register_event", "unregister_event", "schedule_", "cancel_scheduled",
	"simulate_", "execute_menu", "execute_system", "execute_extendscript",
	"execute_qe", "execute_script", "run_extend", "evaluate_expression",
	"execute_batch", "execute_parallel", "execute_with", "run_qa",
	"set_snapping", "set_timeline_snap", "set_time_display",
	"set_playback_resolution", "set_program_monitor",
	"set_source_monitor", "fit_program_monitor", "match_frame",
	"reverse_match_frame", "open_in_", "edit_in_", "send_to_",
	"import_", "batch_import", "scan_", "relink_", "make_offline",
	"refresh_", "attach_proxy", "detach_proxy", "create_proxy",
	"toggle_proxies", "set_scratch", "set_media_cache", "set_memory",
	"set_ingest", "create_ingest",
}

// classifyTool decides the audit treatment for an MCP tool name.
func classifyTool(name string) ToolClass {
	n := strings.TrimPrefix(name, "premiere_")

	if readOnlyExact[n] {
		return ToolClass{}
	}
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(n, p) {
			return ToolClass{}
		}
	}
	for _, p := range noSnapshotPrefixes {
		if strings.HasPrefix(n, p) {
			return ToolClass{Mutating: true}
		}
	}
	return ToolClass{Mutating: true, Snapshot: true}
}
