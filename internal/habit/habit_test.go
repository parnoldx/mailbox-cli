package habit

import (
	"testing"
	"time"
)

func TestParseDays(t *testing.T) {
	got, err := ParseDays("")
	if err != nil || len(got) != 7 {
		t.Fatalf("no days means every day, got %v (%v)", got, err)
	}
	got, err = ParseDays("Fri, montag,1")
	if err != nil {
		t.Fatal(err)
	}
	// Deduplicated and in week order, whatever order they were typed in.
	if len(got) != 2 || got[0] != "mon" || got[1] != "fri" {
		t.Fatalf("days = %v", got)
	}
	if _, err := ParseDays("caturday"); err == nil {
		t.Fatal("an unknown day is a mistake worth naming")
	}
}

func TestCompletingADayIsIdempotent(t *testing.T) {
	h := Habit{Name: "Meditation"}
	h.Complete("2026-08-29")
	h.Complete("2026-08-29")
	if len(h.Done) != 1 {
		t.Fatalf("done = %v — a habit is a fact about a day, not a counter", h.Done)
	}
	h.Uncomplete("2026-08-29")
	if h.DoneOn("2026-08-29") {
		t.Fatal("it was taken back")
	}
}

func TestStreakCountsOnlyTheDaysItWasDue(t *testing.T) {
	// Due on weekdays only, done every weekday for a fortnight. The weekend
	// does not break it, because it was never due then.
	h := Habit{Name: "Sport", Days: []string{"mon", "tue", "wed", "thu", "fri"}}
	on := time.Date(2026, 8, 28, 12, 0, 0, 0, time.Local) // a Friday
	for i := 0; i < 14; i++ {
		day := on.AddDate(0, 0, -i)
		if h.Due(DayOf(day)) {
			h.Complete(day.Format("2006-01-02"))
		}
	}
	if got := h.Streak(on); got != 10 {
		t.Fatalf("streak = %d, want 10 weekdays", got)
	}

	// A weekday missed ends it there.
	h.Uncomplete(on.AddDate(0, 0, -7).Format("2006-01-02"))
	if got := h.Streak(on); got != 5 {
		t.Fatalf("streak after a missed day = %d, want 5", got)
	}
}

func TestTodayNotDoneYetIsNotABrokenStreak(t *testing.T) {
	h := Habit{Name: "Lesen"}
	on := time.Now()
	for i := 1; i <= 3; i++ {
		h.Complete(on.AddDate(0, 0, -i).Format("2006-01-02"))
	}
	if got := h.Streak(on); got != 3 {
		t.Fatalf("streak = %d: a day that is not over yet has not been missed", got)
	}
}

func TestFindTakesAnIdOrANameAndRefusesAGuess(t *testing.T) {
	bag := Bag{Habits: []Habit{
		{ID: "aaa111", Name: "Meditation"},
		{ID: "bbb222", Name: "Meditation abends"},
		{ID: "ccc333", Name: "Sport"},
	}}
	if h, err := bag.Find("Sport"); err != nil || h.ID != "ccc333" {
		t.Fatalf("by name: %+v (%v)", h, err)
	}
	if h, err := bag.Find("ccc"); err != nil || h.ID != "ccc333" {
		t.Fatalf("by id prefix: %+v (%v)", h, err)
	}
	if h, err := bag.Find("Meditation"); err != nil || h.ID != "aaa111" {
		t.Fatalf("an exact name beats a longer one that contains it: %+v (%v)", h, err)
	}
	// Two could match, so neither is ticked off.
	if _, err := bag.Find("medit"); err == nil {
		t.Fatal("an ambiguous name must be refused rather than guessed")
	}
	if _, err := bag.Find("nothing"); err == nil {
		t.Fatal("a habit that does not exist is an error")
	}
}

func TestTheRecordSurvivesARoundTrip(t *testing.T) {
	bag := Bag{Habits: []Habit{{ID: "a", Name: "Meditation", Days: []string{"mon"}, Done: []string{"2026-08-29"}, Color: "#3355ff"}}}
	raw, err := Encode(bag)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Habits) != 1 || back.Habits[0].Name != "Meditation" || !back.Habits[0].DoneOn("2026-08-29") {
		t.Fatalf("bag = %+v", back)
	}
	// An empty description is an empty bag: that is what a calendar with no
	// habits in it yet looks like.
	if empty, err := Decode("  "); err != nil || len(empty.Habits) != 0 {
		t.Fatalf("empty = %+v (%v)", empty, err)
	}
	if _, err := Decode("{not json"); err == nil {
		t.Fatal("a corrupt record should say so rather than read as empty")
	}
}
