package mirror

import (
	"fmt"
	"strings"
	"unicode"
)

// Query is a search over the Mirror. Text is required: filters narrow a search,
// they are not one (ADR-0009).
type Query struct {
	Text  string
	Box   string // one Box, or empty for all of them
	From  string // substring of the sender
	Limit int
}

// Hit is one search result: a Placement, its Message, and the text around the
// match. A Message in two Boxes is one Hit, so a result list is a list of
// Messages and not of copies.
type Hit struct {
	Row
	Snippet string
	Rank    float64
}

// Search answers a query from the Mirror and never from the server (ADR-0009).
// Trash is not searchable because it is not mirrored, and a Message whose last
// Placement has gone — trashed after it was mirrored — falls out with it: the
// join to placements is what makes deleted mail stop turning up.
func (m *Mirror) Search(account string, q Query) ([]Hit, error) {
	match, err := MatchExpr(q.Text)
	if err != nil {
		return nil, err
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 50
	}
	args := []any{match, account}
	where := ""
	if q.Box != "" {
		where += " AND p.folder = ?"
		args = append(args, q.Box)
	}
	if q.From != "" {
		where += " AND m.from_addr LIKE ?"
		args = append(args, "%"+q.From+"%")
	}
	// Bounded rather than exact: a Message in two Boxes is two rows here and
	// one Hit below, so the query asks for more rows than it will return.
	args = append(args, limit*4+32)

	// snippet() and bm25() may only be used where the FTS table is queried
	// directly — not through a subquery or a window function — so the
	// one-Hit-per-Message rule is applied here rather than in SQL. Ordering by
	// rank and then by Box keeps a Message's Placements adjacent.
	rows, err := m.db.Query(`
		SELECT `+rowFields+`, p.folder,
		       snippet(messages_fts, 2, '', '', '…', 12),
		       bm25(messages_fts) AS rank
		  FROM messages_fts
		  JOIN messages m ON m.id = messages_fts.rowid
		  JOIN placements p ON p.message_id = m.id AND p.account = m.account
		 WHERE messages_fts MATCH ? AND m.account = ?`+where+`
		 ORDER BY rank, (p.folder = 'INBOX') DESC, p.folder
		 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Hit
	seen := map[int64]bool{}
	for rows.Next() {
		var h Hit
		var folder string
		scan := func(dest ...any) error {
			return rows.Scan(append(dest, &folder, &h.Snippet, &h.Rank)...)
		}
		r, err := scanRow(scan, "")
		if err != nil {
			return nil, err
		}
		if seen[r.Message.ID] {
			continue // the same Message in another Box
		}
		seen[r.Message.ID] = true
		r.Placement.Folder = folder
		h.Row = r
		out = append(out, h)
		if len(out) == limit {
			break
		}
	}
	return out, rows.Err()
}

// MatchExpr turns what a caller typed into an FTS5 expression. Every term is
// quoted and the terms are ANDed, so a query is read as words to find rather
// than as syntax: `subject:x`, `foo-bar` and a lone `"` are all ordinary text
// here, and none of them can make the query fail to parse.
//
// A "quoted phrase" survives as one term, because that is the one piece of
// query syntax people actually mean.
func MatchExpr(text string) (string, error) {
	terms := terms(text)
	if len(terms) == 0 {
		return "", fmt.Errorf("search needs something to look for")
	}
	quoted := make([]string, 0, len(terms))
	for _, t := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND "), nil
}

// terms splits on whitespace, keeping "quoted phrases" whole and dropping
// anything with no word characters in it at all.
func terms(text string) []string {
	var out []string
	var cur strings.Builder
	inQuotes := false
	flush := func() {
		if s := cur.String(); hasWord(s) {
			out = append(out, s)
		}
		cur.Reset()
	}
	for _, r := range text {
		switch {
		case r == '"':
			if inQuotes {
				flush()
			}
			inQuotes = !inQuotes
		case unicode.IsSpace(r) && !inQuotes:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

func hasWord(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
