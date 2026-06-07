package usermodel

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// DefaultMinSessions is the minimum activity rows required before we'll
// spend an LLM call distilling. Below this we leave the profile alone.
const DefaultMinSessions = 3

// DefaultMaxSessions is how many recent activity rows we feed into the
// distillation prompt by default.
const DefaultMaxSessions = 10

// DefaultDistillModel is the provider name the distiller calls when its
// caller hasn't configured one. We pick a small/cheap model intentionally
// — distillation is best-effort and runs once per active user per day.
const DefaultDistillModel = "claude-haiku"

// DefaultDistillInterval is the minimum spacing between automatic distills
// for a single user. Manual triggers ignore this.
const DefaultDistillInterval = 23 * time.Hour

// distillSystemPrompt is the instruction handed to the model. The variable
// section ({{profile}}, {{summaries}}) is filled at call time. We keep it
// terse so the prompt itself doesn't burn budget.
const distillSystemPrompt = `You're maintaining a living profile of a user based on recent conversations.

Current profile (markdown):
---
%s
---

Recent conversations (summaries):
---
%s
---

Update the profile to incorporate what you learned. Rules:
- Keep the markdown well-organized (sections like ## Background, ## Interests, ## Recent context).
- Don't restate everything — keep what's still relevant, add what's new, remove what's stale.
- Maximum 4 KB. Be terse.
- Don't include sensitive data (passwords, full credit cards, SSNs).
- Don't fabricate. If you're uncertain, leave it out.
Output the updated profile markdown only. No prose around it.`

// DistillMessage is a single chat turn passed to the LLM. We mirror the
// agent package's Message shape but keep our own type so the distiller is
// importable from anywhere without dragging the agent package along.
type DistillMessage struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// LLMProvider is the minimum surface the distiller needs from a chat
// model — single-turn completion with input/output token reporting. The
// agent.ModelProvider implementations satisfy this via a thin adapter so we
// don't take a hard dependency on the agent package.
type LLMProvider interface {
	Name() string
	DistillChat(ctx context.Context, messages []DistillMessage) (content string, inputTokens, outputTokens int, err error)
}

// AuditEmitter is the slice of the audit module the distiller cares about.
// Defined as an interface so tests can capture entries without bringing the
// real audit writer up.
type AuditEmitter interface {
	Emit(action, tenant, user, status string, detail map[string]string)
}

// noopAudit is used when the caller doesn't wire an emitter — distill
// operations still run, but they're not auditable. Acceptable for tests
// and one-off CLI use; in production main.go always installs the real one.
type noopAudit struct{}

func (noopAudit) Emit(action, tenant, user, status string, detail map[string]string) {}

// Distiller turns a user's recent chat activity into an updated profile_md
// using an LLM. It's intentionally state-light — the only persistent thing
// it owns is the in-process concurrency semaphore that caps fan-out across
// scheduled ticks. All durable state lives on Store.
type Distiller struct {
	store    *Store
	provider LLMProvider
	model    string
	audit    AuditEmitter

	// concurrency cap (global, in-process). Distillation is LLM-expensive
	// and we don't want a tick that fires for hundreds of users to overwhelm
	// the provider. nil semaphore means uncapped.
	sem chan struct{}

	// interval enforces "at most one distill per user per day" — manual
	// IPC/HTTP triggers can bypass via DistillUserForce.
	interval time.Duration

	mu     sync.Mutex
	logger func(msg string, kv map[string]any) // optional

	// factsEnabled turns on the dialectic facts pass (A6) alongside the
	// narrative profile distill. Off by default.
	factsEnabled bool
}

// DistillerOptions configures a Distiller. Zero values get sensible
// defaults; the only required wiring is Store and Provider.
type DistillerOptions struct {
	Store       *Store
	Provider    LLMProvider
	Model       string         // provider-side model identifier; passed through in audit detail
	Audit       AuditEmitter   // optional; defaults to no-op
	Concurrency int            // max simultaneous distills (0 -> unlimited; recommended: 5)
	Interval    time.Duration  // min spacing per user (0 -> 23h)
	Logger      func(string, map[string]any)
	EnableFacts bool           // run the dialectic facts pass (A6) alongside the profile distill
}

// NewDistiller constructs a Distiller. Returns an error only for hard
// misconfiguration (missing store or provider) so the caller can decide
// whether to skip registering the scheduler tick.
func NewDistiller(opts DistillerOptions) (*Distiller, error) {
	if opts.Store == nil {
		return nil, errors.New("usermodel: distiller requires a Store")
	}
	if opts.Provider == nil {
		return nil, errors.New("usermodel: distiller requires an LLMProvider")
	}
	d := &Distiller{
		store:    opts.Store,
		provider: opts.Provider,
		model:    opts.Model,
		audit:    opts.Audit,
		interval: opts.Interval,
		logger:   opts.Logger,

		factsEnabled: opts.EnableFacts,
	}
	if d.audit == nil {
		d.audit = noopAudit{}
	}
	if d.interval == 0 {
		d.interval = DefaultDistillInterval
	}
	if opts.Concurrency > 0 {
		d.sem = make(chan struct{}, opts.Concurrency)
	}
	return d, nil
}

