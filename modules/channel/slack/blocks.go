package slack

import "strings"

// FormatAsBlocks converts text into Slack Block Kit section blocks.
// Detects code blocks (triple backticks) and wraps them appropriately.
func FormatAsBlocks(text string) []map[string]any {
	if text == "" {
		return nil
	}

	var blocks []map[string]any
	lines := strings.Split(text, "\n")
	var current strings.Builder
	inCode := false

	for _, line := range lines {
		if strings.HasPrefix(line, "```") {
			if inCode {
				// End code block
				blocks = append(blocks, map[string]any{
					"type": "section",
					"text": map[string]any{"type": "mrkdwn", "text": "```\n" + current.String() + "```"},
				})
				current.Reset()
				inCode = false
			} else {
				// Flush current text
				if current.Len() > 0 {
					blocks = append(blocks, map[string]any{
						"type": "section",
						"text": map[string]any{"type": "mrkdwn", "text": current.String()},
					})
					current.Reset()
				}
				inCode = true
			}
			continue
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	if current.Len() > 0 {
		if inCode {
			blocks = append(blocks, map[string]any{
				"type": "section",
				"text": map[string]any{"type": "mrkdwn", "text": "```\n" + current.String() + "```"},
			})
		} else {
			blocks = append(blocks, map[string]any{
				"type": "section",
				"text": map[string]any{"type": "mrkdwn", "text": current.String()},
			})
		}
	}

	return blocks
}
