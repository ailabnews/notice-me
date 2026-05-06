package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ApplyPartial constructs the merged result of accepting only the specified
// hunk indices, writes it to the file, and returns nil on success.
//
// The approach:
//  1. Split old_string into lines.
//  2. Walk through old lines, inserting hunk content at the right ranges.
//  3. Lines between hunks (gaps) are preserved from old_string as-is.
//  4. For accepted hunks, use NewLines; for rejected, use OldLines.
//  5. In the actual file, find old_string and replace with the merged text.
func ApplyPartial(filePath string, oldString string, hunks []HunkMeta, acceptedIndices []int) error {
	// Build set of accepted hunk indices for O(1) lookup.
	accepted := make(map[int]bool, len(acceptedIndices))
	for _, idx := range acceptedIndices {
		accepted[idx] = true
	}

	// Split old_string into lines (1-based indexing to match hunk OldStart).
	oldStrLines := strings.Split(oldString, "\n")
	// Remove trailing empty element from trailing newline.
	if len(oldStrLines) > 0 && oldStrLines[len(oldStrLines)-1] == "" {
		oldStrLines = oldStrLines[:len(oldStrLines)-1]
	}

	// Reconstruct merged text: walk through old lines and splice in hunk content.
	var merged strings.Builder
	curLine := 1 // 1-based position in old_string lines

	for i, hunk := range hunks {
		// Emit gap lines between current position and this hunk's start.
		for curLine < hunk.OldStart && curLine <= len(oldStrLines) {
			merged.WriteString(oldStrLines[curLine-1])
			merged.WriteByte('\n')
			curLine++
		}
		// Emit hunk content (new for accepted, old for rejected).
		if accepted[i] {
			for _, line := range hunk.NewLines {
				merged.WriteString(line)
				merged.WriteByte('\n')
			}
		} else {
			for _, line := range hunk.OldLines {
				merged.WriteString(line)
				merged.WriteByte('\n')
			}
		}
		// Advance past the hunk's old lines.
		curLine = hunk.OldStart + hunk.OldCount
	}
	// Emit remaining gap lines after the last hunk.
	for curLine <= len(oldStrLines) {
		merged.WriteString(oldStrLines[curLine-1])
		merged.WriteByte('\n')
		curLine++
	}
	mergedText := merged.String()

	// Read the current file.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	content := string(data)

	// Verify old_string still exists in the file (detect concurrent modification).
	if !strings.Contains(content, oldString) {
		return fmt.Errorf("file has been modified since the edit was requested; the original text no longer matches")
	}

	// Replace old_string with the merged text.
	newContent := strings.Replace(content, oldString, mergedText, 1)

	// Write atomically: temp file + rename.
	dir := filepath.Dir(filePath)
	tmp, err := os.CreateTemp(dir, ".notify-me-diff-*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(newContent); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, filePath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}
