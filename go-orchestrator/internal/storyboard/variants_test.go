package storyboard

import (
	"encoding/json"
	"strings"
	"testing"
)

func baseStoryboard(t *testing.T) *Storyboard {
	t.Helper()
	sb, err := Parse([]byte(`{
		"version": "storyboard/v1",
		"title": "hook test base",
		"sequence": {"name": "base", "width": 1080, "height": 1920, "fps": 30},
		"scenes": [
			{"name": "Hook", "shots": [
				{"clip": "demo.mp4", "trim": {"from_seconds": 2, "to_seconds": 4.5},
				 "text": [{"content": "Set up in 60 seconds.", "style": "title"}],
				 "caption": "Set up in sixty seconds.",
				 "transition_after": {"name": "Cross Dissolve", "duration_seconds": 0.3}}
			]},
			{"name": "CTA", "shots": [
				{"duration_seconds": 2.5, "text": [{"content": "Link in bio.", "style": "title"}]}
			]}
		],
		"music": {"clip": "upbeat.wav", "level_db": -18}
	}`))
	if err != nil {
		t.Fatalf("base storyboard: %v", err)
	}
	return sb
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestExpandVariants_HappyPath(t *testing.T) {
	got, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "hookA", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"Still doing this manually?"`)}},
			{VariantID: "musicB", Overrides: map[string]json.RawMessage{
				"music.level_db": raw(`-24`)}},
			{VariantID: "trimC", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].trim.to_seconds": raw(`5.5`)}},
		},
		EnforceSingleVariable: true,
	})
	if err != nil {
		t.Fatalf("ExpandVariants: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 variants, got %d", len(got))
	}
	if got[0].SequenceName != "EXP014-hookA" || got[1].SequenceName != "EXP014-musicB" {
		t.Fatalf("sequence names wrong: %q, %q", got[0].SequenceName, got[1].SequenceName)
	}
	if len(got[0].ChangedPaths) != 1 || got[0].ChangedPaths[0] != "scenes[0].shots[0].text[0].content" {
		t.Fatalf("changed paths wrong: %v", got[0].ChangedPaths)
	}
	if got[0].Storyboard.Scenes[0].Shots[0].Text[0].Content != "Still doing this manually?" {
		t.Fatalf("override not applied: %+v", got[0].Storyboard.Scenes[0].Shots[0].Text[0])
	}
	// The base must not have been mutated by expansion.
	if got[0].Storyboard.Sequence.Name == "base" {
		t.Fatalf("variant kept the base sequence name")
	}
	for _, v := range got {
		if problems := v.Storyboard.Validate(); len(problems) > 0 {
			t.Fatalf("variant %s does not validate: %v", v.VariantID, problems)
		}
	}
}

func TestExpandVariants_TwoFieldsListsBoth(t *testing.T) {
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "sloppy", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"New hook"`),
				"scenes[0].shots[0].trim.to_seconds": raw(`6.0`),
			}},
		},
		EnforceSingleVariable: true,
	})
	if err == nil {
		t.Fatalf("two-field variant must fail the gate")
	}
	msg := err.Error()
	if !strings.Contains(msg, "scenes[0].shots[0].text[0].content") || !strings.Contains(msg, "scenes[0].shots[0].trim.to_seconds") {
		t.Fatalf("gate error must name every changed path, got: %s", msg)
	}
}

func TestExpandVariants_NoOpVariantRejected(t *testing.T) {
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			// Restates the base value verbatim: zero actual change.
			{VariantID: "noop", Overrides: map[string]json.RawMessage{
				"music.level_db": raw(`-18`)}},
		},
		EnforceSingleVariable: true,
	})
	if err == nil || !strings.Contains(err.Error(), "identical to the base") {
		t.Fatalf("no-op variant must be rejected, got: %v", err)
	}
}

func TestExpandVariants_UnknownFieldFailsAtExpansion(t *testing.T) {
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "typo", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].clipp": raw(`"demo.mp4"`)}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("unknown field must fail at expansion naming the variant, got: %v", err)
	}
}

func TestExpandVariants_IndexOutOfRangeFails(t *testing.T) {
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "oob", Overrides: map[string]json.RawMessage{
				"scenes[5].shots[0].clip": raw(`"other.mp4"`)}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("out-of-range index must fail, got: %v", err)
	}
}

func TestExpandVariants_NullDeletesField(t *testing.T) {
	got, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "cut", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].transition_after": raw(`null`)}},
		},
		EnforceSingleVariable: true,
	})
	if err != nil {
		t.Fatalf("ExpandVariants: %v", err)
	}
	if got[0].Storyboard.Scenes[0].Shots[0].TransitionAfter != nil {
		t.Fatalf("null override should delete transition_after")
	}
	if len(got[0].ChangedPaths) != 1 || got[0].ChangedPaths[0] != "scenes[0].shots[0].transition_after" {
		t.Fatalf("deletion should be the one reported change, got %v", got[0].ChangedPaths)
	}
}

func TestExpandVariants_NullDeleteMissingParentIsNoOp(t *testing.T) {
	// Deleting a leaf whose parent is absent must not materialize a phantom
	// empty object (which would trip Validate). Base has no music.
	base := baseStoryboard(t)
	base.Music = nil
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         base,
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			// This is a no-op (music absent already), so it changes nothing —
			// with the gate on that's an "identical to base" error, which is
			// the correct signal, NOT a phantom-music validation failure.
			{VariantID: "noop", Overrides: map[string]json.RawMessage{
				"music.level_db": raw(`null`)}},
		},
		EnforceSingleVariable: true,
	})
	if err == nil || !strings.Contains(err.Error(), "identical to the base") {
		t.Fatalf("null-delete of an absent field should be a clean no-op, got: %v", err)
	}
}

