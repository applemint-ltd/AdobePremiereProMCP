package storyboard

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Variant expansion: one base storyboard + per-variant overrides -> N
// validated storyboards with deterministic sequence names. This is the A/B
// mechanism: the overrides ARE the experiment definition, and the diff gate
// turns "one variable per experiment" from a guideline into a check — it
// diffs the actual documents, so an override that restates a base value, or
// an agent "improving" unrelated fields, is caught either way.
// Pure data + logic like the rest of this package: no bridge calls.
// ---------------------------------------------------------------------------

// VariantSpec is one experiment arm: an id plus the field overrides that
// distinguish it from the base.
type VariantSpec struct {
	VariantID string `json:"variant_id"`
	Label     string `json:"label,omitempty"`
	// Overrides maps field paths to replacement values, e.g.
	//   {"scenes[0].shots[0].text[0].content": "New hook"}
	// A null value deletes the field. Missing intermediate objects are
	// created; array indexes must already exist (arrays are never extended).
	Overrides map[string]json.RawMessage `json:"overrides"`
}

// ExpandVariantsInput parameterizes an expansion.
type ExpandVariantsInput struct {
	Base         *Storyboard
	ExperimentID string
	Variants     []VariantSpec
	// NamingTemplate renders each variant's sequence name; placeholders
	// {experiment_id}, {variant_id}, {label}. Empty means
	// "{experiment_id}-{variant_id}".
	NamingTemplate string
	// EnforceSingleVariable requires each variant to differ from base in
	// exactly one field path — the discipline that makes A/B results
	// attributable.
	EnforceSingleVariable bool
	// AllowedExtraPaths exempts specific field paths from the single-variable
	// gate (sequence.name is always exempt — it carries the variant identity).
	AllowedExtraPaths []string
}

// ExpandedVariant is one output storyboard plus its experiment identity.
type ExpandedVariant struct {
	ExperimentID string      `json:"experiment_id"`
	VariantID    string      `json:"variant_id"`
	Label        string      `json:"label,omitempty"`
	SequenceName string      `json:"sequence_name"`
	ChangedPaths []string    `json:"changed_paths"`
	Storyboard   *Storyboard `json:"storyboard"`
}

const defaultNamingTemplate = "{experiment_id}-{variant_id}"

// Any {...} token, so a typo'd placeholder with digits, hyphens, or spaces
// (e.g. "{experiment-id}", "{v2}") is caught, not shipped literally.
var placeholderRe = regexp.MustCompile(`\{[^}]*\}`)

