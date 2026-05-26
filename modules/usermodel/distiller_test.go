package usermodel

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeProvider records prompts handed to it and returns a canned response.
// Lets tests assert what the distiller actually sent to the model.
type fakeProvider struct {
	mu             sync.Mutex
	response       string
	inputTokens    int
	outputTokens   int
	err            error
	captured       []DistillMessage
	calls          int
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) DistillChat(ctx context.Context, msgs []DistillMessage) (string, int, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.captured = make([]DistillMessage, len(msgs))
	copy(f.captured, msgs)
	if f.err != nil {
		return "", 0, 0, f.err
	}
	return f.response, f.inputTokens, f.outputTokens, nil
}

func (f *fakeProvider) lastPrompt() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.captured) == 0 {
		return ""
	}
	return f.captured[len(f.captured)-1].Content
}

func (f *fakeProvider) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureAudit collects every Emit call so tests can assert the audit log.
type captureAudit struct {
	mu      sync.Mutex
	entries []auditCall
}

type auditCall struct {
	Action, Tenant, User, Status string
	Detail                       map[string]string
}

func (c *captureAudit) Emit(action, tenant, user, status string, detail map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, auditCall{action, tenant, user, status, detail})
}

func (c *captureAudit) all() []auditCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]auditCall, len(c.entries))
	copy(out, c.entries)
	return out
}

func newTestStoreForDistill(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "usermodel.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seedActivity(t *testing.T, s *Store, tenant, user string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		if err := s.RecordActivity(tenant, user, fmt.Sprintf("session %d: user asked about topic %d", i, i)); err != nil {
			t.Fatalf("record activity: %v", err)
		}
	}
}

func TestDistillerUpsertsProfileFromLLMResponse(t *testing.T) {
	s := newTestStoreForDistill(t)
	if err := s.SetTenantDistillEnabled("acme", true); err != nil {
		t.Fatal(err)
	}
	seedActivity(t, s, "acme", "alice", 5)

	fp := &fakeProvider{
		response:     "## Background\nAlice asks about Go.\n## Recent context\nWorking on a distiller.",
		inputTokens:  100,
		outputTokens: 50,
	}
	audit := &captureAudit{}
	d, err := NewDistiller(DistillerOptions{
		Store: s, Provider: fp, Model: "claude-haiku", Audit: audit, Concurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}

	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if res.Skipped {
		t.Fatalf("expected distill to run, got skipped: %s", res.SkipReason)
	}
	if res.SessionsProcessed != 5 {
		t.Errorf("sessions_processed = %d, want 5", res.SessionsProcessed)
	}
	if res.NewSize == 0 {
		t.Errorf("new_size = 0, expected populated profile")
	}
	if res.LLMTokens != 150 {
		t.Errorf("llm_tokens = %d, want 150", res.LLMTokens)
	}

	got, err := s.Get("acme", "alice")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(got.ProfileMD, "Alice asks about Go") {
		t.Errorf("profile not persisted, got %q", got.ProfileMD)
	}

	// The prompt should contain both the current (empty) profile marker and
	// the seeded activity bullets.
	prompt := fp.lastPrompt()
	if !strings.Contains(prompt, "Recent conversations") {
		t.Errorf("prompt missing summaries block: %q", prompt)
	}
	if !strings.Contains(prompt, "session 0") {
		t.Errorf("prompt missing activity content: %q", prompt)
	}
}

func TestDistillerSkipsInsufficientSessions(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 2) // below DefaultMinSessions=3

	fp := &fakeProvider{response: "should not be called"}
	d, err := NewDistiller(DistillerOptions{Store: s, Provider: fp})
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if !res.Skipped || res.SkipReason != "insufficient_sessions" {
		t.Fatalf("expected skip insufficient_sessions, got %+v", res)
	}
	if fp.callCount() != 0 {
		t.Errorf("LLM should not be called for insufficient sessions, got %d calls", fp.callCount())
	}
}

func TestDistillerEnforces4KBCap(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 5)

	// LLM returns an oversize response. The distiller must truncate rather
	// than error — distillation is best-effort.
	huge := strings.Repeat("# overflow\nfiller filler filler filler\n", 200) // ~7 KB
	if len(huge) <= MaxSectionBytes {
		t.Fatalf("test setup: huge response is only %d bytes", len(huge))
	}
	fp := &fakeProvider{response: huge}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp})

	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if res.NewSize > MaxSectionBytes {
		t.Errorf("profile written above cap: %d bytes", res.NewSize)
	}
	got, _ := s.Get("acme", "alice")
	if len(got.ProfileMD) > MaxSectionBytes {
		t.Errorf("stored profile above cap: %d bytes", len(got.ProfileMD))
	}
}

