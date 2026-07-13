package mcp

import (
	"context"

	gomcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// registerResources registers all MCP resources with the server.
// Resources provide static context that AI assistants can read to understand
// how to work with this MCP server and Premiere Pro.
func registerResources(s *server.MCPServer) {
	s.AddResource(
		gomcp.NewResource(
			"config://premiere-instructions",
			"Premiere Pro MCP Instructions",
			gomcp.WithResourceDescription("Instructions for controlling Adobe Premiere Pro via this MCP server"),
			gomcp.WithMIMEType("text/plain"),
		),
		handlePremiereInstructions,
	)

	s.AddResource(
		gomcp.NewResource(
			"config://tool-categories",
			"Tool Categories",
			gomcp.WithResourceDescription("List of all tool categories with descriptions"),
			gomcp.WithMIMEType("text/plain"),
		),
		handleToolCategories,
	)

	s.AddResource(
		gomcp.NewResource(
			"config://extendscript-reference",
			"ExtendScript Quick Reference",
			gomcp.WithResourceDescription("Quick ExtendScript API reference for Premiere Pro"),
			gomcp.WithMIMEType("text/plain"),
		),
		handleExtendScriptReference,
	)

	s.AddResource(
		gomcp.NewResource(
			"config://project-defaults",
			"Project Defaults",
			gomcp.WithResourceDescription("Default project settings and paths used by the MCP server"),
			gomcp.WithMIMEType("text/plain"),
		),
		handleProjectDefaults,
	)
}

// ---------------------------------------------------------------------------
// Resource handlers
// ---------------------------------------------------------------------------

func handlePremiereInstructions(
	_ context.Context,
	_ gomcp.ReadResourceRequest,
) ([]gomcp.ResourceContents, error) {
	return []gomcp.ResourceContents{
		gomcp.TextResourceContents{
			URI:      "config://premiere-instructions",
			MIMEType: "text/plain",
			Text: `You are controlling Adobe Premiere Pro 2026 via the PremierPro MCP server.
The exposed tool set is a curated, verified-working subset — trust it over
memory of other Premiere scripting surfaces.

THE GOLDEN WORKFLOW (storyboard + clips -> reviewable cut):
 1. premiere_ping — confirm the pipeline is alive; premiere_health_check for detail.
 2. premiere_get_project / premiere_new_project / premiere_open_project.
 3. Get media in: premiere_fetch_slack_attachment (Slack uploads),
    premiere_import_media / premiere_import_files, premiere_scan_assets for folders.
 4. Build the cut with the STORYBOARD pipeline (preferred):
    premiere_storyboard_schema -> emit/collect a storyboard (JSON, shot-list
    CSV, or script via script_text + assets_directory) ->
    premiere_storyboard_validate (dry run; show the user unresolved clips) ->
    premiere_assemble_storyboard (per-shot report: placements, transitions
    applied-or-reported, baked-PNG text, SRT captions, music bed).
    For manual edits: premiere_insert_clip / premiere_overwrite_clip
    (non-ripple), premiere_create_subclip + premiere_trim_clip_end,
    premiere_add_transition. premiere_auto_edit runs script->match->assemble
    end-to-end through the same executor.
 5. Show your work: premiere_capture_frame_base64 for a frame of the timeline.
 6. Iterate with clip/audio/lumetri/transform tools.
 7. Export: premiere_export (semantic presets: h264_1080p, h264_4k, prores_422,
    prores_4444, dnxhd, gif — or a real .epr path). Exports QUEUE in Adobe
    Media Encoder; poll premiere_get_export_progress rather than assuming done.
 8. Explain what you did: premiere_get_session_digest ("what did you do?"),
    premiere_what_changed (timeline diff since the last edit).

PREMIERE 2026 HARD LIMITS (do not fight these):
- On-screen text: scripted native/Essential Graphics text DOES NOT RENDER.
  premiere_add_text_layer is the ONLY working text method — it bakes the text
  to a PNG and places it as a clip. To change it, re-run the tool.
- Subtitles: there is no auto-transcription. Captions come from an SRT file
  (premiere_add_subtitles_from_srt) — native, editable, works.
- Frame stills come from premiere_capture_frame_base64 / premiere_export_frame
  (takes up to ~8s per frame; renders via a still-export preset).
- Scene detection: premiere_detect_scene_changes (real ffmpeg analysis of a
  media file). There is no working silence detection.

CONVENTIONS:
- Track indices are zero-based; video track 0 = V1.
- Time positions are seconds (floating point).
- Every tool call is audit-logged with a correlation ID; failures are honest —
  an error means it did not happen, an ok means it did.
- When talking to end users (e.g. via Slack), describe changes in plain
  language: clip names and m:ss times, never track indices or raw seconds.`,
		},
	}, nil
}

func handleToolCategories(
	_ context.Context,
	_ gomcp.ReadResourceRequest,
) ([]gomcp.ResourceContents, error) {
	return []gomcp.ResourceContents{
		gomcp.TextResourceContents{
			URI:      "config://tool-categories",
			MIMEType: "text/plain",
			Text: `PremierPro MCP Tool Categories (curated for Premiere Pro 2026)
==============================================================

By default only a verified core set (~185 tools) is exposed. Categories:

1.  Connection & health — premiere_ping, premiere_is_running, premiere_open,
    premiere_close, premiere_health_check, premiere_test_bridge_connection
2.  Project & bins — premiere_get_project(_info), premiere_new/open/save/
    close_project, premiere_create/rename/delete_bin, premiere_get_project_items,
    premiere_find_project_items
3.  Import & media — premiere_import_media/files/folder,
    premiere_fetch_slack_attachment (Slack uploads), premiere_scan_assets,
    premiere_get_media_info, premiere_relink_media, premiere_replace_media
4.  Sequences & tracks — premiere_create_sequence(_from_clips),
    premiere_get/set_active_sequence, premiere_get_sequence_list,
    premiere_add/delete_video/audio_track, premiere_get_timeline
5.  Clip editing — premiere_insert/overwrite/place_clip, premiere_remove/
    move/duplicate_clip, premiere_razor_clip, premiere_trim_clip_start/end,
    premiere_ripple/roll_trim, premiere_slip/slide_clip, premiere_set_clip_speed,
    premiere_create_subclip, premiere_get_all_clips, premiere_find_gaps,
    premiere_close_(all_)gap(s)
6.  Transitions — premiere_add_transition (+video/audio variants),
    premiere_add_audio_crossfade, premiere_get_available_transitions
7.  Audio — premiere_set_audio_gain/level, premiere_normalize_(all_)audio,
    premiere_fade_in/out, volume keyframes, track mute/solo/volume,
    premiere_add_music_bed, premiere_duck_music_under_dialogue
8.  Color (Lumetri) — premiere_lumetri_set_* (exposure/contrast/temperature/...),
    premiere_lumetri_apply/remove_lut, premiere_lumetri_auto_color/reset/get_all
9.  Transform — premiere_set_position/scale/rotation/opacity/crop,
    premiere_fit_clip_to_frame, premiere_center_clip, premiere_scale_all_clips_to_frame
10. Markers & metadata — sequence/clip markers, item metadata, labels, notes
11. Text & captions — premiere_add_text_layer (baked-PNG text: the ONLY
    working text on 2026), premiere_add_subtitles_from_srt, caption editing
12. Preview & analysis — premiere_capture_frame_base64, premiere_export_frame,
    premiere_detect_scene_changes, playhead/timecode navigation
13. Export — premiere_export (the real path; AME-queued), premiere_export_direct,
    premiere_export_via_ame, premiere_get_exporters/export_presets/export_progress
14. Assembly & pipeline — premiere_assemble_from_csv/edl/folder_order,
    premiere_create_slideshow, premiere_auto_edit, premiere_parse_script
15. State, audit & safety — premiere_dump_project/sequence_state,
    premiere_snapshot_timeline, premiere_undo/redo, premiere_get_audit_log,
    premiere_what_changed, premiere_diff_timeline, premiere_get_session_digest

Not exposed by default:
- Broken-on-2026 tools (missing host functions, removed APIs like
  exportFramePNG, fake-success placeholders) are never registered.
- Arbitrary-execution escape hatches (execute_extendscript, execute_system_command,
  file writers, schedulers) require MCP_ENABLE_ESCAPE_HATCHES=1.
- The remaining long tail (UI/workspace/panel/menu control, QE-heavy tools,
  Team Projects, VR/HDR, platform export variants, deep analytics reports)
  requires MCP_EXPOSE_ALL_TOOLS=1. Neither flag belongs on the shared hub.`,
		},
	}, nil
}

func handleExtendScriptReference(
	_ context.Context,
	_ gomcp.ReadResourceRequest,
) ([]gomcp.ResourceContents, error) {
	return []gomcp.ResourceContents{
		gomcp.TextResourceContents{
			URI:      "config://extendscript-reference",
			MIMEType: "text/plain",
			Text: `ExtendScript Quick Reference for Adobe Premiere Pro
====================================================

The MCP server wraps ExtendScript calls internally. This reference
is for understanding what operations are possible and how the
underlying API works.

Core Objects:
  app                       The Application object
  app.project               Current project (Project)
  app.project.activeSequence Active sequence (Sequence)
  app.project.rootItem      Root bin (ProjectItem)

Project:
  app.project.name          Project name
  app.project.path          Project file path
  app.project.sequences     Array of all Sequence objects
  app.project.importFiles(paths)         Import media files
  app.project.createNewSequence(name)    Create a new sequence
  app.project.openSequence(id)           Set active sequence

ProjectItem:
  item.name                 Item name
  item.type                 1=clip, 2=bin, 3=root, 4=file
  item.treePath             Full path in project panel
  item.getMediaPath()       File path on disk
  item.setInPoint(secs)     Set source in point
  item.setOutPoint(secs)    Set source out point
  item.children             Array of child items (for bins)
  item.createBin(name)      Create sub-bin
  item.moveBin(destBin)     Move to another bin

Sequence:
  seq.name                  Sequence name
  seq.sequenceID            Unique ID
  seq.videoTracks           Array of Track objects
  seq.audioTracks           Array of Track objects
  seq.getPlayerPosition()   Current playhead position (Time)
  seq.setPlayerPosition(t)  Set playhead position
  seq.setInPoint(secs)      Set sequence in point
  seq.setOutPoint(secs)     Set sequence out point
  seq.getInPoint()          Get sequence in point
  seq.getOutPoint()         Get sequence out point
  seq.insertClip(item, t)   Insert at position
  seq.overwriteClip(item,t) Overwrite at position
  seq.createSubSequence()   Create subsequence

Track:
  track.clips               Array of TrackItem objects
  track.name                Track name
  track.id                  Track index
  track.isMuted()           Check if muted
  track.setMute(bool)       Mute/unmute

TrackItem (Clip):
  clip.name                 Clip name
  clip.start                Start time on timeline (Time)
  clip.end                  End time on timeline (Time)
  clip.duration             Clip duration (Time)
  clip.inPoint              Source in point (Time)
  clip.outPoint             Source out point (Time)
  clip.type                 1=clip, 2=transition
  clip.components           Array of Component (effects)
  clip.remove(false, false) Remove from timeline
  clip.disabled             Is clip disabled

Component (Effect):
  comp.displayName          Effect name
  comp.properties           Array of ComponentParam
  comp.matchName            Internal match name

ComponentParam:
  param.displayName         Parameter name
  param.getValue()          Current value
  param.setValue(v, true)    Set value (with undo)
  param.addKey(time)        Add keyframe
  param.removeKey(time)     Remove keyframe
  param.getKeys()           Array of keyframe times

Time:
  time.seconds              Time in seconds (float)
  time.ticks                Time in ticks (string)

Common Patterns:
  // Get active sequence clips on video track 0
  var track = app.project.activeSequence.videoTracks[0];
  for (var i = 0; i < track.clips.numItems; i++) {
      var clip = track.clips[i];
      // work with clip
  }

  // Apply effect by matchName
  var fx = qe.project.getVideoEffectByName("matchName");

  // Lumetri Color match name: "Lumetri Color"
  // Cross Dissolve match name: "Cross Dissolve"`,
		},
	}, nil
}

func handleProjectDefaults(
	_ context.Context,
	_ gomcp.ReadResourceRequest,
) ([]gomcp.ResourceContents, error) {
	return []gomcp.ResourceContents{
		gomcp.TextResourceContents{
			URI:      "config://project-defaults",
			MIMEType: "text/plain",
			Text: `PremierPro MCP Default Project Settings
========================================

Sequence Defaults:
  Resolution:        1920x1080 (Full HD)
  Frame Rate:        24 fps
  Pixel Aspect:      Square Pixels (1.0)
  Video Tracks:      3
  Audio Tracks:      2
  Audio Sample Rate: 48000 Hz
  Audio Bit Depth:   16-bit

Export Presets:
  h264_1080p    H.264, 1920x1080, ~20 Mbps VBR
  h264_4k       H.264, 3840x2160, ~50 Mbps VBR
  prores_422    Apple ProRes 422
  prores_4444   Apple ProRes 4444 (with alpha)
  dnxhd         Avid DNxHR HQX
  gif           Animated GIF (low res)

Supported Media Formats (Import):
  Video:  .mp4, .mov, .avi, .mkv, .mxf, .r3d, .braw, .ari
  Audio:  .wav, .mp3, .aac, .aif, .flac, .ogg
  Image:  .png, .jpg, .jpeg, .tiff, .psd, .exr, .dpx
  Other:  .mogrt, .prproj, .xml, .edl, .aaf, .omf

Project File Paths:
  Premiere Pro projects use the .prproj extension.
  Auto-save:       ~/Documents/Adobe/Premiere Pro Auto-Save/
  Media Cache:     ~/Library/Application Support/Adobe/Common/Media Cache Files/
  Presets:         ~/Documents/Adobe/Premiere Pro/<version>/Profile-<user>/Settings/Export/
  Effect Presets:  ~/Documents/Adobe/Premiere Pro/<version>/Profile-<user>/Effect Presets/

Track Index Convention:
  Track indices are zero-based.
  Video track 0 is the bottom-most video track (V1 in Premiere UI).
  Audio track 0 is the top-most audio track (A1 in Premiere UI).

Timecode:
  Positions are in seconds (float64). For example, 61.5 = 1 minute, 1.5 seconds.
  The server converts seconds to internal timecode representation automatically.

Speed:
  Default playback speed is 1.0 (100%).
  Values < 1.0 create slow motion, > 1.0 create fast motion.
  Negative values reverse playback.`,
		},
	}, nil
}
