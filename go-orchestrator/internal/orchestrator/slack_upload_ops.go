package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// slackUploadMaxBytes caps uploads well under Slack's 1 GB limit — previews
// and contact sheets should never be anywhere near this.
const slackUploadMaxBytes = 512 << 20 // 512 MB

// PostFileResult reports a completed Slack upload.
type PostFileResult struct {
	Status    string `json:"status"`
	FileName  string `json:"file_name"`
	SizeBytes int64  `json:"size_bytes"`
	Channel   string `json:"channel"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

// PostFileToSlack uploads a local file into a Slack channel/thread using the
// server's own bot token — the mirror image of FetchSlackAttachment. This is
// how previews, contact sheets, and exports get back to the remote user: the
// Slack bot's agent has only premiere MCP tools, so the upload must happen
// here.
//
// Uses Slack's v2 upload flow: files.getUploadURLExternal -> POST bytes ->
// files.completeUploadExternal. Requires the files:write bot scope.
func (e *Engine) PostFileToSlack(ctx context.Context, filePath, channelID, threadTS, title, comment string) (*PostFileResult, error) {
	if filePath == "" || channelID == "" {
		return nil, fmt.Errorf("post_file_to_slack: file_path and channel_id are required (the Slack bot includes them in the prompt's [Slack context] line)")
	}
	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("post_file_to_slack: SLACK_BOT_TOKEN is not set in this server's environment")
	}

	// Only ship files this pipeline produced or ingested — not arbitrary
	// disk contents. Everything the review loop generates lands under the
	// project's subfolders or the OS temp dir (frame captures).
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, fmt.Errorf("post_file_to_slack: bad path: %w", err)
	}
	allowedRoots := []string{os.TempDir()}
	// projectSubDir("") resolves to the open project's own folder (all
	// generated media, previews, and Slack attachments live beneath it).
	if projDir := e.projectSubDir(ctx, ""); projDir != "" {
		allowedRoots = append(allowedRoots, projDir)
	}
	allowed := false
	for _, root := range allowedRoots {
		if root == "" {
			continue
		}
		if rel, err := filepath.Rel(root, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, fmt.Errorf("post_file_to_slack: %s is outside the project and temp directories — only files this pipeline produced can be uploaded", absPath)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("post_file_to_slack: file not found: %s", absPath)
	}
	if info.Size() == 0 {
		return nil, fmt.Errorf("post_file_to_slack: %s is empty — if this is an export, it may still be rendering", absPath)
	}
	if info.Size() > slackUploadMaxBytes {
		return nil, fmt.Errorf("post_file_to_slack: %s is %d MB, over the %d MB upload cap — export a lower-bitrate preview instead", absPath, info.Size()>>20, slackUploadMaxBytes>>20)
	}

	client := &http.Client{Timeout: 300 * time.Second}
	fileName := filepath.Base(absPath)

	// Step 1: reserve an upload URL.
	form := url.Values{
		"filename": {fileName},
		"length":   {fmt.Sprintf("%d", info.Size())},
	}
	var ticket struct {
		OK        bool   `json:"ok"`
		Error     string `json:"error"`
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := slackAPICall(ctx, client, token, "https://slack.com/api/files.getUploadURLExternal", form, &ticket); err != nil {
		return nil, fmt.Errorf("post_file_to_slack: reserve upload: %w", err)
	}
	if !ticket.OK {
		if ticket.Error == "missing_scope" {
			return nil, fmt.Errorf("post_file_to_slack: the bot token is missing the files:write scope — add it in the Slack app config and reinstall the app")
		}
		return nil, fmt.Errorf("post_file_to_slack: Slack refused the upload reservation: %s", ticket.Error)
	}

	// Step 2: POST the bytes to the reserved URL (must also be slack.com).
	if u, err := url.Parse(ticket.UploadURL); err != nil || u.Scheme != "https" || !(u.Host == "slack.com" || strings.HasSuffix(u.Host, ".slack.com")) {
		return nil, fmt.Errorf("post_file_to_slack: Slack returned a non-Slack upload URL (%q); refusing to send the file there", ticket.UploadURL)
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("post_file_to_slack: open file: %w", err)
	}
	defer f.Close()
	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ticket.UploadURL, f)
	if err != nil {
		return nil, fmt.Errorf("post_file_to_slack: build upload request: %w", err)
	}
	upReq.ContentLength = info.Size()
	upReq.Header.Set("Content-Type", "application/octet-stream")
	upResp, err := client.Do(upReq)
	if err != nil {
		return nil, fmt.Errorf("post_file_to_slack: upload failed: %w", err)
	}
	defer upResp.Body.Close()
	if upResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(upResp.Body, 256))
		return nil, fmt.Errorf("post_file_to_slack: upload failed with status %s: %s", upResp.Status, strings.TrimSpace(string(body)))
	}

	// Step 3: complete the upload into the channel/thread.
	filesJSON, _ := json.Marshal([]map[string]string{{"id": ticket.FileID, "title": firstNonEmpty([]string{title, fileName})}})
	complete := url.Values{
		"files":      {string(filesJSON)},
		"channel_id": {channelID},
	}
	if threadTS != "" {
		complete.Set("thread_ts", threadTS)
	}
	if comment != "" {
		complete.Set("initial_comment", comment)
	}
	var done struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := slackAPICall(ctx, client, token, "https://slack.com/api/files.completeUploadExternal", complete, &done); err != nil {
		return nil, fmt.Errorf("post_file_to_slack: complete upload: %w", err)
	}
	if !done.OK {
		return nil, fmt.Errorf("post_file_to_slack: Slack rejected the completed upload: %s", done.Error)
	}

	return &PostFileResult{
		Status:    "uploaded",
		FileName:  fileName,
		SizeBytes: info.Size(),
		Channel:   channelID,
		ThreadTS:  threadTS,
	}, nil
}

// slackAPICall POSTs a form to a Slack Web API method and decodes the JSON
// response.
func slackAPICall(ctx context.Context, client *http.Client, token, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("unexpected response from %s: %.200s", endpoint, string(body))
	}
	return nil
}
