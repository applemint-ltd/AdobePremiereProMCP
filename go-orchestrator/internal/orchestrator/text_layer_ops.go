package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/anthropics/premierpro-mcp/go-orchestrator/assets"
)

// projectSubDir returns a directory named subDir next to the open project,
// created if it doesn't exist yet. Falls back to the OS temp directory if
// the project hasn't been saved yet (no project path to anchor to) or its
// path can't be determined, so callers always get a usable directory back.
// Reuses getProjectInfo (already part of the persistent host-script scope)
// rather than adding a dedicated command, since new top-level functions in
// premiere.jsx don't take effect until the CEP panel itself reloads
// core.jsx's #include chain — a plain server restart doesn't do that.
func (e *Engine) projectSubDir(ctx context.Context, subDir string) string {
	// EvalCommand already unwraps the ExtendScript host's {success, data,
	// error} envelope (see unwrapEnvelope in grpc/premiere_client.go) and
	// returns an error directly on failure — result here is already just
	// getProjectInfo's inner data object.
	result, err := e.premiere.EvalCommand(ctx, "getProjectInfo", "{}")
	if err != nil {
		return os.TempDir()
	}
	var info struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(result), &info); err != nil || info.Path == "" {
		return os.TempDir()
	}
	dir := filepath.Join(filepath.Dir(info.Path), subDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return os.TempDir()
	}
	return dir
}

// generatedMediaDir returns a directory to write generated media into
// (rendered text-layer PNGs, synthesized single-caption SRTs) — a "Generated
// Media" folder next to the open project.
func (e *Engine) generatedMediaDir(ctx context.Context) string {
	return e.projectSubDir(ctx, "Generated Media")
}

// TextLayerParams configures a rendered text layer clip.
type TextLayerParams struct {
	Text        string
	FontName    string
	FontSize    float64
	Color       string // hex, e.g. "#FFFFFF"
	X           float64 // normalized 0-1 horizontal anchor (0.5 = centered)
	Y           float64 // normalized 0-1 vertical anchor (0.5 = centered)
	Orientation string  // "horizontal" (default) or "vertical"
	TrackIndex  int
	StartTime   float64
	Duration    float64
	Width       int // canvas width in pixels; defaults to 1920
	Height      int // canvas height in pixels; defaults to 1080
}

// AddTextLayer places a text layer clip on the timeline.
//
// Premiere Pro 2026's ExtendScript DOM cannot make a scripted Essential
// Graphics "Source Text" edit actually render — the data model updates and
// reads back correctly, but the compositor never repaints the glyphs, even
// under a forced fresh render via exportAsMediaDirect (confirmed live). UXP
// does not solve this either: as of writing, no Premiere UXP API exposes
// graphics/text creation at all. Until Adobe fixes the renderer or ships a
// working UXP graphics API, the only way to get exact, arbitrary on-screen
// text is to render it ourselves (via macOS AppKit) to a transparent PNG and
// place that PNG as a clip. This is NOT an editable native text layer —
// changing the text means calling this again with new text, not double-
// clicking the clip in Premiere.
func (e *Engine) AddTextLayer(ctx context.Context, params *TextLayerParams) (*GenericResult, error) {
	if params == nil || params.Text == "" {
		return nil, fmt.Errorf("add_text_layer: text is required")
	}
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("add_text_layer: text rendering uses macOS AppKit and is only available on macOS")
	}

	fontName := params.FontName
	if fontName == "" {
		fontName = "Helvetica-Bold"
	}
	fontSize := params.FontSize
	if fontSize <= 0 {
		fontSize = 96
	}
	color := params.Color
	if color == "" {
		color = "#FFFFFF"
	}
	x := params.X
	if x == 0 {
		x = 0.5
	}
	y := params.Y
	if y == 0 {
		y = 0.5
	}
	orientation := params.Orientation
	if orientation != "vertical" {
		orientation = "horizontal"
	}
	width := params.Width
	if width <= 0 {
		width = 1920
	}
	height := params.Height
	if height <= 0 {
		height = 1080
	}
	duration := params.Duration
	if duration <= 0 {
		duration = 5.0
	}

	swiftFile, err := os.CreateTemp("", "render_text_layer_*.swift")
	if err != nil {
		return nil, fmt.Errorf("add_text_layer: create temp swift script: %w", err)
	}
	defer os.Remove(swiftFile.Name())
	if _, err := swiftFile.Write(assets.RenderTextLayerSwift); err != nil {
		swiftFile.Close()
		return nil, fmt.Errorf("add_text_layer: write temp swift script: %w", err)
	}
	if err := swiftFile.Close(); err != nil {
		return nil, fmt.Errorf("add_text_layer: close temp swift script: %w", err)
	}

	pngFile, err := os.CreateTemp(e.generatedMediaDir(ctx), "premiere_text_layer_*.png")
	if err != nil {
		return nil, fmt.Errorf("add_text_layer: create temp PNG: %w", err)
	}
	pngPath := pngFile.Name()
	pngFile.Close()
	// Deliberately not removed: the placed clip's ProjectItem keeps pointing
	// at this path as its media source.

	cmd := exec.CommandContext(ctx, "swift", swiftFile.Name(),
		pngPath,
		fmt.Sprintf("%d", width), fmt.Sprintf("%d", height),
		params.Text, fontName, fmt.Sprintf("%g", fontSize), color,
		fmt.Sprintf("%g", x), fmt.Sprintf("%g", y), orientation,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(pngPath)
		return nil, fmt.Errorf("add_text_layer: render text image: %w (%s)", err, string(out))
	}

	argsJSON, _ := json.Marshal(map[string]any{
		"filePath":   pngPath,
		"trackIndex": params.TrackIndex,
		"startTime":  params.StartTime,
		"duration":   duration,
	})
	result, err := e.premiere.EvalCommand(ctx, "addTextLayerImage", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("AddTextLayer: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}
