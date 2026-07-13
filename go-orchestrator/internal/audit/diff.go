package audit

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// TimelineSnapshot mirrors the snapshotTimeline ExtendScript payload after
// the orchestrator's envelope unwrapping (which normalizes keys to
// snake_case).
type TimelineSnapshot struct {
	SequenceName string  `json:"sequence_name"`
	SequenceID   string  `json:"sequence_id"`
	Timestamp    string  `json:"timestamp"`
	VideoTracks  []Track `json:"video_tracks"`
	AudioTracks  []Track `json:"audio_tracks"`
}

// Track is one video or audio track in a snapshot.
type Track struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Clips []Clip `json:"clips"`
}

// Clip is one timeline clip in a snapshot. Times are seconds.
type Clip struct {
	Index     int     `json:"index"`
	Name      string  `json:"name"`
	Start     float64 `json:"start"`
	End       float64 `json:"end"`
	Duration  float64 `json:"duration"`
	InPoint   float64 `json:"in_point"`
	OutPoint  float64 `json:"out_point"`
	MediaPath string  `json:"media_path"`
	Enabled   bool    `json:"enabled"`
}

// ClipChange describes one added/removed/modified clip.
type ClipChange struct {
	TrackType  string `json:"track_type"` // "video" | "audio"
	TrackIndex int    `json:"track_index"`
	ClipName   string `json:"clip_name"`
	Kind       string `json:"kind"` // "added" | "removed" | "moved" | "trimmed" | "enabled" | "disabled"
	Detail     string `json:"detail,omitempty"`
	Before     *Clip  `json:"before,omitempty"`
	After      *Clip  `json:"after,omitempty"`
}

// TimelineDiff is the result of comparing two snapshots.
type TimelineDiff struct {
	SequenceName string       `json:"sequence_name"`
	Added        []ClipChange `json:"added"`
	Removed      []ClipChange `json:"removed"`
	Modified     []ClipChange `json:"modified"`
}

// Empty reports whether the diff contains no changes.
func (d *TimelineDiff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Modified) == 0
}

const timeEpsilon = 0.01 // seconds; below this, float jitter, not an edit

// Diff compares two timeline snapshot JSON payloads (before, after).
func Diff(beforeJSON, afterJSON string) (*TimelineDiff, error) {
	var before, after TimelineSnapshot
	if err := json.Unmarshal([]byte(beforeJSON), &before); err != nil {
		return nil, fmt.Errorf("parse before snapshot: %w", err)
	}
	if err := json.Unmarshal([]byte(afterJSON), &after); err != nil {
		return nil, fmt.Errorf("parse after snapshot: %w", err)
	}

	d := &TimelineDiff{SequenceName: after.SequenceName}
	if d.SequenceName == "" {
		d.SequenceName = before.SequenceName
	}
	diffTracks(d, "video", before.VideoTracks, after.VideoTracks)
	diffTracks(d, "audio", before.AudioTracks, after.AudioTracks)
	return d, nil
}

func diffTracks(d *TimelineDiff, trackType string, before, after []Track) {
	byIndex := func(ts []Track) map[int]Track {
		m := make(map[int]Track, len(ts))
		for _, t := range ts {
			m[t.Index] = t
		}
		return m
	}
	b, a := byIndex(before), byIndex(after)

	indices := map[int]bool{}
	for i := range b {
		indices[i] = true
	}
	for i := range a {
		indices[i] = true
	}
	ordered := make([]int, 0, len(indices))
	for i := range indices {
		ordered = append(ordered, i)
	}
	sort.Ints(ordered)

	for _, i := range ordered {
		diffClips(d, trackType, i, b[i].Clips, a[i].Clips)
	}
}

// diffClips pairs clips sharing the same identity (name|mediaPath) by
// ascending start time, then classifies leftovers as added/removed and pairs
// as moved/trimmed/enabled changes.
func diffClips(d *TimelineDiff, trackType string, trackIndex int, before, after []Clip) {
	key := func(c Clip) string { return c.Name + "|" + c.MediaPath }

	group := func(cs []Clip) map[string][]Clip {
		m := map[string][]Clip{}
		for _, c := range cs {
			m[key(c)] = append(m[key(c)], c)
		}
		for k := range m {
			sort.Slice(m[k], func(i, j int) bool { return m[k][i].Start < m[k][j].Start })
		}
		return m
	}
	b, a := group(before), group(after)

	keys := map[string]bool{}
	for k := range b {
		keys[k] = true
	}
	for k := range a {
		keys[k] = true
	}
	orderedKeys := make([]string, 0, len(keys))
	for k := range keys {
		orderedKeys = append(orderedKeys, k)
	}
	sort.Strings(orderedKeys)

	for _, k := range orderedKeys {
		bs, as := b[k], a[k]
		n := len(bs)
		if len(as) < n {
			n = len(as)
		}
		for i := 0; i < n; i++ {
			compareClip(d, trackType, trackIndex, bs[i], as[i])
		}
		for _, c := range bs[n:] {
			clip := c
			d.Removed = append(d.Removed, ClipChange{
				TrackType: trackType, TrackIndex: trackIndex, ClipName: displayName(clip),
				Kind: "removed", Before: &clip,
			})
		}
		for _, c := range as[n:] {
			clip := c
			d.Added = append(d.Added, ClipChange{
				TrackType: trackType, TrackIndex: trackIndex, ClipName: displayName(clip),
				Kind: "added", After: &clip,
			})
		}
	}
}