func TestDistillerEmitsAuditEntry(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 4)

	fp := &fakeProvider{response: "## Profile\nstuff", inputTokens: 10, outputTokens: 5}
	audit := &captureAudit{}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp, Audit: audit, Model: "claude-haiku"})

	if _, err := d.DistillUser(context.Background(), "acme", "alice"); err != nil {
		t.Fatalf("distill: %v", err)
	}
	entries := audit.all()
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	last := entries[len(entries)-1]
	if last.Action != "usermodel.distill" || last.Status != "success" {
		t.Errorf("expected success audit entry, got %+v", last)
	}
	if last.Tenant != "acme" || last.User != "alice" {
		t.Errorf("audit entry tenant/user mismatch: %+v", last)
	}
	if last.Detail["model"] != "claude-haiku" {
		t.Errorf("audit detail missing model: %+v", last.Detail)
	}
}

func TestDistillerRespectsUserOptOut(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 5)
	// User opt-out via preferences_md.
	if err := s.UpsertPreferences("acme", "alice", "- prefers metric units\n- auto_distill: false\n"); err != nil {
		t.Fatal(err)
	}

	fp := &fakeProvider{response: "should not be called"}
	audit := &captureAudit{}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp, Audit: audit})

	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if !res.Skipped || res.SkipReason != "user_opt_out" {
		t.Fatalf("expected skip user_opt_out, got %+v", res)
	}
	if fp.callCount() != 0 {
		t.Errorf("LLM should not be called when user opted out, got %d calls", fp.callCount())
	}
	// Skipped distills are still audited so admins can see why.
	entries := audit.all()
	if len(entries) == 0 {
		t.Fatal("expected skip audit entry")
	}
	if entries[0].Status != "skipped" || entries[0].Detail["reason"] != "user_opt_out" {
		t.Errorf("expected skipped/user_opt_out audit, got %+v", entries[0])
	}
}

func TestDistillerRespectsTenantOptOut(t *testing.T) {
	s := newTestStoreForDistill(t)
	// Tenant NOT opted in (default).
	seedActivity(t, s, "acme", "alice", 5)

	fp := &fakeProvider{response: "should not be called"}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp})

	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if !res.Skipped || res.SkipReason != "tenant_distill_disabled" {
		t.Fatalf("expected skip tenant_distill_disabled, got %+v", res)
	}
	if fp.callCount() != 0 {
		t.Errorf("LLM should not be called when tenant opted out, got %d calls", fp.callCount())
	}
}

func TestDistillerRateLimitedOnRepeatRun(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 5)

	fp := &fakeProvider{response: "## P\nstuff"}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp, Interval: 23 * time.Hour})

	if _, err := d.DistillUser(context.Background(), "acme", "alice"); err != nil {
		t.Fatalf("first distill: %v", err)
	}
	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("second distill: %v", err)
	}
	if !res.Skipped || res.SkipReason != "rate_limited" {
		t.Fatalf("expected rate_limited on repeat run, got %+v", res)
	}

	// Force should bypass the rate limit.
	res, err = d.DistillUserForce(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("force distill: %v", err)
	}
	if res.Skipped {
		t.Errorf("force distill should not skip, got %+v", res)
	}
}

func TestDistillerTickProcessesActiveUsers(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	s.SetTenantDistillEnabled("globex", true)

	seedActivity(t, s, "acme", "alice", 5)
	seedActivity(t, s, "acme", "bob", 4)
	seedActivity(t, s, "globex", "carol", 3)
	seedActivity(t, s, "ghost", "dave", 5) // ghost tenant not opted in

	fp := &fakeProvider{response: "## P\nstuff"}
	audit := &captureAudit{}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp, Audit: audit, Concurrency: 2})

	results := d.Tick(context.Background())
	if len(results) == 0 {
		t.Fatal("expected results from tick")
	}

	// alice, bob, carol should produce real distills; dave should be skipped.
	successByUser := map[string]bool{}
	skippedByUser := map[string]string{}
	for _, r := range results {
		key := r.Tenant + "/" + r.User
		if r.Skipped {
			skippedByUser[key] = r.SkipReason
		} else if r.Error == "" {
			successByUser[key] = true
		}
	}
	if !successByUser["acme/alice"] {
		t.Errorf("acme/alice should have been distilled: %+v", results)
	}
	if !successByUser["acme/bob"] {
		t.Errorf("acme/bob should have been distilled: %+v", results)
	}
	if !successByUser["globex/carol"] {
		t.Errorf("globex/carol should have been distilled: %+v", results)
	}
	if reason := skippedByUser["ghost/dave"]; reason != "tenant_distill_disabled" {
		t.Errorf("ghost/dave should be skipped tenant_distill_disabled, got %q", reason)
	}
}

