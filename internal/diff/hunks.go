package diff

import (
	"strings"
)

// ComputeHunks splits the diff between old and new into hunks. A hunk is a
// contiguous group of changed lines, separated from other hunks by at least
// contextGap unchanged lines. Returns the hunks and the full line-level change
// list used to derive them.
//
// The algorithm is a simple longest-common-subsequence-free approach: compare
// old and new line-by-line, grouping runs of equal lines as context and runs
// of different lines as changes.
func ComputeHunks(old, newStr string) []HunkMeta {
	oldLines := splitLines(old)
	newLines := splitLines(newStr)
	changes := lcsDiff(oldLines, newLines)
	return groupIntoHunks(changes)
}

// change represents one line in the diff output.
type change struct {
	typ     string // "equal", "delete", "insert"
	content string
}

// lcsDiff produces a minimal edit script from old to new using a simple
// patience-inspired approach. For our use case (old_string → new_string from
// Claude Code edits), the inputs are typically small, so we use a
// straightforward O(NM) LCS algorithm.
func lcsDiff(old, new []string) []change {
	n, m := len(old), len(new)
	// Build LCS table.
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if old[i-1] == new[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	// Backtrack to produce the edit script.
	var result []change
	i, j := n, m
	for i > 0 && j > 0 {
		if old[i-1] == new[j-1] {
			result = append(result, change{"equal", old[i-1]})
			i--
			j--
		} else if dp[i-1][j] > dp[i][j-1] {
			result = append(result, change{"delete", old[i-1]})
			i--
		} else {
			result = append(result, change{"insert", new[j-1]})
			j--
		}
	}
	for i > 0 {
		result = append(result, change{"delete", old[i-1]})
		i--
	}
	for j > 0 {
		result = append(result, change{"insert", new[j-1]})
		j--
	}

	// Reverse (we backtracked).
	for l, r := 0, len(result)-1; l < r; l, r = l+1, r-1 {
		result[l], result[r] = result[r], result[l]
	}
	return result
}

const contextGap = 3

// groupIntoHunks walks the change list and groups consecutive changes into
// hunks, separated by at least contextGap equal lines.
func groupIntoHunks(changes []change) []HunkMeta {
	if len(changes) == 0 {
		return nil
	}

	// Find change positions (non-equal entries).
	type changeSpan struct {
		start, end int // indices into changes[]
	}
	var spans []changeSpan
	inChange := false
	start := 0
	for i, c := range changes {
		if c.typ != "equal" {
			if !inChange {
				start = i
				inChange = true
			}
		} else {
			if inChange {
				spans = append(spans, changeSpan{start, i - 1})
				inChange = false
			}
		}
	}
	if inChange {
		spans = append(spans, changeSpan{start, len(changes) - 1})
	}
	if len(spans) == 0 {
		return nil
	}

	// Merge spans that are within contextGap equal lines of each other.
	var merged []changeSpan
	cur := spans[0]
	for i := 1; i < len(spans); i++ {
		gap := spans[i].start - cur.end - 1
		if gap < contextGap {
			cur.end = spans[i].end
		} else {
			merged = append(merged, cur)
			cur = spans[i]
		}
	}
	merged = append(merged, cur)

	// Build HunkMeta from each merged span.
	var hunks []HunkMeta

	// Walk changes to track line numbers and extract hunk content.
	// We need to map change indices to old/new line numbers.
	oldLines := make([]int, len(changes)) // 1-based old line for each change entry
	newLines := make([]int, len(changes)) // 1-based new line for each change entry
	ol, nl := 1, 1
	for i, c := range changes {
		switch c.typ {
		case "equal":
			oldLines[i] = ol
			newLines[i] = nl
			ol++
			nl++
		case "delete":
			oldLines[i] = ol
			newLines[i] = 0
			ol++
		case "insert":
			oldLines[i] = 0
			newLines[i] = nl
			nl++
		}
	}

	for idx, span := range merged {
		// Include context lines around the span.
		ctxStart := span.start
		ctxEnd := span.end
		if ctxStart > 0 {
			// Add up to contextGap leading context lines.
			n := contextGap
			for ctxStart > 0 && n > 0 {
				ctxStart--
				if changes[ctxStart].typ == "equal" {
					n--
				} else {
					ctxStart++
					break
				}
			}
		}
		if ctxEnd < len(changes)-1 {
			n := contextGap
			for ctxEnd < len(changes)-1 && n > 0 {
				ctxEnd++
				if changes[ctxEnd].typ == "equal" {
					n--
				} else {
					break
				}
			}
		}

		hunkOldStart := 0
		hunkNewStart := 0
		var hOldLines, hNewLines []string

		for i := span.start; i <= span.end; i++ {
			c := changes[i]
			switch c.typ {
			case "delete":
				if hunkOldStart == 0 {
					hunkOldStart = oldLines[i]
				}
				hOldLines = append(hOldLines, c.content)
			case "insert":
				if hunkNewStart == 0 {
					hunkNewStart = newLines[i]
				}
				hNewLines = append(hNewLines, c.content)
			case "equal":
				if hunkOldStart == 0 {
					hunkOldStart = oldLines[i]
				}
				if hunkNewStart == 0 {
					hunkNewStart = newLines[i]
				}
				hOldLines = append(hOldLines, c.content)
				hNewLines = append(hNewLines, c.content)
			}
		}
		if hunkOldStart == 0 {
			hunkOldStart = 1
		}
		if hunkNewStart == 0 {
			hunkNewStart = 1
		}

		hunks = append(hunks, HunkMeta{
			Index:    idx,
			OldStart: hunkOldStart,
			OldCount: len(hOldLines),
			NewStart: hunkNewStart,
			NewCount: len(hNewLines),
			OldLines: hOldLines,
			NewLines: hNewLines,
		})
	}

	return hunks
}

// splitLines splits s into lines, preserving empty trailing lines.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	// Remove trailing empty line from trailing newline.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
