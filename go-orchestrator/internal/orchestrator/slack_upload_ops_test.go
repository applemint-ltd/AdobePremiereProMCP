package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
)

// The NFC/NFD fixtures are built from explicit escapes so no editor or
// tool renormalizes them: "プロジェクト" precomposed (NFC) vs decomposed
// (NFD — base kana + combining voicing marks U+3099/U+309A).
const (
	dirNFC = "\u30d7\u30ed\u30b8\u30a7\u30af\u30c8"             // precomposed
	dirNFD = "\u30d5\u309a\u30ed\u30b7\u3099\u30a7\u30af\u30c8" // decomposed (combining U+309A/U+3099)
)

// These tests exercise the pure helper with literal paths on purpose:
// APFS is normalization-insensitive, so a filesystem-backed NFD/NFC test
// passes with or without the Go-side fix and would guard nothing.

func TestIsAllowedUploadPath_NFDRootNFCPath(t *testing.T) {
	root := "/Users/editor/" + dirNFD
	path := "/Users/editor/" + dirNFC + "/Previews/preview.mp4"
	if !isAllowedUploadPath(path, []string{root}) {
		t.Fatalf("NFC path under NFD-spelled root should be allowed")
	}
}

func TestIsAllowedUploadPath_NFCRootNFDPath(t *testing.T) {
	root := "/Users/editor/" + dirNFC
	path := "/Users/editor/" + dirNFD + "/Previews/preview.mp4"
	if !isAllowedUploadPath(path, []string{root}) {
		t.Fatalf("NFD path under NFC-spelled root should be allowed")
	}
}

func TestIsAllowedUploadPath_OutsideRootRejected(t *testing.T) {
	root := "/Users/editor/project"
	if isAllowedUploadPath("/Users/editor/project-evil/x.mp4", []string{root}) {
		t.Fatalf("sibling directory sharing a name prefix must be rejected")
	}
	if isAllowedUploadPath("/Users/editor/elsewhere/x.mp4", []string{root}) {
		t.Fatalf("unrelated directory must be rejected")
	}
}

func TestIsAllowedUploadPath_TraversalRejected(t *testing.T) {
	root := "/tmp/allowed"
	// PostFileToSlack passes filepath.Abs output, which cleans "..", so the
	// realistic attack shape is a cleaned path that lands outside the root.
	if isAllowedUploadPath("/etc/passwd", []string{root}) {
		t.Fatalf("path outside root must be rejected")
	}
	if isAllowedUploadPath(filepath.Clean("/tmp/allowed/../../etc/passwd"), []string{root}) {
		t.Fatalf("cleaned traversal path must be rejected")
	}
}

func TestIsAllowedUploadPath_ExactRootAndEmptyRootSkip(t *testing.T) {
	root := "/Users/editor/project"
	if !isAllowedUploadPath(root, []string{"", root}) {
		t.Fatalf("the root itself should be allowed; empty roots skipped")
	}
	if isAllowedUploadPath("/anything", []string{""}) {
		t.Fatalf("an empty root alone must allow nothing")
	}
}

func TestIsAllowedUploadPath_SymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	file := filepath.Join(real, "out.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Root spelled via the symlink, candidate via the real path: only the
	// EvalSymlinks branch makes these match.
	if !isAllowedUploadPath(file, []string{link}) {
		t.Fatalf("file under symlinked root should be allowed")
	}
}

func TestIsAllowedUploadPath_NFDDirOnDisk(t *testing.T) {
	// Documents real-world behavior end-to-end: an NFD-named directory on
	// disk, checked with the NFC spelling. On APFS the FS itself may match
	// either spelling; the helper must match regardless of filesystem.
	base := t.TempDir()
	nfdDir := filepath.Join(base, dirNFD)
	if err := os.Mkdir(nfdDir, 0o755); err != nil {
		t.Fatalf("mkdir NFD dir: %v", err)
	}
	file := filepath.Join(nfdDir, "preview.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	nfcSpelling := filepath.Join(base, dirNFC, "preview.mp4")
	if !isAllowedUploadPath(nfcSpelling, []string{nfdDir}) {
		t.Fatalf("NFC spelling of a file in an NFD-named dir should be allowed")
	}
}
