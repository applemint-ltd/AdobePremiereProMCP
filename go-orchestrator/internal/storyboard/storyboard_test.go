package storyboard

import (
	"strings"
	"testing"
)

func items() []Item {
	return []Item{
		{Index: 0, Name: "beach.mp4", MediaPath: "/m/beach.mp4"},
		{Index: 1, Name: "sunset drone", MediaPath: "/m/DJI_0042.mp4"},
		{Index: 2, Name: "interview_wide.mov", MediaPath: "/m/interview_wide.mov"},
		{Index: 3, Name: "interview_close.mov", MediaPath: "/m/interview_close.mov"},
		{Index: 4, Name: "music_upbeat.mp3", MediaPath: "/m/music_upbeat.mp3"},
	}
}

func TestResolveStages(t *testing.T) {
	its := items()

	if rc := Resolve("beach.mp4", its); !rc.Found || rc.Index != 0 {
		t.Fatalf("exact: %+v", rc)
	}
	if rc := Resolve("BEACH.MP4", its); !rc.Found || rc.Index != 0 {
		t.Fatalf("case-insensitive: %+v", rc)
	}
	if rc := Resolve("beach", its); !rc.Found || rc.Index != 0 {
		t.Fatalf("basename sans ext: %+v", rc)
	}
	if rc := Resolve("/somewhere/else/DJI_0042.mp4", its); !rc.Found || rc.Index != 1 {
		t.Fatalf("path basename against media path: %+v", rc)
	}
	if rc := Resolve("sunset", its); !rc.Found || rc.Index != 1 {
		t.Fatalf("unique substring: %+v", rc)
	}

	rc := Resolve("interview", its)
	if rc.Found || len(rc.Candidates) != 2 {
		t.Fatalf("ambiguous should list candidates: %+v", rc)
	}
	rc = Resolve("nonexistent", its)
	if rc.Found || rc.Reason == "" {
		t.Fatalf("missing should carry a reason: %+v", rc)
	}
}

func TestParseValidateNormalize(t *testing.T) {
	doc := `{
	  "title": "Promo",
	  "scenes": [
	    {"name": "Intro", "shots": [
	      {"clip": "beach", "duration_seconds": 4, "caption": "Welcome to summer"},
	      {"text": [{"content": "SUMMER", "style": "title"}], "duration_seconds": 3}
	    ]},
	    {"shots": [{"clip": "sunset", "trim": {"from_seconds": 2, "to_seconds": 6}, "transition_after": {}}]}
	  ],
	  "music": {"clip": "music_upbeat"}
	}`
	sb, err := Parse([]byte(doc))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if problems := sb.Validate(); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	shots := sb.Shots()
	if len(shots) != 3 || shots[0].ID != "s1sh1" || shots[2].ID != "s2sh1" {
		t.Fatalf("IDs not normalized: %+v", shots)
	}
	if tr := shots[2].TransitionAfter; tr.Name != "Cross Dissolve" || tr.DurationSeconds != 1.0 {
		t.Fatalf("transition defaults: %+v", tr)
	}

	if _, err := Parse([]byte(`{"scenes": [], "bogus_field": 1}`)); err == nil || !strings.Contains(err.Error(), "unexpected fields") {
		t.Fatalf("unknown fields should be reported: %v", err)
	}
}

func TestValidateProblems(t *testing.T) {
	sb := &Storyboard{Scenes: []Scene{{Shots: []Shot{
		{},
		{Clip: "x", Trim: &TrimHint{FromSeconds: 5, ToSeconds: 2}},
	}}}}
	sb.Normalize()
	problems := sb.Validate()
	joined := strings.Join(problems, "\n")
	if !strings.Contains(joined, "names no clip") || !strings.Contains(joined, "ends") {
		t.Fatalf("problems: %v", problems)
	}
}

func TestFromShotListCSV(t *testing.T) {
	csv := `order,clip,duration,from,to,text,caption,transition,notes
2,sunset,4,,,,"golden hour",dissolve,
1,beach,,0:02,0:06,SUMMER,,cut,opener
3,interview_wide,6,,,,,fade to black,`
	sb, err := FromShotListCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("FromShotListCSV: %v", err)
	}
	shots := sb.Shots()
	if len(shots) != 3 {
		t.Fatalf("want 3 shots, got %d", len(shots))
	}
	if shots[0].Clip != "beach" || shots[1].Clip != "sunset" || shots[2].Clip != "interview_wide" {
		t.Fatalf("order not applied: %+v", shots)
	}
	if shots[0].Trim == nil || shots[0].Trim.FromSeconds != 2 || shots[0].Trim.ToSeconds != 6 {
		t.Fatalf("m:ss trim: %+v", shots[0].Trim)
	}
	if shots[0].TransitionAfter != nil {
		t.Fatalf("'cut' must mean no transition: %+v", shots[0].TransitionAfter)
	}
	if shots[1].TransitionAfter == nil || shots[1].TransitionAfter.Name != "Cross Dissolve" {
		t.Fatalf("dissolve normalization: %+v", shots[1].TransitionAfter)
	}
	if shots[2].TransitionAfter == nil || shots[2].TransitionAfter.Name != "Dip to Black" {
		t.Fatalf("fade to black normalization: %+v", shots[2].TransitionAfter)
	}
	if shots[0].Text[0].Content != "SUMMER" || shots[1].Caption != "golden hour" {
		t.Fatalf("text/caption: %+v", shots)
	}

	if _, err := FromShotListCSV(strings.NewReader("a,b\n1,2")); err == nil || !strings.Contains(err.Error(), `"clip" column`) {
		t.Fatalf("missing clip column: %v", err)
	}
}

func TestCompile(t *testing.T) {
	csv := `clip,duration,from,to,transition
beach,4,,,dissolve
missing_clip,3,,,
sunset,,1,5,`
	sb, err := FromShotListCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	sb.Music = &MusicSpec{Clip: "music_upbeat"}
	res := ResolveAll(sb, items())

	plan, err := Compile(sb, res)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if len(plan.Shots) != 3 {
		t.Fatalf("shots: %+v", plan.Shots)
	}
	if plan.Shots[0].Clip.Index != 0 || plan.Shots[0].TargetDuration != 4 {
		t.Fatalf("shot0: %+v", plan.Shots[0])
	}
	if !plan.Shots[1].Skipped {
		t.Fatalf("unresolved shot must be skipped with a reason: %+v", plan.Shots[1])
	}
	if plan.Shots[2].Subclip == nil || plan.Shots[2].TargetDuration != 4 {
		t.Fatalf("trim length should set target duration: %+v", plan.Shots[2])
	}
	if plan.Music == nil || len(plan.Warnings) == 0 {
		t.Fatalf("music kept + skip warning expected: music=%+v warnings=%v", plan.Music, plan.Warnings)
	}

	// All shots unresolvable -> hard error.
	bad := &Storyboard{Scenes: []Scene{{Shots: []Shot{{Clip: "nope"}}}}}
	bad.Normalize()
	if _, err := Compile(bad, ResolveAll(bad, items())); err == nil {
		t.Fatalf("expected error when nothing is placeable")
	}
}
