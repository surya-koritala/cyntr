package policy

import "testing"

func TestSpendingTrackerRecordAndCheck(t *testing.T) {
	st := NewSpendingTracker()
	st.SetBudget("finance", 10.0)

	if err := st.RecordCost("finance", "bot", 3.0); err != nil {
		t.Fatalf("record: %v", err)
	}
	if st.Usage("finance") != 3.0 {
		t.Fatalf("expected 3.0, got %.2f", st.Usage("finance"))
	}
}

func TestSpendingTrackerBudgetExceeded(t *testing.T) {
	st := NewSpendingTracker()
	st.SetBudget("finance", 5.0)

	st.RecordCost("finance", "", 4.0)
	if err := st.RecordCost("finance", "", 2.0); err == nil {
		t.Fatal("expected budget exceeded")
	}
}

func TestSpendingTrackerAgentBudget(t *testing.T) {
	st := NewSpendingTracker()
	st.SetBudget("finance/bot", 2.0)
	st.SetBudget("finance", 100.0) // high tenant budget

	st.RecordCost("finance", "bot", 1.5)
	if err := st.RecordCost("finance", "bot", 1.0); err == nil {
		t.Fatal("expected agent budget exceeded")
	}
}

func TestSpendingTrackerNoBudget(t *testing.T) {
	st := NewSpendingTracker()
	// No budget set — unlimited
	if err := st.RecordCost("marketing", "bot", 1000.0); err != nil {
		t.Fatalf("should allow unlimited: %v", err)
	}
}

func TestSpendingTrackerReset(t *testing.T) {
	st := NewSpendingTracker()
	st.RecordCost("finance", "", 5.0)
	st.Reset()
	if st.Usage("finance") != 0 {
		t.Fatalf("expected 0 after reset, got %.2f", st.Usage("finance"))
	}
}

func TestSpendingTrackerIsolation(t *testing.T) {
	st := NewSpendingTracker()
	st.RecordCost("finance", "", 5.0)
	st.RecordCost("marketing", "", 3.0)
	if st.Usage("finance") != 5.0 {
		t.Fatalf("got %.2f", st.Usage("finance"))
	}
	if st.Usage("marketing") != 3.0 {
		t.Fatalf("got %.2f", st.Usage("marketing"))
	}
}