// DistillUser runs the distillation pipeline for one (tenant, user),
// respecting all opt-out and rate-limit gates. Returns the result with
// Skipped set when the operation was a no-op (insufficient sessions, tenant
// opt-out, user opt-out, recently distilled).
func (d *Distiller) DistillUser(ctx context.Context, tenant, user string) (*DistillResult, error) {
	return d.distillUser(ctx, tenant, user, false)
}

// DistillUserForce bypasses the per-user rate-limit. Tenant + user opt-out
// gates still apply — there is no admin override for those.
func (d *Distiller) DistillUserForce(ctx context.Context, tenant, user string) (*DistillResult, error) {
	return d.distillUser(ctx, tenant, user, true)
}

func (d *Distiller) distillUser(ctx context.Context, tenant, user string, force bool) (*DistillResult, error) {
	res := &DistillResult{Tenant: tenant, User: user}

	// Tenant opt-in gate. Off by default — explicit row in
	// tenant_distill_config required.
	enabled, err := d.store.TenantDistillEnabled(tenant)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	if !enabled {
		res.Skipped = true
		res.SkipReason = "tenant_distill_disabled"
		d.audit.Emit("usermodel.distill", tenant, user, "skipped", map[string]string{"reason": res.SkipReason})
		return res, nil
	}

	// Per-user rate limit. Honored for scheduler-driven ticks; manual
	// triggers (force=true) skip this check.
	if !force {
		lastTs, _ := d.store.LastDistilledAt(tenant, user)
		if lastTs > 0 {
			last := time.Unix(lastTs, 0)
			if time.Since(last) < d.interval {
				res.Skipped = true
				res.SkipReason = "rate_limited"
				return res, nil
			}
		}
	}

	// User-level opt-out via preferences_md. Looked up on every distill so
	// a freshly-set "auto_distill: false" takes effect immediately without
	// waiting for the next tenant reconfiguration.
	profile, err := d.store.Get(tenant, user)
	if err != nil && err != ErrNotFound {
		res.Error = err.Error()
		return res, err
	}
	if userOptedOut(profile.PreferencesMD) {
		res.Skipped = true
		res.SkipReason = "user_opt_out"
		d.audit.Emit("usermodel.distill", tenant, user, "skipped", map[string]string{"reason": res.SkipReason})
		return res, nil
	}

	// Pull recent activity. <3 sessions is "not enough signal to update"
	// per the spec — return without spending a token.
	activity, err := d.store.RecentActivity(tenant, user, DefaultMaxSessions)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	if len(activity) < DefaultMinSessions {
		res.Skipped = true
		res.SkipReason = "insufficient_sessions"
		res.SessionsProcessed = len(activity)
		return res, nil
	}

	// Bound LLM concurrency. We block here rather than queue — the
	// scheduler tick is the queue.
	if d.sem != nil {
		select {
		case d.sem <- struct{}{}:
			defer func() { <-d.sem }()
		case <-ctx.Done():
			res.Error = ctx.Err().Error()
			return res, ctx.Err()
		}
	}

	// Build prompt. We scrub the activity summaries lightly so secrets
	// leaked into a recent chat don't end up baked into the profile.
	summaries := buildSummaryBlock(activity)
	prompt := fmt.Sprintf(distillSystemPrompt, defaultIfEmpty(profile.ProfileMD, "(empty)"), summaries)

	res.OldSize = len(profile.ProfileMD)
	res.SessionsProcessed = len(activity)

	// Single-shot completion. We don't iterate — the prompt either
	// produces a valid profile or we leave the existing one alone.
	ctxLLM, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	content, inTok, outTok, err := d.provider.DistillChat(ctxLLM, []DistillMessage{
		{Role: "user", Content: prompt},
	})
	res.LLMTokens = inTok + outTok
	if err != nil {
		res.Error = err.Error()
		d.audit.Emit("usermodel.distill", tenant, user, "error", map[string]string{
			"error": err.Error(), "model": d.model,
		})
		return res, err
	}

	// Validate + clamp the response. We don't want a runaway model
	// blowing past the 4 KB cap, and we want to bail if the model gave us
	// an obvious refusal rather than a profile.
	cleaned := strings.TrimSpace(stripCodeFence(content))
	if cleaned == "" {
		res.Error = "empty response"
		d.audit.Emit("usermodel.distill", tenant, user, "error", map[string]string{"error": "empty response"})
		return res, errors.New("usermodel: distiller got empty response")
	}
	if !looksLikeMarkdown(cleaned) {
		res.Error = "response did not look like markdown"
		d.audit.Emit("usermodel.distill", tenant, user, "error", map[string]string{"error": "non_markdown_response"})
		return res, errors.New("usermodel: distiller response did not look like markdown")
	}
	if len(cleaned) > MaxSectionBytes {
		// Truncate to cap rather than erroring. Distillation is best-effort
		// and we prefer "slightly stale but stored" over "silent failure".
		cleaned = truncateAtRuneBoundary(cleaned, MaxSectionBytes)
	}

	if err := d.store.UpsertProfile(tenant, user, cleaned); err != nil {
		res.Error = err.Error()
		d.audit.Emit("usermodel.distill", tenant, user, "error", map[string]string{"error": err.Error()})
		return res, err
	}
	// Stamp last-distilled even when we fall back to the original profile —
	// the goal is "don't re-spend tokens on this user for $interval", not
	// "stamp only on content change".
	d.store.MarkDistilled(tenant, user)

	// Dialectic facts pass (A6): maintain the structured fact model alongside
	// the narrative profile. Best-effort — a facts failure must never fail
	// the profile distill that already succeeded.
	if d.factsEnabled {
		if _, ferr := d.DistillFacts(ctx, tenant, user); ferr != nil && d.logger != nil {
			d.logger("usermodel fact distill failed", map[string]any{"tenant": tenant, "user": user, "error": ferr.Error()})
		}
	}

	res.NewSize = len(cleaned)
	d.audit.Emit("usermodel.distill", tenant, user, "success", map[string]string{
		"old_size":     fmt.Sprintf("%d", res.OldSize),
		"new_size":     fmt.Sprintf("%d", res.NewSize),
		"sessions":     fmt.Sprintf("%d", res.SessionsProcessed),
		"llm_tokens":   fmt.Sprintf("%d", res.LLMTokens),
		"model":        d.model,
	})
	if d.logger != nil {
		d.logger("user profile distilled", map[string]any{
			"tenant": tenant, "user": user,
			"old_size": res.OldSize, "new_size": res.NewSize,
			"sessions": res.SessionsProcessed, "tokens": res.LLMTokens,
		})
	}
	return res, nil
}

