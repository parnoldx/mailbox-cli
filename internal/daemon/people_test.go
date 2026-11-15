// `people search`: the org directory behind the invite picker. Which rows a
// typed word finds, in what order, and how often Graph is asked.
package daemon

import (
	"context"
	"errors"
	"testing"
	"time"
)

func directoryDaemon(t *testing.T, rows []Colleague) (*Daemon, *int) {
	t.Helper()
	d, _, _ := seedTasks(t)
	calls := 0
	d.Primary.Directory = func(context.Context) ([]Colleague, error) {
		calls++
		return rows, nil
	}
	return d, &calls
}

func peopleRows(resp Response) []Colleague {
	out := make([]Colleague, 0)
	for _, h := range resp.Data.([]colleagueHit) {
		out = append(out, Colleague{Name: h.Name, Email: h.Email})
	}
	return out
}

// Every typed word has to appear, in the name or the address; a name that
// starts with the box sorts first, the rest follows alphabetically.
func TestPeopleSearchMatchesNameAndAddress(t *testing.T) {
	d, _ := directoryDaemon(t, []Colleague{
		{Name: "Ana Silva", Email: "ana.silva@example.com"},
		{Name: "Cara Doe", Email: "cara.doe@example.com"},
		{Name: "Erika Mustermann", Email: "erika.mustermann@example.com"},
		{Name: "Jamie Roe", Email: "jamie.roe@example.com"},
	})
	got := peopleRows(mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "a"}))
	want := []string{"Ana Silva", "Cara Doe", "Erika Mustermann", "Jamie Roe"}
	if len(got) != len(want) || got[0].Name != want[0] || got[1].Name != want[1] {
		t.Fatalf("sch found %+v, want %v", got, want)
	}

	// A name that starts with the word sorts above one where it only appears
	// inside: "a" starts Ana Silva, and only turns up inside the others.
	got = peopleRows(mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "a"}))
	if len(got) != 4 || got[0].Name != "Ana Silva" {
		t.Errorf("a found %+v", got)
	}

	// Two words must both match: "jamie roe" is one person, "jamie silva" nobody.
	got = peopleRows(mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "jamie roe"}))
	if len(got) != 1 || got[0].Name != "Jamie Roe" {
		t.Errorf("jamie roe found %+v", got)
	}
	if got = peopleRows(mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "jamie silva"})); len(got) != 0 {
		t.Errorf("two words from two people matched: %+v", got)
	}

	// An address finds its owner too, and the limit cuts the answer.
	got = peopleRows(mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "example.com", "limit": float64(2)}))
	if len(got) != 2 {
		t.Errorf("limit 2 gave %d rows", len(got))
	}
}

// The directory is asked once, then answered from the cache; after
// DirectoryEvery has passed, Graph is asked again.
func TestPeopleSearchCachesTheDirectory(t *testing.T) {
	d, calls := directoryDaemon(t, []Colleague{{Name: "Jamie Roe", Email: "jamie@example.com"}})
	mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "jamie"})
	mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "sch"})
	if *calls != 1 {
		t.Errorf("Directory was called %d times, want 1", *calls)
	}
	d.DirectoryEvery = time.Nanosecond
	mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "jamie"})
	if *calls != 2 {
		t.Errorf("after the interval Directory was called %d times, want 2", *calls)
	}
}

// A refresh that fails answers with the list from before rather than an empty
// picker, and only having nothing at all is an error.
func TestPeopleSearchKeepsTheStaleList(t *testing.T) {
	d, _, _ := seedTasks(t)
	rows := []Colleague{{Name: "Ana Silva", Email: "ana@example.com"}}
	fail := false
	d.Primary.Directory = func(context.Context) ([]Colleague, error) {
		if fail {
			return nil, errors.New("graph is down")
		}
		return rows, nil
	}
	mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "ana"})
	fail = true
	d.DirectoryEvery = time.Nanosecond
	got := peopleRows(mustAsk(t, d, []string{"people", "search"}, map[string]any{"positional": "ana"}))
	if len(got) != 1 || got[0].Name != "Ana Silva" {
		t.Errorf("stale directory did not answer: %+v", got)
	}
}

// No Microsoft 365 account means no directory, and the caller is told rather
// than handed an empty list that reads like "nobody works here".
func TestPeopleSearchWithoutAGraphAccount(t *testing.T) {
	d, _, _ := seedTasks(t)
	resp := ask(t, d, []string{"people", "search"}, map[string]any{"positional": "jamie"})
	if resp.OK || resp.Code != "usage" {
		t.Errorf("no directory answered %+v", resp)
	}
}
