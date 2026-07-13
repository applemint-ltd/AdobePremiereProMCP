package audit

import (
	"strings"
	"testing"
)

const snapBase = `{
  "sequence_name": "Main",
  "video_tracks": [
    {"index": 0, "name": "V1", "clips": [
      {"index": 0, "name": "intro.mov", "start": 0, "end": 6, "duration": 6, "in_point": 0, "out_point": 6, "media_path": "/m/intro.mov", "enabled": true},
      {"index": 1, "name": "beach.mp4", "start": 6, "end": 10, "duration": 4, "in_point": 0, "out_point": 4, "media_path": "/m/beach.mp4", "enabled": true}
    ]}
  ],
  "audio_tracks": []
}`

func TestDiffNoChanges(t *testing.T) {
	d, err := Diff(snapBase, snapBase)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !d.Empty() {
		t.Fatalf("expected empty diff, got %+v", d)
	}
	lines := d.HumanLines()
	if len(lines) != 1 || lines[0] != "No timeline changes." {
		t.Fatalf("lines: %v", lines)
	}
}

func TestDiffAddedRemovedMovedTrimmed(t *testing.T) {
	after := `{
  "sequence_name": "Main",
  "video_tracks": [
    {"index": 0, "name": "V1", "clips": [
      {"index": 0, "name": "intro.mov", "start": 0, "end": 4.5, "duration": 4.5, "in_point": 0, "out_point": 4.5, "media_path": "/m/intro.mov", "enabled": true},
      {"index": 1, "name": "sunset.mp4", "start": 4.5, "end": 9, "duration": 4.5, "in_point": 0, "out_point": 4.5, "media_path": "/m/sunset.mp4", "enabled": true}
    ]}
  ],
  "audio_tracks": []
}`
	d, err := Diff(snapBase, after)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(d.Added) != 1 || d.Added[0].ClipName != "sunset.mp4" {
		t.Fatalf("added: %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ClipName != "beach.mp4" {
		t.Fatalf("removed: %+v", d.Removed)
	}
	if len(d.Modified) != 1 || d.Modified[0].Kind != "trimmed" {
		t.Fatalf("modified: %+v", d.Modified)
	}

	joined := strings.Join(d.HumanLines(), "\n")
	for _, want := range []string{`Added "sunset.mp4"`, `Removed "beach.mp4"`, `Shortened "intro.mov" by 1.5s`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("human lines missing %q:\n%s", want, joined)
		}
	}
}

func TestDiffMovedAndDisabled(t *testing.T) {
	after := strings.ReplaceAll(snapBase, `"start": 6, "end": 10`, `"start": 8, "end": 12`)
	after = strings.ReplaceAll(after, `"out_point": 4, "media_path": "/m/beach.mp4", "enabled": true`,
		`"out_point": 4, "media_path": "/m/beach.mp4", "enabled": false`)
	d, err := Diff(snapBase, after)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	kinds := map[string]bool{}
	for _, m := range d.Modified {
		kinds[m.Kind] = true
	}
	if !kinds["moved"] || !kinds["disabled"] {
		t.Fatalf("want moved+disabled, got %+v", d.Modified)
	}
	joined := strings.Join(d.HumanLines(), "\n")
	if !strings.Contains(joined, `Moved "beach.mp4" from 0:06 to 0:08`) {
		t.Fatalf("human: %s", joined)
	}
}
