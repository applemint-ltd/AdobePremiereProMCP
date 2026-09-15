package mcp

import (
	"os"
	"path"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/server"
	"go.uber.org/zap"
)

// Tool profiles let clients load a workflow-sized tool catalog instead of the
// complete low-level surface. Every specialized profile includes core tools.
// The default is the compact "standard" profile.
var toolProfilePatterns = map[string][]string{
	"core": {
		"premiere_ping",
		"premiere_is_running",
		"premiere_get_premiere_version",
		"premiere_get_project",
		"premiere_get_timeline",
		"premiere_get_sequence_list",
		"premiere_get_sequence_hash",
		"premiere_get_state_snapshot",
		"premiere_snapshot_timeline",
		"premiere_save_sequence_version",
		"premiere_duplicate_sequence",
		"premiere_set_active_sequence",
		"premiere_save_project",
	},
	"standard": {
		"premiere_ping",
		"premiere_is_running",
		"premiere_open",
		"premiere_health_check",
		"premiere_get_premiere_version",
		"premiere_get_project",
		"premiere_get_project_info",
		"premiere_new_project",
		"premiere_open_project",
		"premiere_save_project",
		"premiere_save_project_as",
		"premiere_close_project",
		"premiere_create_bin",
		"premiere_rename_bin",
		"premiere_delete_bin",
		"premiere_move_bin_item",
		"premiere_get_project_items",
		"premiere_find_project_items",
		"premiere_get_media_path",
		"premiere_import_media",
		"premiere_import_files",
		"premiere_import_folder",
		"premiere_fetch_slack_attachment",
		"premiere_scan_assets",
		"premiere_get_media_info",
		"premiere_relink_media",
		"premiere_replace_media",
		"premiere_get_offline_items",
		"premiere_create_sequence",
		"premiere_create_sequence_from_clips",
		"premiere_get_active_sequence",
		"premiere_set_active_sequence",
		"premiere_get_sequence_list",
		"premiere_duplicate_sequence",
		"premiere_rename_sequence",
		"premiere_delete_sequence",
		"premiere_get_sequence_settings",
		"premiere_get_sequence_duration",
		"premiere_get_timeline",
		"premiere_add_video_track",
		"premiere_add_audio_track",
		"premiere_delete_video_track",
		"premiere_delete_audio_track",
		"premiere_get_video_tracks",
		"premiere_get_audio_tracks",
		"premiere_insert_clip",
		"premiere_overwrite_clip",
		"premiere_place_clip",
		"premiere_remove_clip",
		"premiere_move_clip",
		"premiere_duplicate_clip",
		"premiere_razor_clip",
		"premiere_razor_all_tracks",
		"premiere_trim_clip_start",
		"premiere_trim_clip_end",
		"premiere_ripple_trim",
		"premiere_roll_trim",
		"premiere_slip_clip",
		"premiere_slide_clip",
		"premiere_set_clip_speed",
		"premiere_freeze_frame",
		"premiere_set_clip_enabled",
		"premiere_set_clip_name",
		"premiere_create_subclip",
		"premiere_get_all_clips",
		"premiere_get_clips_on_track",
		"premiere_get_clip_info",
		"premiere_get_clip_at_time",
		"premiere_find_gaps",
		"premiere_close_gap",
		"premiere_close_all_gaps",
		"premiere_ripple_delete_gap",
		"premiere_set_audio_level",
		"premiere_add_volume_keyframe",
		"premiere_get_volume_keyframes",
		"premiere_remove_all_audio_keyframes",
		"premiere_normalize_audio",
		"premiere_normalize_all_audio",
		"premiere_fade_in",
		"premiere_fade_out",
		"premiere_mute_audio_track",
		"premiere_unmute_all_audio_tracks",
		"premiere_solo_audio_track",
		"premiere_get_audio_level",
		"premiere_get_sequence_loudness",
		"premiere_add_music_bed",
		"premiere_duck_music_under_dialogue",
		"premiere_add_text_layer",
		"premiere_add_subtitles_from_srt",
		"premiere_create_caption_track",
		"premiere_import_captions",
		"premiere_export_captions",
		"premiere_edit_caption",
		"premiere_delete_caption",
		"premiere_adjust_subtitle_timing",
		"premiere_set_caption_position",
		"premiere_capture_frame_base64",
		"premiere_export_frame",
		"premiere_detect_scene_changes",
		"premiere_get_scene_list",
		"premiere_set_playhead_position",
		"premiere_get_playhead_position",
		"premiere_get_current_timecode",
		"premiere_go_to_timecode",
		"premiere_export",
		"premiere_export_direct",
		"premiere_export_via_ame",
		"premiere_get_exporters",
		"premiere_get_export_presets",
		"premiere_get_export_progress",
		"premiere_get_render_queue_status",
		"premiere_export_audio_only",
		"premiere_assemble_storyboard",
		"premiere_storyboard_validate",
		"premiere_storyboard_schema",
		"premiere_auto_edit",
		"premiere_parse_script",
		"premiere_get_audio_mixer_state",
		"premiere_apply_audio_effect",
		"premiere_probe_media",
		"premiere_generate_thumbnail",
		"premiere_analyze_waveform",
		"premiere_get_captions",
		"premiere_validate_closed_captions",
	},
	"dialogue": {
		"premiere_analyze_waveform",
		"premiere_probe_media",
		"premiere_*trim*",
		"premiere_*razor*",
		"premiere_*gap*",
		"premiere_add_audio_transition",
		"premiere_*audio_level*",
		"premiere_remove_clip*",
		"premiere_get_clips_on_track",
		"premiere_get_clip_info",
	},
	"captions": {
		"premiere_add_subtitles_from_srt",
		"premiere_get_captions",
		"premiere_export_captions",
		"premiere_validate_closed_captions",
	},
	"social": {
		"premiere_auto_reframe",
		"premiere_duplicate_sequence",
		"premiere_get_sequence_markers",
		"premiere_get_captions",
		"premiere_set_position",
		"premiere_set_scale",
		"premiere_export_direct",
		"premiere_export_via_ame",
		"premiere_probe_media",
	},
	"transitions": {
		"premiere_get_available_transitions",
		"premiere_get_installed_transitions",
		"premiere_get_transitions",
		"premiere_get_transition_properties",
		"premiere_get_duration_of_transition",
		"premiere_add_video_transition",
		"premiere_add_audio_transition",
		"premiere_remove_transition",
		"premiere_add_transition",
		"premiere_add_audio_crossfade",
		"premiere_set_transition_duration",
	},
	"effects": {
		"premiere_get_installed_effects",
		"premiere_get_clip_effects",
		"premiere_apply_video_effect",
		"premiere_apply_audio_effect",
		"premiere_set_effect_parameter",
		"premiere_lumetri_set_exposure",
		"premiere_lumetri_set_contrast",
		"premiere_lumetri_set_highlights",
		"premiere_lumetri_set_shadows",
		"premiere_lumetri_set_whites",
		"premiere_lumetri_set_blacks",
		"premiere_lumetri_set_saturation",
		"premiere_lumetri_set_temperature",
		"premiere_lumetri_set_tint",
		"premiere_lumetri_set_vibrance",
		"premiere_lumetri_apply_lut",
		"premiere_lumetri_remove_lut",
		"premiere_lumetri_reset",
		"premiere_lumetri_auto_color",
		"premiere_apply_lut_to_all_clips",
		"premiere_reset_color_on_all_clips",
		"premiere_set_opacity",
		"premiere_set_position",
		"premiere_set_scale",
		"premiere_set_rotation",
		"premiere_set_crop",
		"premiere_reset_crop",
		"premiere_reset_transform",
		"premiere_set_blend_mode",
		"premiere_fit_clip_to_frame",
		"premiere_center_clip",
		"premiere_scale_all_clips_to_frame",
		"premiere_get_transform_properties",
		"premiere_get_motion_properties",
	},
	"proxies": {
		"premiere_*proxy*",
		"premiere_toggle_proxies",
		"premiere_get_project_items",
		"premiere_get_media_path",
		"premiere_get_media_info",
		"premiere_get_offline_items",
		"premiere_probe_media",
	},
	"delivery": {
		"premiere_export_direct",
		"premiere_export_via_ame",
		"premiere_export_captions",
		"premiere_validate_closed_captions",
		"premiere_probe_media",
		"premiere_warm_encoder",
		"premiere_export_preview",
		"premiere_generate_contact_sheet",
		"premiere_post_file_to_slack",
	},
	"metadata": {
		"premiere_add_sequence_marker",
		"premiere_get_sequence_markers",
		"premiere_delete_sequence_marker",
		"premiere_add_clip_marker",
		"premiere_get_clip_markers",
		"premiere_delete_clip_marker",
		"premiere_delete_all_markers",
		"premiere_set_item_metadata",
		"premiere_get_item_metadata",
		"premiere_get_clip_metadata",
		"premiere_set_clip_note",
		"premiere_get_clip_note",
		"premiere_set_item_label",
	},
	"audit": {
		"premiere_dump_project_state",
		"premiere_dump_sequence_state",
		"premiere_snapshot_timeline",
		"premiere_get_state_snapshot",
		"premiere_compare_timeline_snapshots",
		"premiere_undo",
		"premiere_redo",
		"premiere_get_last_error",
		"premiere_get_audit_log",
		"premiere_what_changed",
		"premiere_diff_timeline",
		"premiere_get_session_digest",
		"premiere_get_event_history",
		"premiere_get_recent_actions",
	},
	"unsafe": {
		"premiere_evaluate_expression",
		"premiere_execute_script",
		"premiere_execute_script_with_args",
		"premiere_execute_extendscript",
		"premiere_execute_qe_script",
		"premiere_run_extend_script",
		"premiere_if_clip_exists",
		"premiere_if_sequence_open",
		"premiere_if_project_open",
		"premiere_while_condition",
		"premiere_execute_batch",
		"premiere_execute_parallel",
		"premiere_execute_with_retry",
		"premiere_execute_with_timeout",
		"premiere_schedule_script",
		"premiere_schedule_repeating",
		"premiere_list_available_scripts",
		"premiere_execute_system_command",
		"premiere_read_json_file",
		"premiere_write_json_file",
		"premiere_read_csv_file",
		"premiere_write_csv_file",
		"premiere_read_text_file",
		"premiere_write_text_file",
		"premiere_append_text_file",
		"premiere_move_media_file",
		"premiere_copy_media_file",
		"premiere_rename_media_file",
		"premiere_open_url",
		"premiere_copy_to_clipboard",
		"premiere_open_in_external_editor",
	},
}