func TestExpandVariants_NamingTemplateNonLetterPlaceholder(t *testing.T) {
	// A typo'd placeholder with a hyphen must be rejected, not shipped.
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:           baseStoryboard(t),
		ExperimentID:   "EXP014",
		NamingTemplate: "{experiment-id}-{variant_id}",
		Variants: []VariantSpec{
			{VariantID: "x", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"X"`)}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown placeholder") {
		t.Fatalf("hyphenated placeholder typo must be rejected, got: %v", err)
	}
}

func TestExpandVariants_AdditiveIntermediateMaps(t *testing.T) {
	base := baseStoryboard(t)
	base.Music = nil // base without music
	got, err := ExpandVariants(ExpandVariantsInput{
		Base:         base,
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "addmusic", Overrides: map[string]json.RawMessage{
				"music.clip":     raw(`"upbeat.wav"`),
				"music.level_db": raw(`-18`),
			}},
		},
		EnforceSingleVariable: true,
		AllowedExtraPaths:     []string{"music.level_db"},
	})
	if err != nil {
		t.Fatalf("additive override should work: %v", err)
	}
	m := got[0].Storyboard.Music
	if m == nil || m.Clip != "upbeat.wav" || m.LevelDB != -18 {
		t.Fatalf("music not created: %+v", m)
	}
}

func TestExpandVariants_DuplicateVariantIDRejected(t *testing.T) {
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "a", Overrides: map[string]json.RawMessage{"music.level_db": raw(`-20`)}},
			{VariantID: "a", Overrides: map[string]json.RawMessage{"music.level_db": raw(`-24`)}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate variant_id") {
		t.Fatalf("duplicate variant ids must be rejected, got: %v", err)
	}
}

func TestExpandVariants_NamingTemplate(t *testing.T) {
	got, err := ExpandVariants(ExpandVariantsInput{
		Base:           baseStoryboard(t),
		ExperimentID:   "EXP014",
		NamingTemplate: "{experiment_id}_{variant_id}_v1",
		Variants: []VariantSpec{
			{VariantID: "hookB", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"B"`)}},
		},
		EnforceSingleVariable: true,
	})
	if err != nil {
		t.Fatalf("ExpandVariants: %v", err)
	}
	if got[0].SequenceName != "EXP014_hookB_v1" {
		t.Fatalf("template not honored: %q", got[0].SequenceName)
	}

	_, err = ExpandVariants(ExpandVariantsInput{
		Base:           baseStoryboard(t),
		ExperimentID:   "EXP014",
		NamingTemplate: "{experiment_id}-{varant_id}",
		Variants: []VariantSpec{
			{VariantID: "x", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"X"`)}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown placeholder") {
		t.Fatalf("typo'd placeholder must be rejected, got: %v", err)
	}
}

func TestExpandVariants_SequenceNameExemptFromGate(t *testing.T) {
	got, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "named", Overrides: map[string]json.RawMessage{
				"sequence.name":                      raw(`"clobbered"`),
				"scenes[0].shots[0].text[0].content": raw(`"New hook"`),
			}},
		},
		EnforceSingleVariable: true,
	})
	if err != nil {
		t.Fatalf("sequence.name must be exempt from the gate: %v", err)
	}
	// ... and deterministic stamping wins over the override.
	if got[0].SequenceName != "EXP014-named" || got[0].Storyboard.Sequence.Name != "EXP014-named" {
		t.Fatalf("stamped name must win, got %q", got[0].Storyboard.Sequence.Name)
	}
}

func TestExpandVariants_NoPhantomNormalizeIDDiffs(t *testing.T) {
	// A base authored without scene/shot IDs: Normalize assigns them, and
	// the diff must not report those as variant changes.
	got, err := ExpandVariants(ExpandVariantsInput{
		Base:         baseStoryboard(t),
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "clean", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"Different"`)}},
		},
		EnforceSingleVariable: true,
	})
	if err != nil {
		t.Fatalf("ExpandVariants: %v", err)
	}
	for _, p := range got[0].ChangedPaths {
		if strings.HasSuffix(p, ".id") {
			t.Fatalf("phantom normalize-ID diff reported: %v", got[0].ChangedPaths)
		}
	}
}

func TestExpandVariants_BaseNotMutated(t *testing.T) {
	base := baseStoryboard(t)
	originalName := base.Sequence.Name
	originalContent := base.Scenes[0].Shots[0].Text[0].Content
	_, err := ExpandVariants(ExpandVariantsInput{
		Base:         base,
		ExperimentID: "EXP014",
		Variants: []VariantSpec{
			{VariantID: "v", Overrides: map[string]json.RawMessage{
				"scenes[0].shots[0].text[0].content": raw(`"Changed"`)}},
		},
	})
	if err != nil {
		t.Fatalf("ExpandVariants: %v", err)
	}
	if base.Sequence.Name != originalName || base.Scenes[0].Shots[0].Text[0].Content != originalContent {
		t.Fatalf("expansion mutated the caller's base storyboard")
	}
}
