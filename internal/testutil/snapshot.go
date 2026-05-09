package testutil

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "update snapshot files")

// SnapshotCompare compares got against a snapshot file.
// If -update is passed, it writes got to the snapshot file.
// Snapshot files are stored alongside testdata.
func SnapshotCompare(t *testing.T, got, snapshotPath string) {
	t.Helper()

	// Normalize line endings to LF for cross-platform consistency
	got = strings.ReplaceAll(got, "\r\n", "\n")

	if *update {
		dir := filepath.Dir(snapshotPath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("create snapshot dir: %v", err)
		}
		if err := os.WriteFile(snapshotPath, []byte(got), 0644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		return
	}

	data, err := os.ReadFile(snapshotPath)
	if err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("snapshot not found: %s\nRun with -update to create it.", snapshotPath)
		}
		t.Fatalf("read snapshot: %v", err)
	}

	want := strings.ReplaceAll(string(data), "\r\n", "\n")
	if got != want {
		// Show a concise diff
		linesGot := strings.Split(got, "\n")
		linesWant := strings.Split(want, "\n")
		maxLines := len(linesGot)
		if len(linesWant) > maxLines {
			maxLines = len(linesWant)
		}
		var diff strings.Builder
		for i := 0; i < maxLines && i < 20; i++ {
			g, w := "", ""
			if i < len(linesGot) {
				g = linesGot[i]
			}
			if i < len(linesWant) {
				w = linesWant[i]
			}
			if g != w {
				diff.WriteString(fmt.Sprintf("-%s\n+%s\n", w, g))
			}
		}
		t.Fatalf("snapshot mismatch: %s\nDiff (first 20 lines):\n%s", snapshotPath, diff.String())
	}
}