// ExpandVariants expands a base storyboard into one validated storyboard per
// variant. Every failure names the offending variant; any failure aborts the
// whole expansion (a partially-expanded experiment is worse than none).
func ExpandVariants(in ExpandVariantsInput) ([]ExpandedVariant, error) {
	if in.Base == nil {
		return nil, fmt.Errorf("expand_variants: a base storyboard is required")
	}
	if strings.TrimSpace(in.ExperimentID) == "" {
		return nil, fmt.Errorf("expand_variants: experiment_id is required")
	}
	if len(in.Variants) == 0 {
		return nil, fmt.Errorf("expand_variants: at least one variant is required")
	}
	if problems := in.Base.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("expand_variants: the base storyboard is invalid: %s", strings.Join(problems, " "))
	}
	tpl := in.NamingTemplate
	if tpl == "" {
		tpl = defaultNamingTemplate
	}

	// Normalize a deep copy of the base before anything else: Parse()
	// normalizes (auto-assigning scene/shot IDs), so diffing an
	// un-normalized base against a re-parsed variant would report phantom
	// id changes. The round-trip through Parse also avoids mutating the
	// caller's storyboard (Normalize edits scenes in place).
	rawBase, err := json.Marshal(in.Base)
	if err != nil {
		return nil, fmt.Errorf("expand_variants: could not serialize the base storyboard: %w", err)
	}
	baseCopy, err := Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("expand_variants: base storyboard did not round-trip: %w", err)
	}
	baseJSON, err := json.Marshal(baseCopy)
	if err != nil {
		return nil, fmt.Errorf("expand_variants: could not serialize the base storyboard: %w", err)
	}
	var baseMap map[string]any
	if err := json.Unmarshal(baseJSON, &baseMap); err != nil {
		return nil, fmt.Errorf("expand_variants: could not decode the base storyboard: %w", err)
	}

	exempt := map[string]bool{"sequence.name": true}
	for _, p := range in.AllowedExtraPaths {
		exempt[p] = true
	}

	out := make([]ExpandedVariant, 0, len(in.Variants))
	seenIDs := map[string]bool{}
	seenNames := map[string]string{} // sequence name -> variant id
	for _, v := range in.Variants {
		if strings.TrimSpace(v.VariantID) == "" {
			return nil, fmt.Errorf("expand_variants: every variant needs a variant_id")
		}
		if seenIDs[v.VariantID] {
			return nil, fmt.Errorf("expand_variants: duplicate variant_id %q", v.VariantID)
		}
		seenIDs[v.VariantID] = true

		// Fresh working copy of the base document per variant.
		var varMap map[string]any
		if err := json.Unmarshal(baseJSON, &varMap); err != nil {
			return nil, fmt.Errorf("expand_variants: internal copy failed: %w", err)
		}
		for _, path := range sortedKeys(v.Overrides) {
			if err := applyOverride(varMap, path, v.Overrides[path]); err != nil {
				return nil, fmt.Errorf("expand_variants: variant %q override %q: %w", v.VariantID, path, err)
			}
		}

		// The diff gate compares the actual documents (base vs mutated),
		// not the override keys — a no-op override changes nothing and an
		// override list understates nothing.
		changed := leafDiff("", baseMap, varMap)
		filtered := changed[:0]
		for _, p := range changed {
			if !exempt[p] {
				filtered = append(filtered, p)
			}
		}
		changed = filtered
		if in.EnforceSingleVariable {
			if len(changed) == 0 {
				return nil, fmt.Errorf("expand_variants: variant %q is identical to the base — an A/B arm must change exactly one field", v.VariantID)
			}
			if len(changed) > 1 {
				return nil, fmt.Errorf("expand_variants: variant %q changes %d fields (%s) — one variable per experiment; pass allowed_extra_paths or disable enforce_single_variable if this is intentional", v.VariantID, len(changed), strings.Join(changed, ", "))
			}
		}

		// Round-trip through the strict parser: unknown-field typos and
		// structural damage die here, at expansion time, not at assembly.
		varJSON, err := json.Marshal(varMap)
		if err != nil {
			return nil, fmt.Errorf("expand_variants: variant %q: could not re-serialize: %w", v.VariantID, err)
		}
		sb, err := Parse(varJSON)
		if err != nil {
			return nil, fmt.Errorf("expand_variants: variant %q: %w", v.VariantID, err)
		}

		// Deterministic identity is stamped after overrides so nothing can
		// clobber it.
		name, err := renderNameTemplate(tpl, in.ExperimentID, v.VariantID, v.Label)
		if err != nil {
			return nil, fmt.Errorf("expand_variants: variant %q: %w", v.VariantID, err)
		}
		if other, dup := seenNames[name]; dup {
			return nil, fmt.Errorf("expand_variants: variants %q and %q render the same sequence name %q — fix the naming template or variant ids", other, v.VariantID, name)
		}
		seenNames[name] = v.VariantID
		if sb.Sequence == nil {
			sb.Sequence = &SequenceSpec{}
		}
		sb.Sequence.Name = name

		if problems := sb.Validate(); len(problems) > 0 {
			return nil, fmt.Errorf("expand_variants: variant %q would not validate: %s", v.VariantID, strings.Join(problems, " "))
		}

		out = append(out, ExpandedVariant{
			ExperimentID: in.ExperimentID,
			VariantID:    v.VariantID,
			Label:        v.Label,
			SequenceName: name,
			ChangedPaths: changed,
			Storyboard:   sb,
		})
	}
	return out, nil
}

// renderNameTemplate substitutes the known placeholders and rejects unknown
// ones (a typo'd placeholder must not silently ship in a sequence name).
func renderNameTemplate(tpl, experimentID, variantID, label string) (string, error) {
	out := strings.ReplaceAll(tpl, "{experiment_id}", experimentID)
	out = strings.ReplaceAll(out, "{variant_id}", variantID)
	out = strings.ReplaceAll(out, "{label}", label)
	if unknown := placeholderRe.FindString(out); unknown != "" {
		return "", fmt.Errorf("unknown placeholder %s in naming template %q (known: {experiment_id}, {variant_id}, {label})", unknown, tpl)
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("naming template %q rendered an empty sequence name", tpl)
	}
	return out, nil
}

