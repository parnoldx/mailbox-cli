// Package skill carries the agent skill this program installs. It is embedded
// rather than read from a path, because `mailbox setup` installs it on machines
// that have no checkout of this repo — and because a copy that could be edited
// separately from the one the tests gate is the drift the fourteenth slice
// existed to end.
package skill

import _ "embed"

// Markdown is skill/SKILL.md, the one file every path installs.
//
//go:embed SKILL.md
var Markdown string
