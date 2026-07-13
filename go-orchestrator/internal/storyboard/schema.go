// Package storyboard defines the canonical storyboard format — the contract
// between what a non-editor provides (a script, a simple shot list, or JSON
// emitted by an LLM) and what gets assembled onto the Premiere timeline.
// Everything here is pure data + logic: no bridge calls, unit-testable
// without Premiere.
package storyboard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Version is the schema version accepted by this package.
const Version = "storyboard/v1"

//go:embed schema.json
var schemaJSON string

// SchemaJSON returns the JSON Schema document describing the storyboard
// format, for LLMs that want to emit it directly.
func SchemaJSON() string { return schemaJSON }

// Storyboard is an ordered plan of shots to assemble into a sequence.
type Storyboard struct {
	Version  string        `json:"version,omitempty"`
	Title    string        `json:"title,omitempty"`
	Sequence *SequenceSpec `json:"sequence,omitempty"`
	Scenes   []Scene       `json:"scenes"`
	Music    *MusicSpec    `json:"music,omitempty"`
}

// SequenceSpec optionally pins the target sequence. All fields optional;
// unset dimensions inherit from the first clip.
type SequenceSpec struct {
	Name   string  `json:"name,omitempty"`
	Width  int     `json:"width,omitempty"`
	Height int     `json:"height,omitempty"`
	FPS    float64 `json:"fps,omitempty"`
}

// Scene groups shots. Purely organizational; assembly is scene order then
// shot order.
type Scene struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Shots []Shot `json:"shots"`
}

// Shot is one clip placement (or a text-only card when Clip is empty).
type Shot struct {
	ID string `json:"id,omitempty"` // auto-assigned "s1sh2" when empty

	// Clip references a project item or media file by NAME (or file path) —
	// never by index. Empty is allowed only for text-only shots.
	Clip string `json:"clip,omitempty"`

	// DurationSeconds is the target on-timeline duration. 0 = natural length
	// (full clip, or trim length when Trim is set).
	DurationSeconds float64 `json:"duration_seconds,omitempty"`

	// Trim selects a source range (seconds in the source clip).
	Trim *TrimHint `json:"trim,omitempty"`

	Text            []TextOverlay `json:"text,omitempty"`
	Caption         string        `json:"caption,omitempty"` // subtitle line for this shot -> SRT
	TransitionAfter *Transition   `json:"transition_after,omitempty"`
	VONote          string        `json:"vo_note,omitempty"` // informational, echoed in the report
	Notes           string        `json:"notes,omitempty"`
}

// TrimHint is a source-time range.
type TrimHint struct {
	FromSeconds float64 `json:"from_seconds"`
	ToSeconds   float64 `json:"to_seconds"`
}

// TextOverlay is on-screen text rendered via the baked-PNG text layer (the
// only text that renders on Premiere 2026).
type TextOverlay struct {
	Content            string  `json:"content"`
	Style              string  `json:"style,omitempty"` // "title" | "lower_third" | "caption_card"
	StartOffsetSeconds float64 `json:"start_offset_seconds,omitempty"`
	DurationSeconds    float64 `json:"duration_seconds,omitempty"` // 0 = shot duration
}

// Transition after a shot.
type Transition struct {
	Name            string  `json:"name,omitempty"` // default "Cross Dissolve"
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
}

// MusicSpec is an optional background music bed.
type MusicSpec struct {
	Clip    string  `json:"clip"`
	LevelDB float64 `json:"level_db,omitempty"`
	Note    string  `json:"note,omitempty"`
}

// Parse decodes and normalizes a storyboard JSON document.
func Parse(data []byte) (*Storyboard, error) {
	var sb Storyboard
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sb); err != nil {
		// Re-decode leniently to distinguish "malformed" from "unknown field"
		// and give the LLM a precise complaint either way.
		var lenient Storyboard
		if lerr := json.Unmarshal(data, &lenient); lerr != nil {
			return nil, fmt.Errorf("storyboard JSON is malformed: %w", lerr)
		}
		return nil, fmt.Errorf("storyboard JSON has unexpected fields (check against the schema from premiere_storyboard_schema): %w", err)
	}
	sb.Normalize()
	return &sb, nil
}

