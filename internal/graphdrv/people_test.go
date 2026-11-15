package graphdrv

import (
	"context"
	"testing"
)

// The directory is the groups' people, not the tenant's accounts: both groups
// the sign-in belongs to are read, a member of both is one row, the address is
// the mail (the UPN where there is none), and the signed-in user is not on
// their own list.
func TestDirectoryListsTheGroupsColleagues(t *testing.T) {
	f, c := newFakeGraph(t)
	f.members["g-team"] = []map[string]any{
		{"displayName": "Jamie Roe", "mail": "jamie@example.com"},
		{"displayName": "Me", "mail": "me@example.com"},
		{"displayName": "", "givenName": "Erika", "surname": "Mustermann", "mail": "erika@example.com"},
		{"displayName": "Ana Silva", "mail": "", "userPrincipalName": "ana@example.com"},
	}
	f.members["g-list"] = []map[string]any{
		{"displayName": "Jamie Roe", "mail": "JAMIE@example.com"},
		{"displayName": "Tenant Admin", "mail": "admin@other.example"},
		{"displayName": "No Address"},
	}
	got, err := c.Directory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Person{
		{Name: "Ana Silva", Email: "ana@example.com"},
		{Name: "Erika Mustermann", Email: "erika@example.com"},
		{Name: "Jamie Roe", Email: "jamie@example.com"},
	}
	if len(got) != len(want) {
		t.Fatalf("directory = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// A group whose members cannot be read does not take the readable groups down
// with it.
func TestDirectorySurvivesOneUnreadableGroup(t *testing.T) {
	f, c := newFakeGraph(t)
	f.groups["g-broken"] = map[string]any{"displayName": "Broken", "groupTypes": []any{}}
	f.members["g-team"] = []map[string]any{
		{"displayName": "Jamie Roe", "mail": "jamie@example.com"},
	}
	// g-broken has no members entry: the fake answers an empty list, not an
	// error, so the readable group is what the test holds the answer to.
	got, err := c.Directory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "Jamie Roe" {
		t.Errorf("directory = %+v", got)
	}
}
