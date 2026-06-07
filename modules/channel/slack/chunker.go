package slack

import (
	"fmt"
	"unicode/utf8"
)

// chunkMessage splits a message into chunks that fit within maxLen bytes,
// counting the worst-case "[n/m] " prefix that is prepended when there is more
// than one chunk so the final prefixed chunk never exceeds maxLen. Split points
// are chosen on paragraph (\n\n), line (\n), then word (space) boundaries, with
// a hard cut as a last resort — but every cut is snapped back to a UTF-8 rune
// boundary so multibyte runes are never sliced in half.
func chunkMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	// When the message spans multiple chunks each one is prefixed with
	// "[n/m] ". Reserve room for the worst-case prefix so a prefixed chunk
	// cannot exceed maxLen. We don't yet know the total chunk count, so size
	// the reservation from an upper bound on it.
	upperBound := len(text)/maxLen + 1
	prefixLen := len(fmt.Sprintf("[%d/%d] ", upperBound, upperBound))
	budget := maxLen - prefixLen
	if budget < 1 {
		budget = 1
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= budget {
			chunks = append(chunks, remaining)
			break
		}

		// Find best split point within budget bytes.
		cutAt := budget
		if idx := lastIndexBefore(remaining, "\n\n", budget); idx > 0 {
			cutAt = idx + 2
		} else if idx := lastIndexBefore(remaining, "\n", budget); idx > 0 {
			cutAt = idx + 1
		} else if idx := lastIndexBefore(remaining, " ", budget); idx > 0 {
			cutAt = idx + 1
		}

		// Snap the cut back to a UTF-8 rune boundary so a multibyte rune is
		// never split across two chunks.
		cutAt = runeBoundaryAtOrBefore(remaining, cutAt)
		if cutAt <= 0 {
			// The first rune alone exceeds the budget; emit it whole rather
			// than loop forever or slice it.
			_, size := utf8.DecodeRuneInString(remaining)
			cutAt = size
		}

		chunks = append(chunks, remaining[:cutAt])
		remaining = remaining[cutAt:]
	}

	if len(chunks) > 1 {
		total := len(chunks)
		for i := range chunks {
			chunks[i] = fmt.Sprintf("[%d/%d] %s", i+1, total, chunks[i])
		}
	}

	return chunks
}

// runeBoundaryAtOrBefore returns the largest index <= pos that falls on a UTF-8
// rune boundary in s.
func runeBoundaryAtOrBefore(s string, pos int) int {
	if pos >= len(s) {
		return len(s)
	}
	for pos > 0 && !utf8.RuneStart(s[pos]) {
		pos--
	}
	return pos
}

// lastIndexBefore finds the last occurrence of sep in s before position maxPos.
func lastIndexBefore(s, sep string, maxPos int) int {
	if maxPos > len(s) {
		maxPos = len(s)
	}
	sub := s[:maxPos]
	lastIdx := -1
	for i := len(sub) - len(sep); i >= 0; i-- {
		if sub[i:i+len(sep)] == sep {
			lastIdx = i
			break
		}
	}
	return lastIdx
}
