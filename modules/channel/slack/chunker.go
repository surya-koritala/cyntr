package slack

import "fmt"

// chunkMessage splits a message into chunks that fit within maxLen.
// Splits on paragraph boundaries (\n\n), then line boundaries (\n),
// then word boundaries (space), with hard cut as last resort.
func chunkMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	remaining := text

	for len(remaining) > 0 {
		if len(remaining) <= maxLen {
			chunks = append(chunks, remaining)
			break
		}

		// Find best split point
		cutAt := maxLen
		// Try paragraph boundary
		if idx := lastIndexBefore(remaining, "\n\n", maxLen); idx > 0 {
			cutAt = idx + 2
		} else if idx := lastIndexBefore(remaining, "\n", maxLen); idx > 0 {
			cutAt = idx + 1
		} else if idx := lastIndexBefore(remaining, " ", maxLen); idx > 0 {
			cutAt = idx + 1
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