// Normalize assigns missing shot/scene IDs and defaults.
func (sb *Storyboard) Normalize() {
	if sb.Version == "" {
		sb.Version = Version
	}
	for si := range sb.Scenes {
		scene := &sb.Scenes[si]
		if scene.ID == "" {
			scene.ID = fmt.Sprintf("s%d", si+1)
		}
		for hi := range scene.Shots {
			shot := &scene.Shots[hi]
			if shot.ID == "" {
				shot.ID = fmt.Sprintf("%ssh%d", scene.ID, hi+1)
			}
			if shot.TransitionAfter != nil {
				if shot.TransitionAfter.Name == "" {
					shot.TransitionAfter.Name = "Cross Dissolve"
				}
				if shot.TransitionAfter.DurationSeconds <= 0 {
					shot.TransitionAfter.DurationSeconds = 1.0
				}
			}
		}
	}
}

// Validate returns human-readable problems (empty = valid). Messages are
// written for non-editors: they name scenes/shots and say what to add.
func (sb *Storyboard) Validate() []string {
	var problems []string
	if sb.Version != "" && sb.Version != Version {
		problems = append(problems, fmt.Sprintf("Unknown storyboard version %q — this server understands %q.", sb.Version, Version))
	}
	if len(sb.Scenes) == 0 {
		problems = append(problems, "The storyboard has no scenes — add at least one scene with one shot.")
		return problems
	}
	totalShots := 0
	for si, scene := range sb.Scenes {
		sceneName := scene.Name
		if sceneName == "" {
			sceneName = fmt.Sprintf("scene %d", si+1)
		}
		if len(scene.Shots) == 0 {
			problems = append(problems, fmt.Sprintf("%s has no shots.", capitalize(sceneName)))
		}
		for hi, shot := range scene.Shots {
			totalShots++
			label := fmt.Sprintf("Shot %d in %s", hi+1, sceneName)
			if shot.Clip == "" && len(shot.Text) == 0 {
				problems = append(problems, fmt.Sprintf("%s names no clip and has no text — every shot needs a clip or on-screen text.", label))
			}
			if shot.Trim != nil && shot.Trim.ToSeconds <= shot.Trim.FromSeconds {
				problems = append(problems, fmt.Sprintf("%s has a trim that ends (%.1fs) at or before it starts (%.1fs).", label, shot.Trim.ToSeconds, shot.Trim.FromSeconds))
			}
			if shot.DurationSeconds < 0 {
				problems = append(problems, fmt.Sprintf("%s has a negative duration.", label))
			}
			if shot.Clip == "" && len(shot.Text) > 0 && shot.DurationSeconds <= 0 {
				problems = append(problems, fmt.Sprintf("%s is a text-only card but gives no duration — add duration_seconds so we know how long to show it.", label))
			}
			for ti, txt := range shot.Text {
				if strings.TrimSpace(txt.Content) == "" {
					problems = append(problems, fmt.Sprintf("%s text overlay %d has empty content.", label, ti+1))
				}
			}
		}
	}
	if totalShots == 0 {
		problems = append(problems, "The storyboard has no shots at all.")
	}
	if sb.Music != nil && sb.Music.Clip == "" {
		problems = append(problems, "Music is requested but no music clip/file is named.")
	}
	return problems
}

// Shots returns all shots in assembly order.
func (sb *Storyboard) Shots() []*Shot {
	var out []*Shot
	for si := range sb.Scenes {
		for hi := range sb.Scenes[si].Shots {
			out = append(out, &sb.Scenes[si].Shots[hi])
		}
	}
	return out
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
