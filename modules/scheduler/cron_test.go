package scheduler

import (
	"testing"
	"time"
)

func TestParseCronAllStars(t *testing.T) {
	cs, err := ParseCron("* * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Should match every minute
	now := time.Now()
	if !cs.Matches(now) {
		t.Fatal("* * * * * should match any time")
	}
}

func TestParseCronSpecificMinute(t *testing.T) {
	cs, err := ParseCron("30 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t1 := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 12, 15, 0, 0, time.UTC)
	if !cs.Matches(t1) {
		t.Fatal("should match :30")
	}
	if cs.Matches(t2) {
		t.Fatal("should not match :15")
	}
}

func TestParseCronRange(t *testing.T) {
	cs, err := ParseCron("0 9-17 * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) // noon
	t2 := time.Date(2026, 1, 1, 3, 0, 0, 0, time.UTC)  // 3am
	if !cs.Matches(t1) {
		t.Fatal("should match noon (9-17 range)")
	}
	if cs.Matches(t2) {
		t.Fatal("should not match 3am")
	}
}

func TestParseCronStep(t *testing.T) {
	cs, err := ParseCron("*/15 * * * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 12, 15, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 1, 12, 7, 0, 0, time.UTC)
	if !cs.Matches(t1) || !cs.Matches(t2) {
		t.Fatal("should match :00 and :15 with */15")
	}
	if cs.Matches(t3) {
		t.Fatal("should not match :07")
	}
}

func TestParseCronList(t *testing.T) {
	cs, err := ParseCron("0 0 1,15 * *")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	t1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	if !cs.Matches(t1) || !cs.Matches(t2) {
		t.Fatal("should match 1st and 15th")
	}
	if cs.Matches(t3) {
		t.Fatal("should not match 10th")
	}
}

func TestParseCronNext(t *testing.T) {
	cs, _ := ParseCron("0 12 * * *") // every day at noon
	from := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	next := cs.Next(from)
	if next.Hour() != 12 || next.Minute() != 0 {
		t.Fatalf("expected noon, got %v", next)
	}
}

func TestParseCronInvalid(t *testing.T) {
	_, err := ParseCron("bad expression")
	if err == nil {
		t.Fatal("expected error for invalid expression")
	}
}

func TestParseCronTooFewFields(t *testing.T) {
	_, err := ParseCron("* *")
	if err == nil {
		t.Fatal("expected error for 2 fields")
	}
}

func TestParseCronOutOfRange(t *testing.T) {
	_, err := ParseCron("60 * * * *")
	if err == nil {
		t.Fatal("expected error for minute=60")
	}
}

func TestParseCronWeekday(t *testing.T) {
	cs, err := ParseCron("0 9 * * 1-5") // weekdays only
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	mon := time.Date(2026, 3, 23, 9, 0, 0, 0, time.UTC) // Monday
	sat := time.Date(2026, 3, 21, 9, 0, 0, 0, time.UTC) // Saturday
	if !cs.Matches(mon) {
		t.Fatal("should match Monday")
	}
	if cs.Matches(sat) {
		t.Fatal("should not match Saturday")
	}
}