// pathSegment is one step of a field path: a key, optionally indexed.
type pathSegment struct {
	key   string
	index int // -1 = no index
}

var segmentRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)(\[(\d+)\])?$`)

func parsePath(path string) ([]pathSegment, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("empty field path")
	}
	parts := strings.Split(path, ".")
	segs := make([]pathSegment, 0, len(parts))
	for _, part := range parts {
		m := segmentRe.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("malformed path segment %q (expected key or key[N])", part)
		}
		seg := pathSegment{key: m[1], index: -1}
		if m[3] != "" {
			idx, err := strconv.Atoi(m[3])
			if err != nil {
				return nil, fmt.Errorf("bad array index in %q", part)
			}
			seg.index = idx
		}
		segs = append(segs, seg)
	}
	return segs, nil
}

// applyOverride sets (or, for null, deletes) the value at path inside root.
// Missing intermediate objects are created; array indexes must be in range —
// arrays are never implicitly extended.
func applyOverride(root map[string]any, path string, value json.RawMessage) error {
	segs, err := parsePath(path)
	if err != nil {
		return err
	}

	container := any(root)
	for i, seg := range segs {
		last := i == len(segs)-1
		m, ok := container.(map[string]any)
		if !ok {
			return fmt.Errorf("segment %q: parent is not an object", seg.key)
		}
		child, exists := m[seg.key]

		if seg.index < 0 {
			if last {
				if isJSONNull(value) {
					delete(m, seg.key)
					return nil
				}
				var decoded any
				if err := json.Unmarshal(value, &decoded); err != nil {
					return fmt.Errorf("value is not valid JSON: %w", err)
				}
				m[seg.key] = decoded
				return nil
			}
			if !exists {
				// A null-delete of a leaf under a missing parent is a no-op:
				// the field is already absent. Do NOT materialize a phantom
				// empty object (it would trip Validate/the diff gate).
				if isJSONNull(value) {
					return nil
				}
				next := map[string]any{}
				m[seg.key] = next
				container = next
				continue
			}
			container = child
			continue
		}

		// Indexed segment: the array must already exist and cover the index.
		if !exists {
			return fmt.Errorf("array %q does not exist", seg.key)
		}
		arr, ok := child.([]any)
		if !ok {
			return fmt.Errorf("%q is not an array", seg.key)
		}
		if seg.index >= len(arr) {
			return fmt.Errorf("index %d out of range for %q (length %d; arrays are never extended by an override)", seg.index, seg.key, len(arr))
		}
		if last {
			if isJSONNull(value) {
				return fmt.Errorf("cannot delete array element %s[%d] — override the whole array instead", seg.key, seg.index)
			}
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				return fmt.Errorf("value is not valid JSON: %w", err)
			}
			arr[seg.index] = decoded
			return nil
		}
		container = arr[seg.index]
	}
	return nil
}

func isJSONNull(v json.RawMessage) bool {
	return len(bytes.TrimSpace(v)) == 4 && string(bytes.TrimSpace(v)) == "null"
}

// leafDiff reports the field paths whose leaf values differ between two
// decoded JSON documents. A key present on only one side is reported at the
// key's own path; arrays of different lengths are reported at the array's
// path; equal-length arrays recurse element-wise.
func leafDiff(prefix string, a, b any) []string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			return []string{prefix}
		}
		keys := map[string]bool{}
		for k := range av {
			keys[k] = true
		}
		for k := range bv {
			keys[k] = true
		}
		var out []string
		for k := range keys {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			ac, aok := av[k]
			bc, bok := bv[k]
			if !aok || !bok {
				out = append(out, p)
				continue
			}
			out = append(out, leafDiff(p, ac, bc)...)
		}
		sort.Strings(out)
		return out
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return []string{prefix}
		}
		if len(av) != len(bv) {
			return []string{prefix}
		}
		var out []string
		for i := range av {
			out = append(out, leafDiff(fmt.Sprintf("%s[%d]", prefix, i), av[i], bv[i])...)
		}
		return out
	default:
		if !reflect.DeepEqual(a, b) {
			return []string{prefix}
		}
		return nil
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
