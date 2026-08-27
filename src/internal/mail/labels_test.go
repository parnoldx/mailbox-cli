package mail

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeLabel(t *testing.T) {
	cases := map[string]string{
		"Travel receipts": "travel-receipts",
		"travel-receipts": "travel-receipts",
		"Foo_Bar":         "foo-bar",
		"  invoices  ":    "invoices",
	}
	for in, want := range cases {
		got, err := NormalizeLabel(in)
		if err != nil || got != want {
			t.Errorf("NormalizeLabel(%q) = %q, %v want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "!!!", "seen", "\\Flagged", "asidedue-20260101t1200z"} {
		if _, err := NormalizeLabel(in); err == nil {
			t.Errorf("NormalizeLabel(%q) should fail", in)
		}
	}
}

func TestIsLabel(t *testing.T) {
	yes := []string{"learn", "personal", "school", "travel-receipts"}
	no := []string{
		"seen", "$forwarded", "$cl_0", "$cl_10", "cl_0", "cl_10",
		"$hasnoattachment", "$purchases", "$social", "$test",
		"asidedue-20260101t1200z",
	}
	for _, s := range yes {
		if !IsLabel(s) {
			t.Errorf("IsLabel(%q) = false", s)
		}
	}
	for _, s := range no {
		if IsLabel(s) {
			t.Errorf("IsLabel(%q) = true", s)
		}
	}
}

func TestLabelsFromFlags(t *testing.T) {
	got := LabelsFromFlags([]string{"seen", "travel-receipts", "answered", "asidedue-20260101t1200z", "$forwarded", "$label1", "$cl_0", "cl_0", "learn"})
	want := []string{"travel-receipts", "learn"}
	if !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestSearchCriteriaKeyword(t *testing.T) {
	got, err := searchCriteria(SearchQuery{Keyword: "travel-receipts"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"KEYWORD", "travel-receipts"}
	if !eq(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if (SearchQuery{Keyword: "travel-receipts"}).Empty() {
		t.Fatal("keyword-only query should not be empty")
	}
}

func TestCreateLabelCatalog(t *testing.T) {
	dir := t.TempDir()
	old := labelsPathFunc
	defer func() { labelsPathFunc = old }()
	labelsPathFunc = func() (string, error) { return filepath.Join(dir, "labels"), nil }

	if _, ok := CatalogLabels(); ok {
		t.Fatal("missing catalog should not be ok")
	}
	got, err := CreateLabel("Travel receipts")
	if err != nil || got != "travel-receipts" {
		t.Fatalf("CreateLabel: %q %v", got, err)
	}
	names, ok := CatalogLabels()
	if !ok || !eq(names, []string{"travel-receipts"}) {
		t.Fatalf("catalog %v ok=%v", names, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, "labels")); err != nil {
		t.Fatal(err)
	}
}