// Tick runs one round of the scheduled distill. It finds users with at least
// DefaultMinSessions of activity since their last distill and runs
// DistillUser for each. Bounded by the configured Concurrency. Returns the
// list of results (including skipped ones) so the scheduler can log a
// summary.
func (d *Distiller) Tick(ctx context.Context) []*DistillResult {
	// Look back over a generous window — we'd rather pick up a user who
	// chatted lightly than miss them. The per-user opt-in/rate-limit gates
	// inside DistillUser handle the actual filtering.
	since := time.Now().Add(-30 * 24 * time.Hour).Unix()
	users, err := d.store.ListActiveUsers(since, DefaultMinSessions)
	if err != nil {
		if d.logger != nil {
			d.logger("distiller tick: list_active_users failed", map[string]any{"error": err.Error()})
		}
		return nil
	}

	results := make([]*DistillResult, 0, len(users))
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Bound the fan-out. Without a cap, a tick covering thousands of active
	// users would spawn one goroutine each, all racing into the store (and,
	// past the per-user gates, the LLM) at once — a self-inflicted DoS. The
	// LLM-level semaphore (d.sem) only guards the model call, not the
	// pre-flight store reads, so we add a worker-pool bound here too.
	workers := tickWorkerCap(cap(d.sem))
	jobs := make(chan TenantUser)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tu := range jobs {
				r, _ := d.DistillUser(ctx, tu.Tenant, tu.User)
				if r == nil {
					continue
				}
				mu.Lock()
				results = append(results, r)
				mu.Unlock()
			}
		}()
	}

	for _, tu := range users {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results
		case jobs <- tu:
		}
	}
	close(jobs)
	wg.Wait()
	return results
}

// defaultTickWorkers caps fan-out concurrency for a scheduled tick when no
// LLM-level concurrency was configured.
const defaultTickWorkers = 5

// tickWorkerCap derives the fan-out worker count. It tracks the configured
// LLM concurrency when set (cap of d.sem) so we never start more workers than
// can make progress, and falls back to a small fixed bound otherwise.
func tickWorkerCap(semCap int) int {
	if semCap > 0 {
		return semCap
	}
	return defaultTickWorkers
}

// ----- helpers -----

