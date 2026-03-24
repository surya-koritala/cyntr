package agent

import (
	"path/filepath"
	"testing"
	"time"
)

func TestUsageStoreRecord(t *testing.T) {
	store, err := NewUsageStore(filepath.Join(t.TempDir(), "usage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.Record(UsageRecord{
		Timestamp: time.Now(), Tenant: "demo", Agent: "bot",
		Provider: "claude", InputTokens: 100, OutputTokens: 50,
		TotalTokens: 150, DurationMs: 500,
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
}

func TestUsageStoreQuery(t *testing.T) {
	store, _ := NewUsageStore(filepath.Join(t.TempDir(), "usage.db"))
	defer store.Close()

	store.Record(UsageRecord{
		Timestamp: time.Now(), Tenant: "demo", Agent: "bot",
		Provider: "claude", TotalTokens: 100,
	})
	store.Record(UsageRecord{
		Timestamp: time.Now(), Tenant: "demo", Agent: "bot2",
		Provider: "gpt", TotalTokens: 200,
	})

	records, err := store.Query("demo", "", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
}

func TestUsageStoreQueryByAgent(t *testing.T) {
	store, _ := NewUsageStore(filepath.Join(t.TempDir(), "usage.db"))
	defer store.Close()

	store.Record(UsageRecord{Timestamp: time.Now(), Tenant: "demo", Agent: "bot1", Provider: "claude", TotalTokens: 100})
	store.Record(UsageRecord{Timestamp: time.Now(), Tenant: "demo", Agent: "bot2", Provider: "gpt", TotalTokens: 200})

	records, _ := store.Query("demo", "bot1", time.Time{}, time.Time{})
	if len(records) != 1 {
		t.Fatalf("expected 1 record for bot1, got %d", len(records))
	}
}

func TestUsageStoreSummarize(t *testing.T) {
	store, _ := NewUsageStore(filepath.Join(t.TempDir(), "usage.db"))
	defer store.Close()

	store.Record(UsageRecord{Timestamp: time.Now(), Tenant: "demo", Agent: "bot", Provider: "claude", TotalTokens: 100, DurationMs: 200})
	store.Record(UsageRecord{Timestamp: time.Now(), Tenant: "demo", Agent: "bot", Provider: "claude", TotalTokens: 150, DurationMs: 300})

	summaries, err := store.Summarize("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if summaries[0].TotalCalls != 2 {
		t.Fatalf("expected 2 calls, got %d", summaries[0].TotalCalls)
	}
	if summaries[0].TotalTokens != 250 {
		t.Fatalf("expected 250 tokens, got %d", summaries[0].TotalTokens)
	}
}

func TestUsageStoreEmpty(t *testing.T) {
	store, _ := NewUsageStore(filepath.Join(t.TempDir(), "usage.db"))
	defer store.Close()

	records, _ := store.Query("", "", time.Time{}, time.Time{})
	if len(records) != 0 {
		t.Fatalf("expected 0, got %d", len(records))
	}
}
