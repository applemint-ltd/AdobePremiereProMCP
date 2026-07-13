package storyboard

import "fmt"

// ShotPlan is one shot with its clip resolved and parameters finalized —
// exactly what the executor needs, in order.
type ShotPlan struct {
	ShotID  string       `json:"shot_id"`
	SceneID string       `json:"scene_id"`
	Clip    ResolvedClip `json:"clip"`

	// Subclip, when set, means: create a subclip with this source range and
	// place that (trims are honored at insert time — no post-insert in-point
	// mutation).
	Subclip *TrimHint `json:"subclip,omitempty"`

	// TargetDuration is the desired on-timeline duration; 0 = natural.
	TargetDuration float64 `json:"target_duration,omitempty"`

	Text            []TextOverlay `json:"text,omitempty"`
	Caption         string        `json:"caption,omitempty"`
	TransitionAfter *Transition   `json:"transition_after,omitempty"`
	VONote          string        `json:"vo_note,omitempty"`

	// TextOnly shots place no clip; they render a text card of
	// TargetDuration seconds.
	TextOnly bool `json:"text_only,omitempty"`

	Skipped    bool   `json:"skipped,omitempty"`
	SkipReason string `json:"skip_reason,omitempty"`
}

// Plan is the compiled storyboard: deterministic, serializable, and the
// artifact the audit layer can persist alongside the assembly report.
type Plan struct {
	Title    string       `json:"title,omitempty"`
	Sequence SequenceSpec `json:"sequence"`
	Shots    []ShotPlan   `json:"shots"`
	Music    *MusicSpec   `json:"music,omitempty"`
	Warnings []string     `json:"warnings,omitempty"`
}

// Compile validates the storyboard against a clip resolution and produces
// the ordered shot plan. Pure function: no bridge calls.
func Compile(sb *Storyboard, res Resolution) (*Plan, error) {
	sb.Normalize()
	if problems := sb.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("storyboard is not valid: %s", problems[0])
	}

	plan := &Plan{Title: sb.Title, Music: sb.Music}
	if sb.Sequence != nil {
		plan.Sequence = *sb.Sequence
	}
	if plan.Sequence.Name == "" {
		if sb.Title != "" {
			plan.Sequence.Name = sb.Title
		} else {
			plan.Sequence.Name = "Storyboard Cut"
		}
	}

	for si := range sb.Scenes {
		scene := &sb.Scenes[si]
		for hi := range scene.Shots {
			shot := &scene.Shots[hi]
			sp := ShotPlan{
				ShotID:          shot.ID,
				SceneID:         scene.ID,
				TargetDuration:  shot.DurationSeconds,
				Text:            shot.Text,
				Caption:         shot.Caption,
				TransitionAfter: shot.TransitionAfter,
				VONote:          shot.VONote,
			}

			if shot.Clip == "" {
				sp.TextOnly = true
				if sp.TargetDuration <= 0 {
					sp.TargetDuration = 5
				}
				plan.Shots = append(plan.Shots, sp)
				continue
			}

			rc, ok := res.Clips[shot.Clip]
			if !ok {
				rc = ResolvedClip{Query: shot.Clip, Reason: "clip was never resolved (internal error)"}
			}
			sp.Clip = rc
			if !rc.Found {
				sp.Skipped = true
				sp.SkipReason = rc.Reason
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("Shot %s skipped: %s", shot.ID, rc.Reason))
			}
			if shot.Trim != nil {
				sp.Subclip = shot.Trim
				trimLen := shot.Trim.ToSeconds - shot.Trim.FromSeconds
				if sp.TargetDuration == 0 {
					sp.TargetDuration = trimLen
				} else if sp.TargetDuration > trimLen {
					plan.Warnings = append(plan.Warnings, fmt.Sprintf(
						"Shot %s asks for %.1fs but its trim only covers %.1fs — using the trim length.",
						shot.ID, sp.TargetDuration, trimLen))
					sp.TargetDuration = trimLen
				}
			}
			plan.Shots = append(plan.Shots, sp)
		}
	}

	if plan.Music != nil && plan.Music.Clip != "" {
		if rc, ok := res.Clips[plan.Music.Clip]; ok && !rc.Found {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Music clip could not be found: %s", rc.Reason))
			plan.Music = nil
		}
	}

	placeable := 0
	for _, sp := range plan.Shots {
		if !sp.Skipped {
			placeable++
		}
	}
	if placeable == 0 {
		return nil, fmt.Errorf("no shot in the storyboard could be matched to a clip — check clip names against the project (premiere_get_project_items) or attach the files")
	}
	return plan, nil
}