// userOptedOut returns true when the preferences_md contains a top-level
// "auto_distill: false" YAML-ish line. We parse loosely on purpose — the
// preferences markdown is human-edited and could carry any whitespace, but
// we don't want to pull in a YAML parser just for this one flag.
func userOptedOut(prefsMD string) bool {
	if prefsMD == "" {
		return false
	}
	for _, raw := range strings.Split(prefsMD, "\n") {
		line := strings.TrimSpace(raw)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		// Tolerate both "auto_distill: false" and "auto_distill = false"
		// (the latter is what older Hermes-style configs used).
		for _, sep := range []string{":", "="} {
			if i := strings.Index(line, sep); i > 0 {
				key := strings.ToLower(strings.TrimSpace(line[:i]))
				val := strings.ToLower(strings.TrimSpace(line[i+1:]))
				if key == "auto_distill" && (val == "false" || val == "no" || val == "off" || val == "0") {
					return true
				}
			}
		}
	}
	return false
}

// buildSummaryBlock turns recent activity rows into the bullet list that
// goes into the prompt. Newest activity comes from the store first, so we
// reverse here to keep the timeline reading top-down old → new.
func buildSummaryBlock(activity []ActivitySummary) string {
	if len(activity) == 0 {
		return "(no recent activity)"
	}
	var b strings.Builder
	for i := len(activity) - 1; i >= 0; i-- {
		a := activity[i]
		b.WriteString("- [")
		b.WriteString(a.CreatedAt.UTC().Format("2006-01-02"))
		b.WriteString("] ")
		// Strip newlines so each summary stays a single bullet.
		s := strings.ReplaceAll(a.Summary, "\n", " ")
		s = strings.TrimSpace(s)
		// Redact secrets/PII before the summary is baked into the durable
		// profile. A secret leaked into a recent chat must not be persisted
		// (and re-fed to the model on every future distill).
		s = redactSecrets(s)
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String()
}

// secretPatterns matches common secret/credential and high-value PII shapes.
// Self-contained on purpose: the distiller deliberately avoids importing the
// agent package (which would create an import cycle).
var secretPatterns = []*regexp.Regexp{
	// Bearer / Authorization tokens.
	regexp.MustCompile(`(?i)\b(?:bearer|authorization)\s*[:=]?\s*[A-Za-z0-9._\-]{12,}`),
	// "key/token/secret/password = value" style assignments.
	regexp.MustCompile(`(?i)\b(?:api[_-]?key|secret|password|passwd|token|access[_-]?key|client[_-]?secret|private[_-]?key)\b\s*[:=]\s*\S+`),
	// AWS access key IDs.
	regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`),
	// Common provider key prefixes (OpenAI/Anthropic/GitHub/Slack/Google).
	regexp.MustCompile(`\b(?:sk-[A-Za-z0-9]{20,}|sk-ant-[A-Za-z0-9_\-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|xox[baprs]-[A-Za-z0-9-]{10,}|AIza[0-9A-Za-z_\-]{30,})\b`),
	// JWTs (three base64url segments).
	regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\b`),
	// PEM private key blocks (collapsed to one line above, so match the header).
	regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----`),
	// US SSNs.
	regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
	// Credit-card-like 16-digit groups.
	regexp.MustCompile(`\b(?:\d[ -]?){13,16}\b`),
}

// redactSecrets replaces detected secrets/credentials and high-value PII with
// a [REDACTED] marker so they never get persisted into the user profile.
func redactSecrets(s string) string {
	for _, pat := range secretPatterns {
		s = pat.ReplaceAllString(s, "[REDACTED]")
	}
	return s
}

// stripCodeFence pulls the body out of a ```markdown ... ``` block if the
// model wrapped its response in one. Idempotent for non-fenced strings.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence (with or without a language tag).
	if i := strings.Index(s, "\n"); i >= 0 {
		s = s[i+1:]
	}
	// Drop the trailing fence.
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// looksLikeMarkdown is a heuristic guard against catastrophic responses
// (e.g. "I cannot help with that.") We accept any non-trivial string and
// reject only on obvious refusal patterns. Markdown shape isn't required —
// a profile that's just bullets is perfectly valid.
func looksLikeMarkdown(s string) bool {
	if len(strings.TrimSpace(s)) == 0 {
		return false
	}
	// Quick reject for obvious model-side refusals.
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, phrase := range []string{
		"i cannot help",
		"i can't help",
		"i'm not able to",
		"as an ai",
		"i apologize",
		"i cannot",
		"i can't",
	} {
		if strings.HasPrefix(lower, phrase) {
			return false
		}
	}
	return true
}

// truncateAtRuneBoundary cuts s to at most n bytes, never splitting a UTF-8
// rune. We back up to the nearest line break when one exists in the last
// 200 bytes so the truncated profile still ends cleanly.
func truncateAtRuneBoundary(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && (s[cut]&0xC0) == 0x80 {
		cut--
	}
	// Prefer the previous newline if it's reasonably close.
	if nl := strings.LastIndex(s[:cut], "\n"); nl >= cut-200 && nl > cut/2 {
		cut = nl
	}
	return s[:cut]
}

func defaultIfEmpty(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}
