package learn

import (
	"encoding/json"
	"strings"

	"github.com/cyntr-dev/cyntr/modules/agent"
)

// reflectPrompt is the after-action review instruction. The transcript is
// filled in at call time.
const reflectPrompt = `You just completed a task for a user. Review what happened and extract durable, reusable knowledge.

Task transcript:
---
%s
---

Return ONLY a JSON object of this shape:
{
  "memory": "<one or two sentences worth remembering for next time; empty if nothing>",
  "skill": {"name":"<kebab-case-name>","description":"<one line>","instructions":"<SKILL.md body: when to use it and the steps>"}
}
Rules:
- "memory" should be a concrete fact or lesson learned, not a summary of the chat.
- Only include "skill" when a genuinely reusable, multi-step procedure emerged. Omit it otherwise.
- No secrets (passwords, tokens, full card numbers).
- If nothing is worth keeping, return {"memory":""}.`

// reflection is the parsed model output.
type reflection struct {
	Memory string         `json:"memory"`
	Skill  *skillProposal `json:"skill"`
}

type skillProposal struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}

// buildTranscript renders a TurnRecord into the transcript shown to the model.
func buildTranscript(rec agent.TurnRecord) string {
	var b strings.Builder
	b.WriteString("User: ")
	b.WriteString(rec.UserMessage)
	b.WriteString("\n")
	if len(rec.ToolsUsed) > 0 {
		b.WriteString("Tools used: ")
		b.WriteString(strings.Join(rec.ToolsUsed, ", "))
		b.WriteString("\n")
	}
	b.WriteString("Assistant: ")
	b.WriteString(rec.Response)
	b.WriteString("\n")
	return b.String()
}

// parseReflection extracts the JSON object from a model response, tolerating
// code fences and surrounding prose.
func parseReflection(content string) (reflection, error) {
	s := strings.TrimSpace(content)
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end < 0 || end < start {
		return reflection{}, nil // no object -> nothing learned, not an error
	}
	var r reflection
	if err := json.Unmarshal([]byte(s[start:end+1]), &r); err != nil {
		return reflection{}, err
	}
	return r, nil
}
