package orchestrator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var driveUnsafeFilenameChars = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)

// sanitizeDriveFilename makes a Drive file title safe to use as a local
// filename: strips path separators and control characters, and prefixes
// with the (unique) Drive file ID so repeated downloads of same-titled files
// never collide or overwrite each other.
func sanitizeDriveFilename(fileID, title string) string {
	clean := strings.TrimSpace(driveUnsafeFilenameChars.ReplaceAllString(title, "_"))
	clean = strings.TrimLeft(clean, ".")
	if clean == "" {
		clean = "drive_file"
	}
	idPrefix := fileID
	if len(idPrefix) > 8 {
		idPrefix = idPrefix[:8]
	}
	return idPrefix + "_" + clean
}

// ImportFromDriveDownload imports a Google Drive file into the open Premiere
// Pro project, given the path to a saved result of an MCP
// download_file_content call against the "claude.ai Google Drive" connector.
//
// The Go orchestrator has no access to that connector — it's authenticated
// against the calling Claude session, not this process — so it cannot fetch
// Drive files on its own. The intended flow is: the calling agent uses the
// Drive connector's search_files/download_file_content tools itself, which
// (for anything but tiny files) redirects its output to a JSON file on disk
// rather than inlining it (schema: {content: base64 string, id, mimeType,
// title}); the agent then hands this tool the path to that file. This tool
// decodes the content, writes it under a "Downloaded Media" folder next to
// the project, and imports it exactly like premiere_import_media would.
func (e *Engine) ImportFromDriveDownload(ctx context.Context, resultFilePath, targetBin string) (*ImportResult, error) {
	if resultFilePath == "" {
		return nil, fmt.Errorf("import_from_drive_download: result_file_path is required — pass the file path returned by the Google Drive connector's download_file_content tool, not its content")
	}
	raw, err := os.ReadFile(resultFilePath)
	if err != nil {
		return nil, fmt.Errorf("import_from_drive_download: read result file: %w", err)
	}
	var dl struct {
		Content  string `json:"content"`
		ID       string `json:"id"`
		MimeType string `json:"mimeType"`
		Title    string `json:"title"`
	}
	if err := json.Unmarshal(raw, &dl); err != nil {
		return nil, fmt.Errorf("import_from_drive_download: %q is not a Google Drive download_file_content result (expected JSON with a base64 \"content\" field): %w", resultFilePath, err)
	}
	if dl.Content == "" {
		return nil, fmt.Errorf("import_from_drive_download: result file has no content field — for Google-native files (Docs/Sheets/Slides) download_file_content needs exportMimeType and returns exported text, not binary media")
	}
	data, err := base64.StdEncoding.DecodeString(dl.Content)
	if err != nil {
		return nil, fmt.Errorf("import_from_drive_download: decode base64 content: %w", err)
	}

	dir := e.projectSubDir(ctx, "Downloaded Media")
	outPath := filepath.Join(dir, sanitizeDriveFilename(dl.ID, dl.Title))
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("import_from_drive_download: write decoded file: %w", err)
	}

	res, err := e.ImportMedia(ctx, outPath, targetBin)
	if err != nil {
		return nil, fmt.Errorf("import_from_drive_download: downloaded %q to %s but import failed: %w", dl.Title, outPath, err)
	}
	return res, nil
}
