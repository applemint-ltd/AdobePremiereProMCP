package storyboard

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Item is one project item as reported by the host's getProjectItems.
type Item struct {
	Index     int
	Name      string
	MediaPath string
}

// ResolvedClip is the outcome of resolving one clip query against the
// project's items.
type ResolvedClip struct {
	Query      string   `json:"query"`
	Found      bool     `json:"found"`
	Index      int      `json:"index,omitempty"`
	Name       string   `json:"name,omitempty"`
	MediaPath  string   `json:"media_path,omitempty"`
	Candidates []string `json:"candidates,omitempty"` // set when ambiguous
	Reason     string   `json:"reason,omitempty"`     // set when not found
}

// Resolution maps clip queries to project items.
type Resolution struct {
	Clips map[string]ResolvedClip
}

// Resolve finds the project item for a clip query:
// exact name -> case-insensitive name -> basename (sans extension) ->
// unique substring. Ambiguity is an explicit failure listing candidates —
// never a silent first-match.
func Resolve(query string, items []Item) ResolvedClip {
	rc := ResolvedClip{Query: query}
	q := strings.TrimSpace(query)
	if q == "" {
		rc.Reason = "empty clip reference"
		return rc
	}

	match := func(pred func(Item) bool) []Item {
		var out []Item
		for _, it := range items {
			if pred(it) {
				out = append(out, it)
			}
		}
		return out
	}
	stripExt := func(s string) string { return strings.TrimSuffix(s, filepath.Ext(s)) }
	qLower := strings.ToLower(q)
	qBase := strings.ToLower(stripExt(filepath.Base(q)))

	stages := []func(Item) bool{
		func(it Item) bool { return it.Name == q },
		func(it Item) bool { return strings.EqualFold(it.Name, q) },
		func(it Item) bool {
			return strings.ToLower(stripExt(it.Name)) == qBase ||
				strings.ToLower(stripExt(filepath.Base(it.MediaPath))) == qBase
		},
		func(it Item) bool { return strings.Contains(strings.ToLower(it.Name), qLower) },
	}

	for _, stage := range stages {
		hits := match(stage)
		if len(hits) == 1 {
			rc.Found = true
			rc.Index = hits[0].Index
			rc.Name = hits[0].Name
			rc.MediaPath = hits[0].MediaPath
			return rc
		}
		if len(hits) > 1 {
			names := make([]string, 0, len(hits))
			for _, h := range hits {
				names = append(names, h.Name)
			}
			sort.Strings(names)
			rc.Candidates = names
			rc.Reason = fmt.Sprintf("%q matches %d project items — use a more specific name", q, len(hits))
			return rc
		}
	}

	rc.Reason = fmt.Sprintf("no project item matches %q — check the clip name or import the file first", q)
	return rc
}

// ResolveAll resolves every distinct clip query in the storyboard (shots +
// music) against the given items.
func ResolveAll(sb *Storyboard, items []Item) Resolution {
	res := Resolution{Clips: map[string]ResolvedClip{}}
	add := func(query string) {
		if query == "" {
			return
		}
		if _, done := res.Clips[query]; done {
			return
		}
		res.Clips[query] = Resolve(query, items)
	}
	for _, shot := range sb.Shots() {
		add(shot.Clip)
	}
	if sb.Music != nil {
		add(sb.Music.Clip)
	}
	return res
}

// UnresolvedPaths returns queries that failed to resolve but look like file
// paths — candidates for importing before a second resolution pass.
func (r Resolution) UnresolvedPaths() []string {
	var out []string
	for q, rc := range r.Clips {
		if !rc.Found && len(rc.Candidates) == 0 && strings.ContainsAny(q, "/\\") {
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return out
}
