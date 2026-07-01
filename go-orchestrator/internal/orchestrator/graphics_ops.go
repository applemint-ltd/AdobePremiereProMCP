package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
)

// ---------------------------------------------------------------------------
// Motion Graphics Templates (MOGRTs)
// ---------------------------------------------------------------------------

func (e *Engine) ImportMOGRT(ctx context.Context, mogrtPath, timeTicks string, videoTrackOffset, audioTrackOffset int) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"mogrtPath": mogrtPath,
		"timeTicks": timeTicks,
		"videoTrackOffset": videoTrackOffset,
		"audioTrackOffset": audioTrackOffset,
	})
	result, err := e.premiere.EvalCommand(ctx, "importMOGRT", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("ImportMOGRT: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) GetMOGRTProperties(ctx context.Context, trackIndex, clipIndex int) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
	})
	result, err := e.premiere.EvalCommand(ctx, "getMOGRTProperties", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("GetMOGRTProperties: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) SetMOGRTText(ctx context.Context, trackIndex, clipIndex, propertyIndex int, text string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
		"propertyIndex": propertyIndex,
		"text": text,
	})
	result, err := e.premiere.EvalCommand(ctx, "setMOGRTText", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("SetMOGRTText: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) SetMOGRTProperty(ctx context.Context, trackIndex, clipIndex int, propertyName string, value string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
		"propertyName": propertyName,
		"value": value,
	})
	result, err := e.premiere.EvalCommand(ctx, "setMOGRTProperty", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("SetMOGRTProperty: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// ---------------------------------------------------------------------------
// Titles & Lower Thirds
// ---------------------------------------------------------------------------

func (e *Engine) AddTitle(ctx context.Context, text string, trackIndex int, startTime, duration float64, styleJSON string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"text": text,
		"trackIndex": trackIndex,
		"startTime": startTime,
		"duration": duration,
		"styleJSON": styleJSON,
	})
	result, err := e.premiere.EvalCommand(ctx, "addTitle", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("AddTitle: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) AddLowerThird(ctx context.Context, name, title string, trackIndex int, startTime, duration float64) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"name": name,
		"title": title,
		"trackIndex": trackIndex,
		"startTime": startTime,
		"duration": duration,
	})
	result, err := e.premiere.EvalCommand(ctx, "addLowerThird", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("AddLowerThird: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// ---------------------------------------------------------------------------
// Captions & Subtitles
// ---------------------------------------------------------------------------

func (e *Engine) CreateCaptionTrack(ctx context.Context, filePath string, startTime float64, format string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"filePath":  filePath,
		"startTime": startTime,
		"format":    format,
	})
	result, err := e.premiere.EvalCommand(ctx, "createCaptionTrack", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("CreateCaptionTrack: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) ImportCaptions(ctx context.Context, filePath, format string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"filePath": filePath,
		"format": format,
	})
	result, err := e.premiere.EvalCommand(ctx, "importCaptions", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("ImportCaptions: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) GetCaptions(ctx context.Context, trackIndex int) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
	})
	result, err := e.premiere.EvalCommand(ctx, "getCaptions", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("GetCaptions: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// srtTimestamp formats seconds as an SRT "HH:MM:SS,mmm" timestamp.
func srtTimestamp(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	h := int(sec) / 3600
	m := (int(sec) % 3600) / 60
	s := int(sec) % 60
	ms := int(math.Round((sec - math.Floor(sec)) * 1000))
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

// AddCaption creates a native, editable text-based caption track for a
// single caption. Premiere's ExtendScript DOM can only create a caption
// track from an imported caption source file (.srt/.vtt) — there is no API
// to append a caption to an existing track. It also cannot reliably import a
// file that ExtendScript's own File object just wrote (a same-app
// write/import visibility race observed live), so the one-entry SRT is
// generated and written here, in the Go process, before handing the path to
// createCaptionTrack.
func (e *Engine) AddCaption(ctx context.Context, startTime, endTime float64, text, format string) (*GenericResult, error) {
	if endTime <= startTime {
		endTime = startTime + 3.0
	}
	srt := fmt.Sprintf("1\n%s --> %s\n%s\n", srtTimestamp(startTime), srtTimestamp(endTime), text)

	tmpFile, err := os.CreateTemp(e.generatedMediaDir(ctx), "premiere_caption_*.srt")
	if err != nil {
		return nil, fmt.Errorf("AddCaption: create temp SRT: %w", err)
	}
	tmpPath := tmpFile.Name()
	_, writeErr := tmpFile.WriteString(srt)
	closeErr := tmpFile.Close()
	if writeErr != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("AddCaption: write temp SRT: %w", writeErr)
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("AddCaption: close temp SRT: %w", closeErr)
	}
	// Deliberately not removed: the created caption track's ProjectItem
	// keeps pointing at this path as its media source, so deleting it would
	// leave the caption source offline.

	argsJSON, _ := json.Marshal(map[string]any{
		"filePath":  tmpPath,
		"startTime": 0,
		"format":    format,
	})
	result, err := e.premiere.EvalCommand(ctx, "createCaptionTrack", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("AddCaption: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) EditCaption(ctx context.Context, trackIndex, captionIndex int, text string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"captionIndex": captionIndex,
		"text": text,
	})
	result, err := e.premiere.EvalCommand(ctx, "editCaption", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("EditCaption: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) DeleteCaption(ctx context.Context, trackIndex, captionIndex int) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"captionIndex": captionIndex,
	})
	result, err := e.premiere.EvalCommand(ctx, "deleteCaption", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("DeleteCaption: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) ExportCaptions(ctx context.Context, outputPath, format string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"outputPath": outputPath,
		"format": format,
	})
	result, err := e.premiere.EvalCommand(ctx, "exportCaptions", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("ExportCaptions: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) StyleCaptions(ctx context.Context, trackIndex int, font string, size float64, color, bgColor, position string) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"font": font,
		"size": size,
		"color": color,
		"bgColor": bgColor,
		"position": position,
	})
	result, err := e.premiere.EvalCommand(ctx, "styleCaptions", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("StyleCaptions: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// ---------------------------------------------------------------------------
// Graphics
// ---------------------------------------------------------------------------

func (e *Engine) CreateColorMatte(ctx context.Context, name string, red, green, blue, width, height int) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"name": name,
		"red": red,
		"green": green,
		"blue": blue,
		"width": width,
		"height": height,
	})
	result, err := e.premiere.EvalCommand(ctx, "createColorMatte", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("CreateColorMatte: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) PlaceColorMatte(ctx context.Context, projectItemIndex, trackIndex int, startTime, duration float64) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"projectItemIndex": projectItemIndex,
		"trackIndex": trackIndex,
		"startTime": startTime,
		"duration": duration,
	})
	result, err := e.premiere.EvalCommand(ctx, "placeColorMatte", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("PlaceColorMatte: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) CreateTransparentVideo(ctx context.Context, name string, width, height int, duration float64) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"name": name,
		"width": width,
		"height": height,
		"duration": duration,
	})
	result, err := e.premiere.EvalCommand(ctx, "createTransparentVideo", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("CreateTransparentVideo: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// ---------------------------------------------------------------------------
// Speed & Time (Time Remapping, Freeze Frame)
// ---------------------------------------------------------------------------

func (e *Engine) SetTimeRemapping(ctx context.Context, trackIndex, clipIndex int, enabled bool) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
		"enabled": enabled,
	})
	result, err := e.premiere.EvalCommand(ctx, "setTimeRemapping", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("SetTimeRemapping: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) AddTimeRemapKeyframe(ctx context.Context, trackIndex, clipIndex int, time, speed float64) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
		"time": time,
		"speed": speed,
	})
	result, err := e.premiere.EvalCommand(ctx, "addTimeRemapKeyframe", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("AddTimeRemapKeyframe: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

func (e *Engine) FreezeFrame(ctx context.Context, trackIndex, clipIndex int, time, duration float64) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
		"time": time,
		"duration": duration,
	})
	result, err := e.premiere.EvalCommand(ctx, "freezeFrame", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("FreezeFrame: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}

// ---------------------------------------------------------------------------
// Scene Edit Detection
// ---------------------------------------------------------------------------

func (e *Engine) DetectSceneEdits(ctx context.Context, trackIndex, clipIndex int, sensitivity float64) (*GenericResult, error) {
	argsJSON, _ := json.Marshal(map[string]any{
		"trackIndex": trackIndex,
		"clipIndex": clipIndex,
		"sensitivity": sensitivity,
	})
	result, err := e.premiere.EvalCommand(ctx, "detectSceneEdits", string(argsJSON))
	if err != nil {
		return nil, fmt.Errorf("DetectSceneEdits: %w", err)
	}
	return &GenericResult{Status: "success", Message: result}, nil
}
