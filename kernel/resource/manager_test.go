package resource

import (
	"sync"
	"testing"
)

func TestManagerTrackGoroutines(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 10)
	if err := m.Acquire("finance", ResourceGoroutines); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if m.Usage("finance", ResourceGoroutines) != 1 {
		t.Fatal("expected usage 1")
	}
	m.Release("finance", ResourceGoroutines)
	if m.Usage("finance", ResourceGoroutines) != 0 {
		t.Fatal("expected usage 0")
	}
}

func TestManagerEnforceLimit(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 2)
	m.Acquire("finance", ResourceGoroutines)
	m.Acquire("finance", ResourceGoroutines)
	err := m.Acquire("finance", ResourceGoroutines)
	if err != ErrLimitExceeded {
		t.Fatalf("expected ErrLimitExceeded, got %v", err)
	}
}

func TestManagerNoLimit(t *testing.T) {
	m := NewManager()
	for i := 0; i < 100; i++ {
		if err := m.Acquire("marketing", ResourceGoroutines); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}
	if m.Usage("marketing", ResourceGoroutines) != 100 {
		t.Fatal("expected usage 100")
	}
}

func TestManagerMultipleTenants(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 5)
	m.SetLimit("marketing", ResourceGoroutines, 3)
	for i := 0; i < 3; i++ {
		m.Acquire("finance", ResourceGoroutines)
		m.Acquire("marketing", ResourceGoroutines)
	}
	if err := m.Acquire("marketing", ResourceGoroutines); err != ErrLimitExceeded {
		t.Fatalf("expected ErrLimitExceeded for marketing, got %v", err)
	}
	if err := m.Acquire("finance", ResourceGoroutines); err != nil {
		t.Fatalf("finance should have room: %v", err)
	}
}

func TestManagerSnapshot(t *testing.T) {
	m := NewManager()
	m.SetLimit("finance", ResourceGoroutines, 10)
	m.Acquire("finance", ResourceGoroutines)
	m.Acquire("finance", ResourceGoroutines)
	snap := m.Snapshot("finance")
	entry, ok := snap[ResourceGoroutines]
	if !ok {
		t.Fatal("expected goroutines in snapshot")
	}
	if entry.Current != 2 || entry.Limit != 10 {
		t.Fatalf("expected current=2 limit=10, got current=%d limit=%d", entry.Current, entry.Limit)
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	m := NewManager()
	m.SetLimit("tenant", ResourceGoroutines, 1000)
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				if err := m.Acquire("tenant", ResourceGoroutines); err != nil {
					mu.Lock()
					errCount++
					mu.Unlock()
					return
				}
				m.Release("tenant", ResourceGoroutines)
			}
		}()
	}
	wg.Wait()

	if errCount > 0 {
		t.Fatalf("unexpected errors: %d", errCount)
	}
	if m.Usage("tenant", ResourceGoroutines) != 0 {
		t.Fatalf("expected usage 0, got %d", m.Usage("tenant", ResourceGoroutines))
	}
}
