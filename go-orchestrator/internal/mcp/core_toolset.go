package mcp

import (
	"os"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

// Tool-surface curation.
//
// ~1,065 tools are registered, but on Premiere Pro 2026 roughly a quarter to
// a third are broken (missing host functions, removed APIs, QE-DOM
// dependencies) or misleading (fake-success placeholders), and many are
// near-duplicates. A headless agent picking from that surface fails
// unpredictably, so by default only the verified core set below is exposed.
//
// Policy per category:
//   - coreTools: verified working on 2026 (or Go-orchestrated); exposed by
//     default. New tools must be added here explicitly to be visible.
//   - brokenTools: NEVER registered, regardless of flags — they either call
//     ExtendScript functions that do not exist, use APIs removed in 2026, or
//     fabricate success. Entries leave this list only when actually fixed.
//   - escapeHatchTools: arbitrary-execution tools. Gated separately behind
//     MCP_ENABLE_ESCAPE_HATCHES=1 because the Slack bot allowlists the
//     wildcard mcp__premierpro-mcp__* — anything registered is bot-callable.
//   - Everything else (UI/workspace/panel/menu chrome, QE-heavy long tail,
//     Team Projects, VR/HDR, platform-specific export variants, deep
//     analytics reports): behind MCP_EXPOSE_ALL_TOOLS=1 for local
//     experimentation. The hub launchd environment must set neither flag.

var coreTools = []string{
	// Connection / health
	"premiere_ping", "premiere_is_running", "premiere_open", "premiere_close",
	"premiere_health_check", "premiere_get_premiere_version", "premiere_test_bridge_connection",
	"premiere_get_bridge_latency",

	// Project / bins
	"premiere_get_project", "premiere_get_project_info", "premiere_new_project",
	"premiere_open_project", "premiere_save_project", "premiere_save_project_as",
	"premiere_close_project", "premiere_auto_save_now",
	"premiere_create_bin", "premiere_rename_bin", "premiere_delete_bin", "premiere_move_bin_item",
	"premiere_get_project_items", "premiere_find_project_items", "premiere_get_media_path",

	// Import / media
	"premiere_import_media", "premiere_import_files", "premiere_import_folder",
	"premiere_fetch_slack_attachment", "premiere_scan_assets", "premiere_get_media_info",
	"premiere_relink_media", "premiere_replace_media", "premiere_get_offline_items",

	// Sequences / tracks
	"premiere_create_sequence", "premiere_create_sequence_from_clips",
	"premiere_get_active_sequence", "premiere_set_active_sequence", "premiere_get_sequence_list",
	"premiere_duplicate_sequence", "premiere_rename_sequence", "premiere_delete_sequence",
	"premiere_get_sequence_settings", "premiere_get_sequence_duration", "premiere_get_timeline",
	"premiere_add_video_track", "premiere_add_audio_track",
	"premiere_delete_video_track", "premiere_delete_audio_track",
	"premiere_get_video_tracks", "premiere_get_audio_tracks",

	// Clip editing
	"premiere_insert_clip", "premiere_overwrite_clip", "premiere_place_clip",
	"premiere_remove_clip", "premiere_move_clip", "premiere_duplicate_clip",
	"premiere_razor_clip", "premiere_razor_all_tracks",
	"premiere_trim_clip_start", "premiere_trim_clip_end", "premiere_ripple_trim",
	"premiere_roll_trim", "premiere_slip_clip", "premiere_slide_clip",
	"premiere_set_clip_speed", "premiere_get_clip_speed",
	// reverse_clip demoted (long tail): fails "Illegal Parameter type" on 2026.
	"premiere_freeze_frame", "premiere_set_clip_enabled", "premiere_set_clip_name",
	"premiere_create_subclip", "premiere_get_all_clips", "premiere_get_clips_on_track",
	"premiere_get_clip_info", "premiere_get_clip_at_time",
	"premiere_find_gaps", "premiere_close_gap", "premiere_close_all_gaps", "premiere_ripple_delete_gap",

	// Transitions
	"premiere_add_transition", "premiere_add_video_transition", "premiere_add_audio_transition",
	"premiere_add_audio_crossfade", "premiere_remove_transition", "premiere_set_transition_duration",
	"premiere_get_available_transitions", "premiere_get_transitions",

	// Audio (set_audio_gain demoted to long tail: plain imported project items
	// don't expose setAudioGain on 2026 — use set_audio_track_volume /
	// normalize_audio / set_audio_level for leveling instead)
	"premiere_set_audio_level", "premiere_add_volume_keyframe",
	"premiere_get_volume_keyframes", "premiere_remove_all_audio_keyframes",
	"premiere_normalize_audio", "premiere_normalize_all_audio",
	"premiere_fade_in", "premiere_fade_out",
	"premiere_mute_audio_track", "premiere_unmute_all_audio_tracks", "premiere_solo_audio_track",
	// set_audio_track_volume demoted (long tail): the track Volume component
	// isn't reachable via script on 2026 ("Could not find track volume
	// parameter"). Per-clip leveling (set_audio_level / normalize_audio) works.
	"premiere_get_audio_level", "premiere_get_sequence_loudness",
	"premiere_add_music_bed", "premiere_duck_music_under_dialogue",

	// Color (Lumetri basics)
	"premiere_lumetri_set_exposure", "premiere_lumetri_set_contrast",
	"premiere_lumetri_set_highlights", "premiere_lumetri_set_shadows",
	"premiere_lumetri_set_whites", "premiere_lumetri_set_blacks",
	"premiere_lumetri_set_saturation", "premiere_lumetri_set_temperature",
	"premiere_lumetri_set_tint", "premiere_lumetri_set_vibrance",
	"premiere_lumetri_apply_lut", "premiere_lumetri_remove_lut",
	"premiere_lumetri_reset", "premiere_lumetri_auto_color",
	"premiere_apply_lut_to_all_clips", "premiere_reset_color_on_all_clips",
	// lumetri_get_all demoted (long tail): its LUTAsset/LookAsset values carry
	// a control char that breaks JSON re-parse in transit on 2026.

	// Transform / basic effects
	"premiere_set_opacity", "premiere_set_position", "premiere_set_scale", "premiere_set_rotation",
	"premiere_set_crop", "premiere_reset_crop", "premiere_reset_transform", "premiere_set_blend_mode",
	"premiere_fit_clip_to_frame", "premiere_center_clip", "premiere_scale_all_clips_to_frame",
	"premiere_get_transform_properties", "premiere_get_motion_properties",

	// Markers / metadata
	"premiere_add_sequence_marker", "premiere_get_sequence_markers", "premiere_delete_sequence_marker",
	"premiere_add_clip_marker", "premiere_get_clip_markers", "premiere_delete_clip_marker",
	"premiere_delete_all_markers",
	"premiere_set_item_metadata", "premiere_get_item_metadata", "premiere_get_clip_metadata",
	"premiere_set_clip_note", "premiere_get_clip_note", "premiere_set_item_label",

	// Text / captions (text = the baked-PNG layer; native scripted text does
	// not render on 2026)
	"premiere_add_text_layer",
	"premiere_add_subtitles_from_srt", "premiere_create_caption_track",
	"premiere_import_captions", "premiere_export_captions",
	// get_captions demoted (long tail): 2026 exposes no scripted caption read.
	"premiere_edit_caption", "premiere_delete_caption",
	"premiere_adjust_subtitle_timing", "premiere_set_caption_position",

	// Preview / analysis
	"premiere_capture_frame_base64", "premiere_export_frame",
	// batch_export_frames demoted (long tail): its host loop calls the now-
	// async exportFrame and reports success before AME writes any file (and
	// restores the playhead before the queued jobs render). Needs a Go-side
	// reroute (one preview -> ffmpeg-extract N frames) before it's core again.
	"premiere_detect_scene_changes", "premiere_get_scene_list",
	"premiere_set_playhead_position", "premiere_get_playhead_position",
	"premiere_get_current_timecode", "premiere_go_to_timecode",

	// Export (the real paths)
	"premiere_export", "premiere_export_direct", "premiere_export_via_ame",
	"premiere_get_exporters", "premiere_get_export_presets",
	"premiere_get_export_progress", "premiere_get_render_queue_status",
	"premiere_export_audio_only",

	// Remote review loop
	"premiere_warm_encoder", "premiere_export_preview", "premiere_generate_contact_sheet", "premiere_post_file_to_slack",

	// Assembly / pipeline (the golden path). The legacy assemble_from_* trio
	// and create_slideshow stay in the long tail: they silently drop
	// transitions/in-out points; the storyboard assembler replaces them.
	"premiere_assemble_storyboard", "premiere_storyboard_validate", "premiere_storyboard_schema",
	"premiere_auto_edit", "premiere_parse_script",

	// Audio mixer readback (gates the audio-mix prompt)
	"premiere_get_audio_mixer_state", "premiere_apply_audio_effect",

	// State / audit / safety
	"premiere_dump_project_state", "premiere_dump_sequence_state", "premiere_snapshot_timeline",
	"premiere_get_state_snapshot", "premiere_compare_timeline_snapshots",
	"premiere_undo", "premiere_redo", "premiere_get_last_error",
	"premiere_get_audit_log", "premiere_what_changed", "premiere_diff_timeline",
	"premiere_get_session_digest", "premiere_get_event_history", "premiere_get_recent_actions",
}

// brokenTools call ExtendScript functions that do not exist, depend on APIs
// removed in Premiere 2026, or return fabricated success. Never registered.
// NOTE: upstream #13 implemented the host functions behind several entries
// below that were previously marked broken purely because the ExtendScript
// function did not exist (selectAll, listExportPresetsFromDisk, exportEDLFile,
// estimateRenderTime, findSimilarClips, and ~20 more). They remain denied
// because nothing here has been verified against live Premiere Pro 2026 yet —
// move an entry out only after testing it in the app.
var brokenTools = []string{
	// Missing host functions (EvalCommand target absent from premiere.jsx/core.jsx)
	"premiere_generate_rough_cut", "premiere_create_social_cuts", "premiere_refine_edit",
	"premiere_smart_cut", "premiere_smart_trim", "premiere_auto_color_match",
	"premiere_auto_audio_levels", "premiere_suggest_transitions", "premiere_generate_trailer",
	"premiere_analyze_clip", "premiere_analyze_sequence", "premiere_generate_edit_summary",
	"premiere_suggest_music", "premiere_suggest_replacements", "premiere_detect_audio_issues",
	"premiere_find_similar_clips", "premiere_auto_organize_project", "premiere_create_review_markers",
	"premiere_check_delivery_specs", "premiere_create_project_report", "premiere_add_broll_suggestions",
	"premiere_tag_clips", "premiere_export_edl_file", "premiere_import_omf",
	"premiere_list_export_presets_disk", "premiere_select_all", "premiere_get_sequence_statistics",

	// seq.exportFramePNG removed in 2026 -> these silently produce nothing.
	// (premiere_generate_contact_sheet was in this class but is re-implemented
	// Go-side over ffmpeg in review_tools.go, replacing the broken handler.)
	"premiere_generate_storyboard",
	"premiere_generate_clip_thumbnail", "premiere_generate_sequence_thumbnail",
	"premiere_create_thumbnail_from_frame",

	// Fake success / placeholder data
	"premiere_export_for_youtube", "premiere_export_for_instagram", "premiere_export_for_tiktok",
	"premiere_export_for_twitter", "premiere_export_for_web", "premiere_export_for_mobile",
	"premiere_export_for_streaming", "premiere_export_for_broadcast", "premiere_export_for_archive",
	"premiere_create_vertical_version", "premiere_create_square_version",
	"premiere_create_teaser", "premiere_create_bumper",
	"premiere_estimate_render_time", "premiere_simulate_key_press",

	// Instruction-only stubs (tell the human to click UI; nothing happens)
	"premiere_auto_generate_subtitles", "premiere_burn_in_subtitles",
	"premiere_detect_silence", "premiere_get_undo_history",
	"premiere_detect_scene_edits", // uses an unavailable Premiere API; detect_scene_changes is the real one
}

// escapeHatchTools execute arbitrary code or system commands. Only
// registered with MCP_ENABLE_ESCAPE_HATCHES=1, and never on the hub.
var escapeHatchTools = []string{
	"premiere_execute_extendscript", "premiere_execute_qe_script", "premiere_execute_script",
	"premiere_execute_script_with_args", "premiere_run_extend_script", "premiere_evaluate_expression",
	"premiere_execute_batch", "premiere_execute_parallel", "premiere_execute_with_retry",
	"premiere_execute_with_timeout", "premiere_execute_system_command",
	"premiere_execute_menu_command", "premiere_execute_menu_item", "premiere_simulate_menu_click",
	"premiere_write_text_file", "premiere_write_json_file", "premiere_write_csv_file",
	"premiere_append_text_file",
	"premiere_schedule_script", "premiere_schedule_repeating", "premiere_cancel_scheduled_script",
}

// stripUnsafeTools removes the tools that must never reach a client on
// Premiere Pro 2026, regardless of which profile the operator selected:
// brokenTools (missing host functions, removed APIs, fabricated success) are
// always dropped, and escapeHatchTools stay hidden unless
// MCP_ENABLE_ESCAPE_HATCHES=1. It runs before applyToolProfiles so that no
// profile — including "all" — can widen the surface back onto broken tools.
func stripUnsafeTools(s *server.MCPServer, logger *zap.Logger) {
	all := s.ListTools()
	// Either documented gate opens the escape hatches: the dedicated env var,
	// or an explicit "unsafe" entry in MCP_TOOL_PROFILE. Neither is on by
	// default, so the hatches stay closed unless asked for by name.
	enableEscape := os.Getenv("MCP_ENABLE_ESCAPE_HATCHES") == "1" || profileSelectionIncludesUnsafe()

	var del []string
	for _, name := range brokenTools {
		if _, ok := all[name]; ok {
			del = append(del, name)
		}
	}
	if !enableEscape {
		for _, name := range escapeHatchTools {
			if _, ok := all[name]; ok {
				del = append(del, name)
			}
		}
	}
	if len(del) > 0 {
		s.DeleteTools(del...)
	}

	logger.Info("stripped unsafe tools",
		zap.Int("registered", len(all)),
		zap.Int("removed", len(del)),
		zap.Bool("escape_hatches", enableEscape),
	)
}

// applyToolCuration narrows the surface to coreTools unless
// MCP_EXPOSE_ALL_TOOLS=1. It is only consulted when the operator has not
// selected a profile via MCP_TOOL_PROFILE; profiles do the shaping otherwise.
// Called once from NewMCPServer after registration, before any session exists
// (DeleteTools would notify live sessions otherwise) — so curation is
// startup-only by design.
func applyToolCuration(s *server.MCPServer, logger *zap.Logger) {
	all := s.ListTools()

	core := make(map[string]bool, len(coreTools))
	for _, name := range coreTools {
		core[name] = true
		if _, ok := all[name]; !ok {
			logger.Warn("coreTools entry is not a registered tool", zap.String("tool", name))
		}
	}

	exposeAll := os.Getenv("MCP_EXPOSE_ALL_TOOLS") == "1"

	var del []string
	for name := range all {
		if !core[name] && !exposeAll {
			del = append(del, name)
		}
	}
	if len(del) > 0 {
		s.DeleteTools(del...)
	}

	logger.Info("tool curation applied",
		zap.Int("registered", len(all)),
		zap.Int("exposed", len(all)-len(del)),
		zap.Bool("expose_all", exposeAll),
	)
}

// profileSelectionIncludesUnsafe reports whether MCP_TOOL_PROFILE names the
// "unsafe" profile, which is the profile-side gate for escapeHatchTools.
func profileSelectionIncludesUnsafe() bool {
	for _, value := range strings.Split(os.Getenv("MCP_TOOL_PROFILE"), ",") {
		if strings.EqualFold(strings.TrimSpace(value), "unsafe") {
			return true
		}
	}
	return false
}