func compareClip(d *TimelineDiff, trackType string, trackIndex int, b, a Clip) {
	bc, ac := b, a
	base := ClipChange{
		TrackType: trackType, TrackIndex: trackIndex, ClipName: displayName(a),
		Before: &bc, After: &ac,
	}

	moved := math.Abs(a.Start-b.Start) > timeEpsilon
	trimmed := math.Abs(a.Duration-b.Duration) > timeEpsilon ||
		math.Abs(a.InPoint-b.InPoint) > timeEpsilon ||
		math.Abs(a.OutPoint-b.OutPoint) > timeEpsilon

	switch {
	case trimmed:
		c := base
		c.Kind = "trimmed"
		c.Detail = fmt.Sprintf("duration %s -> %s", fmtDur(b.Duration), fmtDur(a.Duration))
		d.Modified = append(d.Modified, c)
	case moved:
		c := base
		c.Kind = "moved"
		c.Detail = fmt.Sprintf("start %s -> %s", fmtTime(b.Start), fmtTime(a.Start))
		d.Modified = append(d.Modified, c)
	}

	if a.Enabled != b.Enabled {
		c := base
		if a.Enabled {
			c.Kind = "enabled"
		} else {
			c.Kind = "disabled"
		}
		d.Modified = append(d.Modified, c)
	}
}

// HumanLines renders the diff as plain-language sentences for non-editors:
// clip display names, mm:ss positions, "video layer N" — no raw seconds or
// track jargon.
func (d *TimelineDiff) HumanLines() []string {
	if d.Empty() {
		return []string{"No timeline changes."}
	}
	var lines []string
	layer := func(c ClipChange) string {
		if c.TrackIndex == 0 {
			return ""
		}
		return fmt.Sprintf(" on %s layer %d", c.TrackType, c.TrackIndex+1)
	}
	for _, c := range d.Added {
		lines = append(lines, fmt.Sprintf("Added %q to the timeline at %s%s.", c.ClipName, fmtTime(c.After.Start), layer(c)))
	}
	for _, c := range d.Removed {
		lines = append(lines, fmt.Sprintf("Removed %q from the timeline (was at %s%s).", c.ClipName, fmtTime(c.Before.Start), layer(c)))
	}
	for _, c := range d.Modified {
		switch c.Kind {
		case "moved":
			lines = append(lines, fmt.Sprintf("Moved %q from %s to %s%s.", c.ClipName, fmtTime(c.Before.Start), fmtTime(c.After.Start), layer(c)))
		case "trimmed":
			delta := c.After.Duration - c.Before.Duration
			verb := "Shortened"
			if delta > 0 {
				verb = "Lengthened"
			}
			lines = append(lines, fmt.Sprintf("%s %q by %s (now %s long, at %s%s).", verb, c.ClipName, fmtDur(math.Abs(delta)), fmtDur(c.After.Duration), fmtTime(c.After.Start), layer(c)))
		case "disabled":
			lines = append(lines, fmt.Sprintf("Turned off (disabled) %q at %s%s.", c.ClipName, fmtTime(c.After.Start), layer(c)))
		case "enabled":
			lines = append(lines, fmt.Sprintf("Turned %q back on at %s%s.", c.ClipName, fmtTime(c.After.Start), layer(c)))
		}
	}
	return lines
}

func displayName(c Clip) string {
	if c.Name != "" {
		return c.Name
	}
	if c.MediaPath != "" {
		parts := strings.Split(strings.ReplaceAll(c.MediaPath, "\\", "/"), "/")
		return parts[len(parts)-1]
	}
	return "(unnamed clip)"
}

// fmtTime renders seconds as m:ss (or h:mm:ss past an hour).
func fmtTime(seconds float64) string {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds + 0.5)
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}

// fmtDur renders a duration in seconds compactly ("4.5s", "1:10").
func fmtDur(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.1fs", seconds)
	}
	return fmtTime(seconds)
}