func TestDistillerColdStartEmptyProfile(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 4)

	// No existing profile row — distiller should still run and prompt should
	// indicate "(empty)" as the current profile placeholder.
	fp := &fakeProvider{response: "## P\nfirst version"}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp})

	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err != nil {
		t.Fatalf("distill: %v", err)
	}
	if res.OldSize != 0 {
		t.Errorf("expected old_size=0 for cold start, got %d", res.OldSize)
	}
	if res.NewSize == 0 {
		t.Errorf("expected new_size > 0, got 0")
	}
	if !strings.Contains(fp.lastPrompt(), "(empty)") {
		t.Errorf("cold-start prompt should mark profile as (empty): %q", fp.lastPrompt())
	}
}

func TestDistillerRejectsObviousRefusal(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 5)
	// Pre-existing profile should NOT be clobbered when the model refuses.
	if err := s.UpsertProfile("acme", "alice", "## Existing\nPrior content."); err != nil {
		t.Fatal(err)
	}

	fp := &fakeProvider{response: "I cannot help with that request."}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp})

	res, err := d.DistillUser(context.Background(), "acme", "alice")
	if err == nil {
		t.Fatal("expected error for refusal-like response")
	}
	if res.NewSize != 0 {
		t.Errorf("expected new_size=0 on rejection, got %d", res.NewSize)
	}
	got, _ := s.Get("acme", "alice")
	if !strings.Contains(got.ProfileMD, "Prior content") {
		t.Errorf("existing profile clobbered after rejection: %q", got.ProfileMD)
	}
}

func TestDistillerHandlesCodeFenceWrappedResponse(t *testing.T) {
	s := newTestStoreForDistill(t)
	s.SetTenantDistillEnabled("acme", true)
	seedActivity(t, s, "acme", "alice", 4)

	// Some providers wrap their output in ```markdown ... ```. The distiller
	// should strip the fence before storing.
	fp := &fakeProvider{response: "```markdown\n## Profile\nAlice likes terse answers.\n```"}
	d, _ := NewDistiller(DistillerOptions{Store: s, Provider: fp})

	if _, err := d.DistillUser(context.Background(), "acme", "alice"); err != nil {
		t.Fatalf("distill: %v", err)
	}
	got, _ := s.Get("acme", "alice")
	if strings.Contains(got.ProfileMD, "```") {
		t.Errorf("code fence not stripped: %q", got.ProfileMD)
	}
	if !strings.Contains(got.ProfileMD, "Alice likes terse answers") {
		t.Errorf("body lost: %q", got.ProfileMD)
	}
}

func TestStoreListActiveUsersFiltersByMinSessions(t *testing.T) {
	s := newTestStoreForDistill(t)
	seedActivity(t, s, "acme", "alice", 5)
	seedActivity(t, s, "acme", "bob", 1)
	seedActivity(t, s, "globex", "carol", 3)

	users, err := s.ListActiveUsers(0, 3)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, u := range users {
		names[u.Tenant+"/"+u.User] = true
	}
	if !names["acme/alice"] {
		t.Errorf("alice missing from active users: %+v", users)
	}
	if !names["globex/carol"] {
		t.Errorf("carol missing from active users: %+v", users)
	}
	if names["acme/bob"] {
		t.Errorf("bob should be filtered out (only 1 session): %+v", users)
	}
}

func TestStoreMarkAndReadDistilledAt(t *testing.T) {
	s := newTestStoreForDistill(t)
	if ts, _ := s.LastDistilledAt("acme", "alice"); ts != 0 {
		t.Errorf("expected 0 before mark, got %d", ts)
	}
	if err := s.MarkDistilled("acme", "alice"); err != nil {
		t.Fatal(err)
	}
	ts, err := s.LastDistilledAt("acme", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if ts == 0 {
		t.Errorf("timestamp not stamped")
	}
}

func TestParseCronExprDefault(t *testing.T) {
	c, err := parseCronExpr("0 4 * * *")
	if err != nil {
		t.Fatal(err)
	}
	t1 := time.Date(2026, 5, 22, 4, 0, 0, 0, time.UTC)
	if !c.matches(t1) {
		t.Errorf("0 4 * * * should match 04:00 UTC")
	}
	t2 := time.Date(2026, 5, 22, 5, 0, 0, 0, time.UTC)
	if c.matches(t2) {
		t.Errorf("0 4 * * * should not match 05:00 UTC")
	}
}

func TestUserOptOutParsing(t *testing.T) {
	cases := []struct {
		md   string
		want bool
	}{
		{"- auto_distill: false", true},
		{"- auto_distill: no", true},
		{"auto_distill = off", true},
		{"  - auto_distill: FALSE", true},
		{"- auto_distill: true", false},
		{"- auto_memory: false", false},
		{"", false},
		{"## prefs\n- prefers metric\n- auto_distill: false\n", true},
	}
	for _, tc := range cases {
		got := userOptedOut(tc.md)
		if got != tc.want {
			t.Errorf("userOptedOut(%q) = %v, want %v", tc.md, got, tc.want)
		}
	}
}
