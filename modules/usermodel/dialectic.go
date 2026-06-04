package usermodel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MaxActiveFacts bounds how many active facts a single user accumulates.
// Beyond this the lowest-confidence facts are retired, so the model deepens
// without growing without bound.
const MaxActiveFacts = 40

// factSystemPrompt asks the model to return a JSON array of fact deltas
// rather than overwrite anything — the "dialectic" update.
const factSystemPrompt = `You maintain a structured model of a user as a list of discrete facts, each with a confidence from 0.0 to 1.0.

Current facts:
%s

Recent activity (oldest to newest):
%s

Propose updates as a JSON array of operations. Each operation is one of:
  {"op":"add","fact":"<new claim about the user>","confidence":0.0-1.0}
  {"op":"revise","id":<existing fact id>,"fact":"<updated text, optional>","confidence":0.0-1.0}
  {"op":"retire","id":<existing fact id>}
Rules:
- Only add facts clearly supported by the activity. Do not fabricate or guess.
- Revise (raise or lower confidence) when activity reinforces or contradicts a fact.
- Retire facts that are clearly stale or wrong.
- Do not include secrets (passwords, full card numbers, SSNs, API keys).
- Return ONLY the JSON array. If nothing should change, return [].`

// FactResult summarizes a dialectic pass.
type FactResult struct {
	Tenant     string `json:"tenant"`
	User       string `json:"user"`
	Added      int    `json:"added"`
	Revised    int    `json:"revised"`
	Retired    int    `json:"retired"`
	LLMTokens  int    `json:"llm_tokens"`
	Skipped    bool   `json:"skipped"`
	SkipReason string `json:"skip_reason,omitempty"`
	Error      string `json:"error,omitempty"`
}

type factDelta struct {
	Op         string  `json:"op"`
	ID         int64   `json:"id"`
	Fact       string  `json:"fact"`
	Confidence float64 `json:"confidence"`
}

// DistillFacts runs one dialectic pass for (tenant, user): it asks the model
// to propose add/revise/retire deltas against the current facts given recent
// activity, applies them, audits each write, and caps total active facts.
// Gated by the same tenant opt-in and minimum-activity thresholds as the
// profile distiller.
func (d *Distiller) DistillFacts(ctx context.Context, tenant, user string) (*FactResult, error) {
	res := &FactResult{Tenant: tenant, User: user}

	enabled, err := d.store.TenantDistillEnabled(tenant)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	if !enabled {
		res.Skipped, res.SkipReason = true, "tenant_distill_disabled"
		return res, nil
	}

	activity, err := d.store.RecentActivity(tenant, user, DefaultMaxSessions)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	if len(activity) < DefaultMinSessions {
		res.Skipped, res.SkipReason = true, "insufficient_sessions"
		return res, nil
	}

	current, err := d.store.ActiveFacts(tenant, user)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}

	if d.sem != nil {
		select {
		case d.sem <- struct{}{}:
			defer func() { <-d.sem }()
		case <-ctx.Done():
			res.Error = ctx.Err().Error()
			return res, ctx.Err()
		}
	}

	prompt := fmt.Sprintf(factSystemPrompt, renderFacts(current), buildSummaryBlock(activity))

	ctxLLM, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	content, inTok, outTok, err := d.provider.DistillChat(ctxLLM, []DistillMessage{{Role: "user", Content: prompt}})
	res.LLMTokens = inTok + outTok
	if err != nil {
		res.Error = err.Error()
		d.audit.Emit("usermodel.fact_distill", tenant, user, "error", map[string]string{"error": err.Error(), "model": d.model})
		return res, err
	}

	deltas, err := parseFactDeltas(content)
	if err != nil {
		res.Error = err.Error()
		d.audit.Emit("usermodel.fact_distill", tenant, user, "error", map[string]string{"error": "parse: " + err.Error()})
		return res, err
	}

	session := ""
	if len(activity) > 0 {
		session = activity[0].CreatedAt.UTC().Format("2006-01-02")
	}

	for _, dl := range deltas {
		switch strings.ToLower(dl.Op) {
		case "add":
			text := strings.TrimSpace(dl.Fact)
			if text == "" {
				continue
			}
			if _, err := d.store.AddFact(tenant, user, text, dl.Confidence, session); err != nil {
				continue
			}
			res.Added++
			d.audit.Emit("usermodel.fact_add", tenant, user, "success", map[string]string{
				"fact": truncateAtRuneBoundary(text, 120), "confidence": fmt.Sprintf("%.2f", clampConfidence(dl.Confidence)),
			})
		case "revise":
			if dl.ID == 0 {
				continue
			}
			if err := d.store.ReviseFact(tenant, user, dl.ID, strings.TrimSpace(dl.Fact), dl.Confidence); err != nil {
				continue // stale/foreign id — skip silently
			}
			res.Revised++
			d.audit.Emit("usermodel.fact_revise", tenant, user, "success", map[string]string{
				"id": fmt.Sprintf("%d", dl.ID), "confidence": fmt.Sprintf("%.2f", clampConfidence(dl.Confidence)),
			})
		case "retire":
			if dl.ID == 0 {
				continue
			}
			if err := d.store.RetireFact(tenant, user, dl.ID); err != nil {
				continue
			}
			res.Retired++
			d.audit.Emit("usermodel.fact_retire", tenant, user, "success", map[string]string{"id": fmt.Sprintf("%d", dl.ID)})
		}
	}

	// Bound growth: retire the weakest facts beyond the cap.
	if capped, err := d.store.RetireLowestConfidence(tenant, user, MaxActiveFacts); err == nil && capped > 0 {
		res.Retired += capped
		d.audit.Emit("usermodel.fact_cap", tenant, user, "success", map[string]string{"retired": fmt.Sprintf("%d", capped)})
	}

	if d.logger != nil {
		d.logger("user facts distilled", map[string]any{
			"tenant": tenant, "user": user,
			"added": res.Added, "revised": res.Revised, "retired": res.Retired, "tokens": res.LLMTokens,
		})
	}
	return res, nil
}

// renderFacts formats the current facts for the prompt as `[id] (conf) text`.
func renderFacts(facts []Fact) string {
	if len(facts) == 0 {
		return "(none yet)"
	}
	var b strings.Builder
	for _, f := range facts {
		fmt.Fprintf(&b, "[%d] (%.2f) %s\n", f.ID, f.Confidence, strings.ReplaceAll(f.Text, "\n", " "))
	}
	return b.String()
}

// parseFactDeltas extracts the JSON array of deltas from a model response,
// tolerating code fences and surrounding prose.
func parseFactDeltas(content string) ([]factDelta, error) {
	s := stripCodeFence(content)
	start := strings.Index(s, "[")
	end := strings.LastIndex(s, "]")
	if start < 0 || end < 0 || end < start {
		// No array at all: treat as "no changes" rather than an error.
		return nil, nil
	}
	var deltas []factDelta
	if err := json.Unmarshal([]byte(s[start:end+1]), &deltas); err != nil {
		return nil, err
	}
	return deltas, nil
}