func init() {
	// The full verified-on-2026 core set, addressable as its own profile so
	// coreTools stays the single source of truth for what is known to work.
	toolProfilePatterns["curated"] = coreTools
}

func applyToolProfiles(s *server.MCPServer, logger *zap.Logger) {
	raw := strings.TrimSpace(os.Getenv("MCP_TOOL_PROFILE"))
	if raw == "" {
		raw = "standard"
	}
	// Backwards compatibility with the pre-profile flag.
	if os.Getenv("MCP_EXPOSE_ALL_TOOLS") == "1" {
		raw = "all"
		if os.Getenv("MCP_ENABLE_ESCAPE_HATCHES") == "1" {
			raw = "all,unsafe"
		}
	}

	selected := []string{"core"}
	unknown := make([]string, 0)
	explicitCore := false
	allSelected := false
	unsafeSelected := false
	for _, value := range strings.Split(raw, ",") {
		profile := strings.ToLower(strings.TrimSpace(value))
		if profile == "" {
			continue
		}
		if profile == "core" {
			explicitCore = true
			continue
		}
		if profile == "all" {
			allSelected = true
			continue
		}
		if profile == "unsafe" {
			unsafeSelected = true
		}
		if _, ok := toolProfilePatterns[profile]; !ok {
			unknown = append(unknown, profile)
			continue
		}
		selected = append(selected, profile)
	}

	if len(unknown) > 0 {
		sort.Strings(unknown)
		logger.Warn("ignoring unknown MCP tool profiles", zap.Strings("profiles", unknown))
	}
	if len(selected) == 1 && len(unknown) > 0 && !explicitCore && !unsafeSelected {
		// A typo should not unexpectedly reduce the default catalog to core.
		selected = append(selected, "standard")
	}

	if allSelected {
		if unsafeSelected {
			return
		}
		removeToolsMatching(s, toolProfilePatterns["unsafe"])
		logger.Info("applied safe all-tools profile", zap.Int("tools", len(s.ListTools())))
		return
	}

	patterns := make([]string, 0)
	for _, profile := range selected {
		patterns = append(patterns, toolProfilePatterns[profile]...)
	}

	tools := s.ListTools()
	remove := make([]string, 0, len(tools))
	for name := range tools {
		if !matchesAnyToolPattern(name, patterns) {
			remove = append(remove, name)
		}
	}
	s.DeleteTools(remove...)
	logger.Info("applied MCP tool profiles",
		zap.Strings("profiles", selected),
		zap.Int("tools", len(tools)-len(remove)),
	)
}

func removeToolsMatching(s *server.MCPServer, patterns []string) {
	remove := make([]string, 0)
	for name := range s.ListTools() {
		if matchesAnyToolPattern(name, patterns) {
			remove = append(remove, name)
		}
	}
	s.DeleteTools(remove...)
}

func matchesAnyToolPattern(name string, patterns []string) bool {
	for _, pattern := range patterns {
		matched, err := path.Match(pattern, name)
		if err == nil && matched {
			return true
		}
	}
	return false
}
