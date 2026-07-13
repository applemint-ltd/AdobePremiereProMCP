package storyboard

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// FromShotListCSV parses a human-friendly shot list into a storyboard.
//
// Header-driven and order-independent; recognized columns (all optional
// except clip): order, clip, duration, from, to, text, caption, transition,
// notes. Times accept plain seconds ("4.5") or mm:ss ("1:20"). This is
// deliberately NOT the old editor-grade 5-column file,in,out,track,position
// format — no track indices, no timeline positions.
func FromShotListCSV(r io.Reader) (*Storyboard, error) {
	cr := csv.NewReader(r)
	cr.TrimLeadingSpace = true
	cr.FieldsPerRecord = -1

	rows, err := cr.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("could not read the CSV: %w", err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("the shot list needs a header row and at least one shot row")
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := col["clip"]; !ok {
		return nil, fmt.Errorf("the shot list needs a \"clip\" column (found: %s) — one row per shot, clip = the file or clip name", strings.Join(rows[0], ", "))
	}

	get := func(row []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	type orderedShot struct {
		order int
		shot  Shot
	}
	var shots []orderedShot

	for ri, row := range rows[1:] {
		if len(row) == 0 || strings.Join(row, "") == "" {
			continue
		}
		rowLabel := fmt.Sprintf("row %d", ri+2)

		shot := Shot{
			Clip:    get(row, "clip"),
			Caption: get(row, "caption"),
			Notes:   get(row, "notes"),
		}

		if d := get(row, "duration"); d != "" {
			secs, err := parseClock(d)
			if err != nil {
				return nil, fmt.Errorf("%s: duration %q — use seconds (4.5) or m:ss (1:20)", rowLabel, d)
			}
			shot.DurationSeconds = secs
		}
		from, to := get(row, "from"), get(row, "to")
		if from != "" || to != "" {
			f, err1 := parseClock(from)
			t, err2 := parseClock(to)
			if from == "" {
				f, err1 = 0, nil
			}
			if err1 != nil || err2 != nil || to == "" {
				return nil, fmt.Errorf("%s: from/to must both be times (seconds or m:ss); got from=%q to=%q", rowLabel, from, to)
			}
			shot.Trim = &TrimHint{FromSeconds: f, ToSeconds: t}
		}
		if txt := get(row, "text"); txt != "" {
			shot.Text = []TextOverlay{{Content: txt, Style: "title"}}
		}
		if tr := get(row, "transition"); tr != "" && !strings.EqualFold(tr, "none") && !strings.EqualFold(tr, "cut") {
			shot.TransitionAfter = &Transition{Name: normalizeTransition(tr)}
		}

		order := ri + 1
		if o := get(row, "order"); o != "" {
			n, err := strconv.Atoi(o)
			if err != nil {
				return nil, fmt.Errorf("%s: order %q is not a whole number", rowLabel, o)
			}
			order = n
		}
		shots = append(shots, orderedShot{order: order, shot: shot})
	}

	if len(shots) == 0 {
		return nil, fmt.Errorf("no shot rows found under the header")
	}

	// Stable sort by explicit order; equal orders keep file order.
	for i := 1; i < len(shots); i++ {
		for j := i; j > 0 && shots[j].order < shots[j-1].order; j-- {
			shots[j], shots[j-1] = shots[j-1], shots[j]
		}
	}

	sb := &Storyboard{
		Version: Version,
		Scenes:  []Scene{{Name: "Shot list", Shots: make([]Shot, 0, len(shots))}},
	}
	for _, os := range shots {
		sb.Scenes[0].Shots = append(sb.Scenes[0].Shots, os.shot)
	}
	sb.Normalize()
	return sb, nil
}

// parseClock accepts "90", "4.5", "1:20", or "01:02:03".
func parseClock(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty time")
	}
	if !strings.Contains(s, ":") {
		return strconv.ParseFloat(s, 64)
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, fmt.Errorf("too many ':' in %q", s)
	}
	total := 0.0
	for _, p := range parts {
		v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil {
			return 0, err
		}
		total = total*60 + v
	}
	return total, nil
}

// normalizeTransition maps casual names onto Premiere transition names.
func normalizeTransition(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "dissolve", "crossfade", "cross fade", "cross dissolve", "fade":
		return "Cross Dissolve"
	case "dip to black", "fade to black":
		return "Dip to Black"
	case "dip to white", "fade to white":
		return "Dip to White"
	default:
		return s
	}
}
