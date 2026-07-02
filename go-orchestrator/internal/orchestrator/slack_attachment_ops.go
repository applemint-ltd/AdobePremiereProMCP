package orchestrator

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var slackFileIDPattern = regexp.MustCompile(`/(F[A-Z0-9]+)/`)

// slackAttachmentFilename picks a collision-safe local filename for a
// downloaded Slack file: prefer the caller-supplied name (as reported by the
// Slack message's files[] payload), falling back to the Content-Disposition
// header and then the URL path. Always prefixed with a short, stable ID so
// repeated fetches of same-named files (very common -- "final.mp4",
// "export.mov") never collide or silently overwrite each other.
func slackAttachmentFilename(fileURL, fileName, contentDisposition string) string {
	name := fileName
	if name == "" && contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			name = params["filename"]
		}
	}
	if name == "" {
		if u, err := url.Parse(fileURL); err == nil {
			name = path.Base(u.Path)
		}
	}
	clean := strings.TrimSpace(driveUnsafeFilenameChars.ReplaceAllString(name, "_"))
	clean = strings.TrimLeft(clean, ".")
	if clean == "" || clean == "_" {
		clean = "slack_file"
	}

	var idPrefix string
	if m := slackFileIDPattern.FindStringSubmatch(fileURL); m != nil {
		idPrefix = m[1]
	} else {
		h := fnv.New32a()
		_, _ = h.Write([]byte(fileURL))
		idPrefix = fmt.Sprintf("%08x", h.Sum32())
	}
	return idPrefix + "_" + clean
}

// FetchSlackAttachment downloads a file that was uploaded to a Slack thread
// and imports it into the open Premiere Pro project.
//
// Unlike ImportFromDriveDownload, this server can do the fetch itself:
// Slack's private file URLs (files.slack.com) just need a bot token in the
// Authorization header, a static credential this process can hold directly
// (SLACK_BOT_TOKEN), rather than a per-user OAuth session. So the calling
// agent only needs to pass the file's URL and name from the Slack message
// payload (the same "premierpro" Slack bot -- cli/src/slack-bot.ts -- that
// dispatches the request already has both), and this tool downloads the raw
// bytes straight to disk and imports them. No decode step, unlike the
// base64-encoded Google Drive connector payload.
func (e *Engine) FetchSlackAttachment(ctx context.Context, fileURL, fileName, targetBin string) (*ImportResult, error) {
	if fileURL == "" {
		return nil, fmt.Errorf("fetch_slack_attachment: file_url is required -- pass the url_private (or url_private_download) field from the Slack message's files[] entry")
	}
	parsed, err := url.Parse(fileURL)
	if err != nil {
		return nil, fmt.Errorf("fetch_slack_attachment: invalid file_url %q: %w", fileURL, err)
	}
	if parsed.Scheme != "https" || !(parsed.Host == "slack.com" || strings.HasSuffix(parsed.Host, ".slack.com")) {
		return nil, fmt.Errorf("fetch_slack_attachment: file_url must be an https://*.slack.com URL (got %q) -- this tool only fetches Slack-hosted attachments, and its bot token must not be sent to other hosts", fileURL)
	}

	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("fetch_slack_attachment: SLACK_BOT_TOKEN is not set in this server's environment -- set it to the same xoxb-... bot token used by cli/src/slack-bot.ts so this tool can authenticate the download")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch_slack_attachment: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch_slack_attachment: download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fetch_slack_attachment: download failed with status %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	// A missing scope or a bot not in the file's channel doesn't fail the
	// HTTP request -- Slack still answers 200, just with its own HTML app
	// shell instead of the file bytes. Catch that here instead of writing
	// garbage to disk and letting Premiere fail with a confusing "unsupported
	// compression type" error later.
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/html") {
		return nil, fmt.Errorf("fetch_slack_attachment: Slack returned an HTML page instead of the file (Content-Type: %s) -- this usually means the bot token is missing the files:read scope, or the bot isn't a member of the channel the file was shared in", ct)
	}

	dir := e.projectSubDir(ctx, "Slack Attachments")
	outPath := filepath.Join(dir, slackAttachmentFilename(fileURL, fileName, resp.Header.Get("Content-Disposition")))

	out, err := os.Create(outPath)
	if err != nil {
		return nil, fmt.Errorf("fetch_slack_attachment: create local file: %w", err)
	}
	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return nil, fmt.Errorf("fetch_slack_attachment: write downloaded file: %w", err)
	}
	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("fetch_slack_attachment: write downloaded file: %w", err)
	}

	res, err := e.ImportMedia(ctx, outPath, targetBin)
	if err != nil {
		return nil, fmt.Errorf("fetch_slack_attachment: downloaded to %s but import failed: %w", outPath, err)
	}
	return res, nil
}
