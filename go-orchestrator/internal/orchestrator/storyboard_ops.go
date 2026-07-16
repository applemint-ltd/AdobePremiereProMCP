package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/internal/storyboard"
)

// ---------------------------------------------------------------------------
// Storyboard assembly — the golden path from "storyboard + clips" to a
// timeline. The executor is a Go-orchestrated loop over verified-working
// host primitives (createSubclip, overwriteClip, trimClipEnd, addTransition,
// AddTextLayer, addSubtitlesFromSRT), producing one honest per-shot result
// each — never a single opaque call that can silently half-work.
// ---------------------------------------------------------------------------

// AssembleStoryboardOptions tweaks execution.
type AssembleStoryboardOptions struct {
	SequenceName string // overrides the storyboard's sequence name
	StopOnError  bool   // stop at the first failed shot instead of continuing
}

// ShotReport is the outcome for one shot.
type ShotReport struct {
	ShotID          string  `json:"shot_id"`
	Clip            string  `json:"clip,omitempty"`
	Status          string  `json:"status"` // placed | text_card | skipped_unresolved | failed | failed_verification
	StartSeconds    float64 `json:"start_seconds"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	Detail          string  `json:"detail,omitempty"`
	VONote          string  `json:"vo_note,omitempty"`
}

// TransitionReport is the outcome for one requested transition.
type TransitionReport struct {
	AfterShot string `json:"after_shot"`
	Name      string `json:"name"`
	Applied   bool   `json:"applied"`
	Detail    string `json:"detail,omitempty"`
}

// CaptionsReport summarizes the generated caption track.
type CaptionsReport struct {
	Count   int    `json:"count"`
	Applied bool   `json:"applied"`
	SRTPath string `json:"srt_path,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// AssemblyReport is the full, honest outcome of a storyboard assembly.
type AssemblyReport struct {
	SequenceName         string             `json:"sequence_name"`
	Shots                []ShotReport       `json:"shots"`
	Transitions          []TransitionReport `json:"transitions,omitempty"`
	TextOverlays         int                `json:"text_overlays"`
	Captions             CaptionsReport     `json:"captions"`
	Music                string             `json:"music,omitempty"`
	Warnings             []string           `json:"warnings,omitempty"`
	TotalDurationSeconds float64            `json:"total_duration_seconds"`
	// Summary is written for non-editors: no track indices, no raw seconds.
	Summary string `json:"summary"`
}

// projectItems fetches the root-level project items in the shape the
// storyboard resolver wants.
func (e *Engine) projectItems(ctx context.Context) ([]storyboard.Item, error) {
	// binPath must be named explicitly: __invoke spreads named args onto
	// positional params, and with an empty object it falls back to passing
	// the raw JSON string as binPath ("Bin not found: {}").
	result, err := e.premiere.EvalCommand(ctx, "getProjectItems", `{"binPath":""}`)
	if err != nil {
		return nil, fmt.Errorf("list project items: %w", err)
	}
	var out struct {
		Items []struct {
			Index     int    `json:"index"`
			Name      string `json:"name"`
			Type      string `json:"type"`
			MediaPath string `json:"media_path"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		return nil, fmt.Errorf("parse project items: %w", err)
	}
	items := make([]storyboard.Item, 0, len(out.Items))
	for _, it := range out.Items {
		if it.Type == "bin" {
			continue
		}
		items = append(items, storyboard.Item{Index: it.Index, Name: it.Name, MediaPath: it.MediaPath})
	}
	return items, nil
}

// resolveWithImports resolves the storyboard's clips, importing any
// unresolved references that are actual files on disk, then re-resolving.
func (e *Engine) resolveWithImports(ctx context.Context, sb *storyboard.Storyboard) (storyboard.Resolution, []storyboard.Item, []string, error) {
	var warnings []string
	items, err := e.projectItems(ctx)
	if err != nil {
		return storyboard.Resolution{}, nil, nil, err
	}
	res := storyboard.ResolveAll(sb, items)

	var toImport []string
	for _, p := range res.UnresolvedPaths() {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			toImport = append(toImport, p)
		}
	}
	if len(toImport) > 0 {
		argsJSON, _ := json.Marshal(map[string]any{"filePaths": toImport})
		if _, err := e.premiere.EvalCommand(ctx, "importFiles", string(argsJSON)); err != nil {
			warnings = append(warnings, fmt.Sprintf("Could not import %d referenced file(s): %v", len(toImport), err))
		} else {
			items, err = e.projectItems(ctx)
			if err != nil {
				return storyboard.Resolution{}, nil, nil, err
			}
			res = storyboard.ResolveAll(sb, items)
		}
	}
	return res, items, warnings, nil
}

// ValidateStoryboard is the dry run: schema problems + per-clip resolution,
// without touching the timeline. The agent shows this to the user before
// assembling.
func (e *Engine) ValidateStoryboard(ctx context.Context, sb *storyboard.Storyboard) (map[string]any, error) {
	sb.Normalize()
	problems := sb.Validate()

	res, _, warnings, err := e.resolveWithImports(ctx, sb)
	if err != nil {
		return nil, err
	}

	type clipCheck struct {
		Shot string                  `json:"shot"`
		Clip string                  `json:"clip"`
		R    storyboard.ResolvedClip `json:"resolution"`
	}
	var checks []clipCheck
	unresolved := 0
	for _, shot := range sb.Shots() {
		if shot.Clip == "" {
			continue
		}
		rc := res.Clips[shot.Clip]
		if !rc.Found {
			unresolved++
		}
		checks = append(checks, clipCheck{Shot: shot.ID, Clip: shot.Clip, R: rc})
	}

	return map[string]any{
		"valid":            len(problems) == 0 && unresolved == 0,
		"schema_problems":  problems,
		"clips":            checks,
		"unresolved_clips": unresolved,
		"warnings":         warnings,
	}, nil
}

// AssembleStoryboard compiles and executes a storyboard against the live
// project. Fail-honest: shots that cannot be placed are reported, not
// papered over.
func (e *Engine) AssembleStoryboard(ctx context.Context, sb *storyboard.Storyboard, opts AssembleStoryboardOptions) (*AssemblyReport, error) {
	sb.Normalize()
	if problems := sb.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("the storyboard has problems: %s", strings.Join(problems, " "))
	}

	res, _, warnings, err := e.resolveWithImports(ctx, sb)
	if err != nil {
		return nil, err
	}

	plan, err := storyboard.Compile(sb, res)
	if err != nil {
		return nil, err
	}
	if opts.SequenceName != "" {
		plan.Sequence.Name = opts.SequenceName
	}

	report := &AssemblyReport{SequenceName: plan.Sequence.Name}
	report.Warnings = append(report.Warnings, warnings...)
	report.Warnings = append(report.Warnings, plan.Warnings...)

	// --- Sequence ---
	// Build the sequence FROM the first placeable clip. createNewSequence
	// with an empty preset pops a modal "New Sequence" dialog on the hub
	// that blocks the whole ExtendScript engine; createSequenceFromClips
	// auto-detects settings from the clip with no dialog. Then clear the
	// auto-placed clip so the placement loop below runs uniformly.
	firstIdx := -1
	for _, sp := range plan.Shots {
		if !sp.Skipped && !sp.TextOnly {
			firstIdx = sp.Clip.Index
			break
		}
	}
	if firstIdx < 0 {
		return nil, fmt.Errorf("no clip-backed shot to seed the sequence from")
	}
	seqArgs, _ := json.Marshal(map[string]any{
		"name":        plan.Sequence.Name,
		"clipIndices": []int{firstIdx},
	})
	if _, err := e.premiere.EvalCommand(ctx, "createSequenceFromClips", string(seqArgs)); err != nil {
		return nil, fmt.Errorf("could not create sequence %q: %w", plan.Sequence.Name, err)
	}
	if err := e.clearActiveSequenceTracks(ctx); err != nil {
		report.Warnings = append(report.Warnings, "could not clear the seed clip: "+err.Error())
	}

	// --- Shots ---
	type pendingTransition struct {
		afterShot string
		clipIndex int
		name      string
		duration  float64
	}
	type pendingText struct {
		overlay storyboard.TextOverlay
		start   float64
		shotDur float64
		track   int
	}
	type captionCue struct {
		start, end float64
		text       string
	}
	var (
		transitions []pendingTransition
		texts       []pendingText
		cues        []captionCue
	)

	pos := 0.0
	trackClipCount := 0 // clips placed on video track 0 so far

	failShot := func(sr ShotReport, reason string) error {
		sr.Status = "failed"
		sr.Detail = reason
		report.Shots = append(report.Shots, sr)
		if opts.StopOnError {
			return fmt.Errorf("shot %s failed: %s", sr.ShotID, reason)
		}
		return nil
	}

	for _, sp := range plan.Shots {
		sr := ShotReport{ShotID: sp.ShotID, Clip: sp.Clip.Name, StartSeconds: pos, VONote: sp.VONote}

		if sp.Skipped {
			sr.Status = "skipped_unresolved"
			sr.Detail = sp.SkipReason
			report.Shots = append(report.Shots, sr)
			if opts.StopOnError {
				return report, fmt.Errorf("shot %s could not be placed: %s", sp.ShotID, sp.SkipReason)
			}
			continue
		}

		if sp.TextOnly {
			// A text-only card (e.g. an intro title) reserves time on the
			// timeline but places its baked PNG on video track 1 (over black),
			// NOT track 0. It must NOT increment trackClipCount: text overlays
			// are added in a deferred phase after this loop, so counting a card
			// here would push the positional track-0 index used to read
			// back/trim/transition every following real clip off by one —
			// making the next real clip fail verification and get clobbered.
			for _, txt := range sp.Text {
				texts = append(texts, pendingText{overlay: txt, start: pos, shotDur: sp.TargetDuration, track: 1})
			}
			if sp.Caption != "" {
				cues = append(cues, captionCue{start: pos, end: pos + sp.TargetDuration, text: sp.Caption})
			}
			sr.Status = "text_card"
			sr.DurationSeconds = sp.TargetDuration
			report.Shots = append(report.Shots, sr)
			pos += sp.TargetDuration
			continue
		}

		placeIndex := sp.Clip.Index

		// Trim via subclip-before-insert: the source range is honored at
		// insert time, no post-insert in-point mutation.
		if sp.Subclip != nil {
			subName := fmt.Sprintf("[SB] %s %s", sp.ShotID, sp.Clip.Name)
			argsJSON, _ := json.Marshal(map[string]any{
				"projectItemIndex": sp.Clip.Index,
				"name":             subName,
				"inPoint":          sp.Subclip.FromSeconds,
				"outPoint":         sp.Subclip.ToSeconds,
			})
			if _, err := e.premiere.EvalCommand(ctx, "createSubclip", string(argsJSON)); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("Shot %s: could not trim %q (%v) — using the whole clip.", sp.ShotID, sp.Clip.Name, err))
			} else {
				// Indices shift after creating an item: re-fetch and find the
				// subclip by its exact name. Never reuse cached indices.
				items, err := e.projectItems(ctx)
				if err != nil {
					return report, err
				}
				found := false
				for _, it := range items {
					if it.Name == subName {
						placeIndex = it.Index
						found = true
						break
					}
				}
				if !found {
					report.Warnings = append(report.Warnings, fmt.Sprintf("Shot %s: subclip %q not found after creation — using the whole clip.", sp.ShotID, subName))
				}
			}
		}

		argsJSON, _ := json.Marshal(map[string]any{
			"projectItemIndex": placeIndex,
			"time":             pos,
			"vTrackIndex":      0,
			"aTrackIndex":      0,
		})
		if _, err := e.premiere.EvalCommand(ctx, "overwriteClip", string(argsJSON)); err != nil {
			if stopErr := failShot(sr, fmt.Sprintf("could not place clip: %v", err)); stopErr != nil {
				return report, stopErr
			}
			continue
		}

		// Read back what actually landed — advance by real duration, not
		// assumption.
		placed, err := e.clipOnTrack(ctx, "video", 0, trackClipCount)
		if err != nil || placed == nil {
			detail := "clip not found on the timeline after placement"
			if err != nil {
				detail = err.Error()
			}
			sr.Status = "failed_verification"
			sr.Detail = detail
			report.Shots = append(report.Shots, sr)
			if opts.StopOnError {
				return report, fmt.Errorf("shot %s: %s", sp.ShotID, detail)
			}
			continue
		}
		actual := placed.End - placed.Start

		if sp.TargetDuration > 0 && actual > sp.TargetDuration+0.01 {
			trimArgs, _ := json.Marshal(map[string]any{
				"trackType":  "video",
				"trackIndex": 0,
				"clipIndex":  trackClipCount,
				"newEndTime": pos + sp.TargetDuration,
			})
			if _, err := e.premiere.EvalCommand(ctx, "trimClipEnd", string(trimArgs)); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("Shot %s: placed at full length; trimming to %.1fs failed: %v", sp.ShotID, sp.TargetDuration, err))
			} else {
				actual = sp.TargetDuration
			}
		} else if sp.TargetDuration > actual+0.01 {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Shot %s: %q is only %.1fs long (%.1fs requested).", sp.ShotID, sp.Clip.Name, actual, sp.TargetDuration))
		}

		for _, txt := range sp.Text {
			texts = append(texts, pendingText{overlay: txt, start: pos, shotDur: actual, track: 1})
		}
		if sp.Caption != "" {
			cues = append(cues, captionCue{start: pos, end: pos + actual, text: sp.Caption})
		}
		if sp.TransitionAfter != nil {
			transitions = append(transitions, pendingTransition{
				afterShot: sp.ShotID,
				clipIndex: trackClipCount,
				name:      sp.TransitionAfter.Name,
				duration:  sp.TransitionAfter.DurationSeconds,
			})
		}

		sr.Status = "placed"
		sr.DurationSeconds = actual
		report.Shots = append(report.Shots, sr)
		trackClipCount++
		pos += actual
	}
	report.TotalDurationSeconds = pos

	// --- Transitions (applied-or-reported, never silent) ---
	for _, tr := range transitions {
		argsJSON, _ := json.Marshal(map[string]any{
			"trackIndex":     0,
			"clipIndex":      tr.clipIndex,
			"transitionName": tr.name,
			"duration":       tr.duration,
		})
		trep := TransitionReport{AfterShot: tr.afterShot, Name: tr.name}
		result, err := e.premiere.EvalCommand(ctx, "addTransition", string(argsJSON))
		if err != nil {
			trep.Detail = err.Error()
		} else {
			var out struct {
				Applied *bool  `json:"applied"`
				Method  string `json:"method"`
			}
			if json.Unmarshal([]byte(result), &out) == nil && out.Applied != nil {
				trep.Applied = *out.Applied
				trep.Detail = out.Method
			} else {
				// Host predates the applied flag; the call not erroring is
				// the only signal we have.
				trep.Applied = true
				trep.Detail = "assumed applied (no applied flag in host response)"
			}
		}
		if !trep.Applied {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Transition after shot %s (%s) was NOT applied%s.", tr.afterShot, tr.name, optionalDetail(trep.Detail)))
		}
		report.Transitions = append(report.Transitions, trep)
	}

	// --- Text overlays (baked-PNG, the only text that renders on 2026) ---
	for _, pt := range texts {
		params := textLayerParamsForStyle(pt.overlay, pt.start, pt.shotDur, pt.track, plan.Sequence)
		if _, err := e.AddTextLayer(ctx, params); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("Text %q could not be added: %v", storyboardTruncate(pt.overlay.Content, 40), err))
			continue
		}
		report.TextOverlays++
	}

	// --- Captions -> generated SRT -> native caption track ---
	if len(cues) > 0 {
		report.Captions.Count = len(cues)
		srt := buildSRT(func(add func(start, end float64, text string)) {
			for _, c := range cues {
				add(c.start, c.end, c.text)
			}
		})
		srtPath := filepath.Join(e.generatedMediaDir(ctx), fmt.Sprintf("storyboard_captions_%d.srt", time.Now().UnixNano()))
		if err := os.WriteFile(srtPath, []byte(srt), 0o644); err != nil {
			report.Captions.Detail = fmt.Sprintf("could not write SRT: %v", err)
		} else {
			report.Captions.SRTPath = srtPath
			argsJSON, _ := json.Marshal(map[string]any{"srtPath": srtPath, "startTime": 0, "format": "Subtitle"})
			if _, err := e.premiere.EvalCommand(ctx, "addSubtitlesFromSRT", string(argsJSON)); err != nil {
				report.Captions.Detail = fmt.Sprintf("caption track failed: %v", err)
			} else {
				report.Captions.Applied = true
			}
		}
		if !report.Captions.Applied {
			report.Warnings = append(report.Warnings, "Captions were NOT added: "+report.Captions.Detail)
		}
	}

	// --- Music bed ---
	if plan.Music != nil && plan.Music.Clip != "" {
		rc := res.Clips[plan.Music.Clip]
		if rc.Found {
			argsJSON, _ := json.Marshal(map[string]any{
				"projectItemIndex": rc.Index,
				"time":             0,
				"vTrackIndex":      0,
				"aTrackIndex":      1,
			})
			if _, err := e.premiere.EvalCommand(ctx, "overwriteClip", string(argsJSON)); err != nil {
				report.Warnings = append(report.Warnings, fmt.Sprintf("Music %q could not be placed: %v", rc.Name, err))
			} else {
				report.Music = rc.Name
				report.Warnings = append(report.Warnings, "Music level/ducking was not adjusted — use premiere_duck_music_under_dialogue or premiere_set_audio_track_volume.")
			}
		}
	}

	report.Summary = summarizeAssembly(report)
	e.logger.Info("storyboard assembled",
		zap.String("sequence", report.SequenceName),
		zap.Int("shots", len(report.Shots)),
		zap.Float64("duration_s", report.TotalDurationSeconds),
		zap.Int("warnings", len(report.Warnings)),
	)
	return report, nil
}

// clearActiveSequenceTracks removes every clip from video track 0 and audio
// track 0 (removing index 0 repeatedly, since indices shift down). Used to
// empty the seed clip left by createSequenceFromClips before the placement
// loop runs.
func (e *Engine) clearActiveSequenceTracks(ctx context.Context) error {
	for _, tt := range []string{"video", "audio"} {
		for guard := 0; guard < 200; guard++ {
			clip, err := e.clipOnTrack(ctx, tt, 0, 0)
			if err != nil {
				return err
			}
			if clip == nil {
				break
			}
			args, _ := json.Marshal(map[string]any{
				"trackType": tt, "trackIndex": 0, "clipIndex": 0, "ripple": false,
			})
			if _, err := e.premiere.EvalCommand(ctx, "removeClipFromTrack", string(args)); err != nil {
				return err
			}
		}
	}
	return nil
}

// clipInfo mirrors _buildClipInfo (keys normalized to snake_case).
type clipInfo struct {
	Index    int     `json:"index"`
	Name     string  `json:"name"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Duration float64 `json:"duration"`
}

// clipOnTrack returns the clip at the given index on a track, or nil.
func (e *Engine) clipOnTrack(ctx context.Context, trackType string, trackIndex, clipIndex int) (*clipInfo, error) {
	argsJSON, _ := json.Marshal(map[string]any{"trackType": trackType, "trackIndex": trackIndex})
	result, err := e.premiere.EvalCommand(ctx, "getClipsOnTrack", string(argsJSON))
	if err != nil {
		return nil, err
	}
	var out struct {
		Clips []clipInfo `json:"clips"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		return nil, fmt.Errorf("parse track clips: %w", err)
	}
	for i := range out.Clips {
		if out.Clips[i].Index == clipIndex {
			return &out.Clips[i], nil
		}
	}
	return nil, nil
}

// textLayerParamsForStyle maps storyboard text styles onto baked-PNG layer
// presets.
func textLayerParamsForStyle(t storyboard.TextOverlay, shotStart, shotDur float64, track int, seq storyboard.SequenceSpec) *TextLayerParams {
	p := &TextLayerParams{
		Text:       t.Content,
		TrackIndex: track,
		StartTime:  shotStart + t.StartOffsetSeconds,
		Duration:   t.DurationSeconds,
	}
	if p.Duration <= 0 {
		p.Duration = shotDur - t.StartOffsetSeconds
		if p.Duration <= 0 {
			p.Duration = 4
		}
	}
	if seq.Width > 0 && seq.Height > 0 {
		p.Width, p.Height = seq.Width, seq.Height
	}
	switch t.Style {
	case "lower_third":
		p.FontSize = 52
		p.Y = 0.85
	case "caption_card":
		p.FontSize = 72
		p.Y = 0.5
	default: // "title"
		p.FontSize = 110
		p.Y = 0.45
	}
	p.X = 0.5
	return p
}

// buildSRT renders cues into SRT text.
func buildSRT(each func(add func(start, end float64, text string))) string {
	var b strings.Builder
	n := 0
	fmtTime := func(s float64) string {
		if s < 0 {
			s = 0
		}
		ms := int(s*1000 + 0.5)
		return fmt.Sprintf("%02d:%02d:%02d,%03d", ms/3600000, (ms/60000)%60, (ms/1000)%60, ms%1000)
	}
	each(func(start, end float64, text string) {
		n++
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", n, fmtTime(start), fmtTime(end), strings.TrimSpace(text))
	})
	return b.String()
}

// summarizeAssembly writes the non-editor summary paragraph.
func summarizeAssembly(r *AssemblyReport) string {
	placed, skipped, failed := 0, 0, 0
	for _, s := range r.Shots {
		switch s.Status {
		case "placed", "text_card":
			placed++
		case "skipped_unresolved":
			skipped++
		default:
			failed++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Built %q: %d of %d shots placed, running %s.",
		r.SequenceName, placed, len(r.Shots), mmss(r.TotalDurationSeconds))
	if skipped > 0 {
		fmt.Fprintf(&b, " %d shot(s) were skipped because their clips could not be found.", skipped)
	}
	if failed > 0 {
		fmt.Fprintf(&b, " %d shot(s) failed to place.", failed)
	}
	applied := 0
	for _, t := range r.Transitions {
		if t.Applied {
			applied++
		}
	}
	if len(r.Transitions) > 0 {
		fmt.Fprintf(&b, " Transitions: %d of %d applied.", applied, len(r.Transitions))
	}
	if r.Captions.Count > 0 {
		if r.Captions.Applied {
			fmt.Fprintf(&b, " Added %d captions.", r.Captions.Count)
		} else {
			fmt.Fprintf(&b, " Captions could not be added.")
		}
	}
	if r.TextOverlays > 0 {
		fmt.Fprintf(&b, " %d on-screen text element(s).", r.TextOverlays)
	}
	if r.Music != "" {
		fmt.Fprintf(&b, " Music: %s.", r.Music)
	}
	return b.String()
}

func mmss(seconds float64) string {
	total := int(seconds + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func optionalDetail(d string) string {
	if d == "" {
		return ""
	}
	return " (" + d + ")"
}

func storyboardTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// assemblyToEDLExecution adapts an AssemblyReport onto the legacy
// EDLExecutionResult shape auto_edit's result type exposes.
func assemblyToEDLExecution(r *AssemblyReport) *EDLExecutionResult {
	out := &EDLExecutionResult{SequenceID: r.SequenceName, Status: "completed", Warnings: r.Warnings}
	for _, s := range r.Shots {
		switch s.Status {
		case "placed", "text_card":
			out.ClipsPlaced++
		case "skipped_unresolved", "failed", "failed_verification":
			out.Errors = append(out.Errors, fmt.Sprintf("shot %s: %s", s.ShotID, s.Detail))
		}
	}
	for _, t := range r.Transitions {
		if t.Applied {
			out.TransitionsAdded++
		}
	}
	if out.ClipsPlaced == 0 {
		out.Status = "failed"
	} else if len(out.Errors) > 0 {
		out.Status = "partial"
	}
	return out
}

// StoryboardFromScript runs the script-understanding pipeline (parse via
// python-intelligence, optionally scan + match assets) and converts the
// result into a storyboard.
func (e *Engine) StoryboardFromScript(ctx context.Context, scriptText, scriptPath, scriptFormat, assetsDirectory string) (*storyboard.Storyboard, []string, error) {
	parsed, err := e.intel.ParseScript(ctx, scriptText, scriptPath, scriptFormat)
	if err != nil {
		return nil, nil, fmt.Errorf("could not parse the script: %w", err)
	}

	var (
		assets  []*AssetInfo
		matches *MatchResult
	)
	if assetsDirectory != "" {
		scan, err := e.media.ScanAssets(ctx, assetsDirectory, true, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("could not scan assets in %q: %w", assetsDirectory, err)
		}
		assets = scan.Assets
		if len(assets) > 0 {
			matches, err = e.intel.MatchAssets(ctx, parsed.Segments, assets, "")
			if err != nil {
				return nil, nil, fmt.Errorf("could not match clips to the script: %w", err)
			}
		}
	}

	sb, warnings := StoryboardFromParsedScript(parsed, matches, assets)
	return sb, warnings, nil
}

// ---------------------------------------------------------------------------
// Converters into the canonical storyboard
// ---------------------------------------------------------------------------

// StoryboardFromParsedScript converts python-intelligence ParseScript +
// MatchAssets output into a storyboard.
func StoryboardFromParsedScript(script *ParsedScript, matches *MatchResult, assets []*AssetInfo) (*storyboard.Storyboard, []string) {
	var warnings []string
	assetPath := map[string]string{}
	for _, a := range assets {
		assetPath[a.ID] = a.FilePath
	}
	matchBySegment := map[uint32]*AssetMatch{}
	if matches != nil {
		for _, m := range matches.Matches {
			matchBySegment[m.SegmentIndex] = m
		}
	}

	sb := &storyboard.Storyboard{Version: storyboard.Version}
	if script.Metadata != nil {
		sb.Title = script.Metadata.Title
	}
	scene := storyboard.Scene{Name: "Script"}

	var prevShot *storyboard.Shot
	appendShot := func(s storyboard.Shot) {
		scene.Shots = append(scene.Shots, s)
		prevShot = &scene.Shots[len(scene.Shots)-1]
	}

	for _, seg := range script.Segments {
		switch seg.Type {
		case SegmentTypeDialogue, SegmentTypeAction, SegmentTypeBRoll:
			shot := storyboard.Shot{
				DurationSeconds: seg.EstimatedDurationSeconds,
				Notes:           seg.VisualDirection,
			}
			if seg.Type == SegmentTypeDialogue {
				shot.Caption = seg.Content
			}
			if m := matchBySegment[seg.Index]; m != nil {
				if p, ok := assetPath[m.AssetID]; ok {
					shot.Clip = p
				}
				if m.SuggestedRange != nil {
					from := timecodeSeconds(m.SuggestedRange.InPoint)
					to := timecodeSeconds(m.SuggestedRange.OutPoint)
					if to > from {
						shot.Trim = &storyboard.TrimHint{FromSeconds: from, ToSeconds: to}
					}
				}
			}
			if shot.Clip == "" {
				warnings = append(warnings, fmt.Sprintf("Script segment %d (%s) has no matched footage — it will be skipped.", seg.Index, storyboardTruncate(seg.Content, 50)))
			}
			appendShot(shot)
		case SegmentTypeTitle, SegmentTypeLowerThird:
			style := "title"
			if seg.Type == SegmentTypeLowerThird {
				style = "lower_third"
			}
			dur := seg.EstimatedDurationSeconds
			if dur <= 0 {
				dur = 4
			}
			appendShot(storyboard.Shot{
				DurationSeconds: dur,
				Text:            []storyboard.TextOverlay{{Content: seg.Content, Style: style}},
			})
		case SegmentTypeVoiceover:
			if prevShot != nil && prevShot.Caption == "" {
				prevShot.Caption = seg.Content
				prevShot.VONote = "voiceover"
			} else {
				warnings = append(warnings, fmt.Sprintf("Voiceover segment %d noted: %s", seg.Index, storyboardTruncate(seg.Content, 60)))
			}
		case SegmentTypeMusic:
			if sb.Music == nil {
				sb.Music = &storyboard.MusicSpec{Clip: firstNonEmpty(seg.AssetHints), Note: seg.Content}
				if sb.Music.Clip == "" {
					warnings = append(warnings, "The script asks for music but names no track — attach or name a music file.")
					sb.Music = nil
				}
			}
		case SegmentTypeTransition:
			if prevShot != nil {
				prevShot.TransitionAfter = &storyboard.Transition{Name: "Cross Dissolve", DurationSeconds: 1}
			}
		default:
			// SFX and unspecified segments are informational for now.
		}
	}

	sb.Scenes = []storyboard.Scene{scene}
	sb.Normalize()
	return sb, warnings
}

// StoryboardFromEDL converts a python-generated EDL into a storyboard so
// auto_edit's assemble step runs through the same executor as everything
// else.
func StoryboardFromEDL(edl *EDL, assets []*AssetInfo) *storyboard.Storyboard {
	assetPath := map[string]string{}
	for _, a := range assets {
		assetPath[a.ID] = a.FilePath
	}

	sb := &storyboard.Storyboard{Version: storyboard.Version, Title: edl.Name}
	if edl.SequenceResolution.Width > 0 {
		sb.Sequence = &storyboard.SequenceSpec{
			Name:   edl.Name,
			Width:  int(edl.SequenceResolution.Width),
			Height: int(edl.SequenceResolution.Height),
			FPS:    edl.SequenceFrameRate,
		}
	}
	scene := storyboard.Scene{Name: "EDL"}
	for _, entry := range edl.Entries {
		clip := assetPath[entry.SourceAssetID]
		if clip == "" {
			clip = entry.SourceAssetID
		}
		shot := storyboard.Shot{Clip: clip, Notes: entry.Notes}
		if entry.SourceRange != nil {
			from := timecodeSeconds(entry.SourceRange.InPoint)
			to := timecodeSeconds(entry.SourceRange.OutPoint)
			if to > from {
				shot.Trim = &storyboard.TrimHint{FromSeconds: from, ToSeconds: to}
			}
		}
		if entry.TimelineRange != nil {
			d := timecodeSeconds(entry.TimelineRange.OutPoint) - timecodeSeconds(entry.TimelineRange.InPoint)
			if d > 0 {
				shot.DurationSeconds = d
			}
		}
		if entry.Transition != nil && entry.Transition.Type != "" && entry.Transition.Type != "cut" {
			shot.TransitionAfter = &storyboard.Transition{
				Name:            edlTransitionName(entry.Transition.Type),
				DurationSeconds: entry.Transition.DurationSeconds,
			}
		}
		scene.Shots = append(scene.Shots, shot)
	}
	sb.Scenes = []storyboard.Scene{scene}
	sb.Normalize()
	return sb
}

func edlTransitionName(t string) string {
	switch strings.ToLower(t) {
	case "cross_dissolve", "dissolve", "crossfade":
		return "Cross Dissolve"
	case "dip_to_black":
		return "Dip to Black"
	case "dip_to_white":
		return "Dip to White"
	default:
		return t
	}
}

func timecodeSeconds(tc Timecode) float64 {
	s := float64(tc.Hours)*3600 + float64(tc.Minutes)*60 + float64(tc.Seconds)
	if tc.FrameRate > 0 {
		s += float64(tc.Frames) / tc.FrameRate
	}
	return s
}

func firstNonEmpty(ss []string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
