package graphdrv

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Person is one colleague in the directory: the basic profile
// User.ReadBasic.All covers, which is a name and an address and nothing more.
type Person struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// directoryGroup is one group the signed-in account belongs to. The cast on
// memberOf leaves directory roles out, so every row here has members.
type directoryGroup struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// directoryUser is what a group's members are asked for. The scope allows
// exactly these properties; asking for jobTitle or accountEnabled is refused
// with a 403 rather than an empty field.
type directoryUser struct {
	DisplayName string `json:"displayName"`
	GivenName   string `json:"givenName"`
	Surname     string `json:"surname"`
	Mail        string `json:"mail"`
	UPN         string `json:"userPrincipalName"`
}

// Directory lists the people in the Microsoft 365 groups the sign-in belongs
// to: the staff team, not every account in the tenant. Room mailboxes,
// bug-tracker addresses and other service accounts are not group members, so
// they stay out, and so does anybody on a domain other than the signed-in
// account's own. The user is left off their own list.
//
// User.ReadBasic.All is what lets the names be read off the group members;
// Group.ReadWrite.All, already needed for the group calendars, is what allows
// the groups themselves.
func (c *Client) Directory(ctx context.Context) ([]Person, error) {
	me, err := c.Me(ctx)
	if err != nil {
		return nil, err
	}
	own := strings.ToLower(strings.TrimSpace(me))
	ownDomain := addressDomain(own)
	if ownDomain == "" {
		return nil, fmt.Errorf("the signed-in account has no address to know its colleagues by")
	}

	groups, _, err := all[directoryGroup](ctx, c,
		"/me/memberOf/microsoft.graph.group?$select=id,displayName&$top=100")
	if err != nil {
		return nil, fmt.Errorf("groups: %w", err)
	}

	seen := map[string]bool{own: true}
	out := []Person{}
	var firstErr error
	for _, g := range groups {
		if g.ID == "" {
			continue
		}
		users, _, err := all[directoryUser](ctx, c,
			"/groups/"+url.PathEscape(g.ID)+"/members/microsoft.graph.user"+
				"?$select=displayName,givenName,surname,mail,userPrincipalName&$top=999")
		if err != nil {
			// One group whose members cannot be read (a role, a group the
			// delegated sign-in cannot see into) must not empty the list.
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, u := range users {
			addr := strings.TrimSpace(u.Mail)
			if addr == "" {
				addr = strings.TrimSpace(u.UPN)
			}
			key := strings.ToLower(addr)
			if !strings.Contains(key, "@") || addressDomain(key) != ownDomain || seen[key] {
				continue
			}
			seen[key] = true
			name := strings.TrimSpace(u.DisplayName)
			if name == "" {
				name = strings.TrimSpace(u.GivenName + " " + u.Surname)
			}
			// Addresses are compared without case; they are carried without it
			// too, so one colleague spelled two ways in two groups is one row.
			out = append(out, Person{Name: name, Email: key})
		}
	}
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// addressDomain is the part after the last @, lowercased. Empty when the
// string is not an address.
func addressDomain(address string) string {
	at := strings.LastIndex(address, "@")
	if at == -1 {
		return ""
	}
	return strings.ToLower(address[at+1:])
}
