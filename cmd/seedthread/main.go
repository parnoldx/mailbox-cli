// Command seedthread drops a simulated, back-and-forth email thread straight
// into the real Mirror, the same way internal/mirror/thread_test.go does, so
// the thread UI has something real to look at.
//
// It is NOT wired into the mailbox registry or build — it's a standing dev
// tool for this one purpose. Re-run it any time; it's idempotent by key, and
// it re-anchors its dates on "now" so the thread always ends today.
//
// The live daemon's reconciler reaps these placements on its next real sync of
// INBOX/Sent (their UIDs don't exist on the server, so it treats them as
// expunged) — that can happen within minutes of ordinary mail traffic. Run
// this, then go look immediately; don't expect it to sit there.
//
// The GUI (mailbox-omarchy) only reloads a bucket on a push or a reconnect
// (see onOnlineChanged in Main.qml) — writing straight to the Mirror file
// triggers neither. DO NOT restart mailbox.service to force that: a cold
// daemon start runs a full reconcile of every mirrored box before it opens
// the socket, and that reconcile treats these fake UIDs as expunged (they
// aren't on the real server) and deletes them within moments of starting.
// Restarting the daemon reaps its own seed.
//
// Instead: run this, then (re)launch mailbox-omarchy itself. Its
// Component.onCompleted does the same loadBucket()/loadCounts() call fresh,
// against the daemon that's still running untouched, so it just sees
// whatever is in the Mirror right now.
//
//	go run ./cmd/seedthread
//	cd gui-omarchy && make run
package main

import (
	"fmt"
	"log"
	"time"

	"mailbox/internal/config"
	"mailbox/internal/mirror"
)

const (
	me       = "Max Mustermann <max@example.org>"
	them     = "Jamie Roe <jamie@example.com>"
	meAddr   = "max@example.org"
	themAddr = "jamie@example.com"
)

type step struct {
	from, to string
	subject  string
	body     string
	offset   time.Duration // before "now"
	folder   string        // "INBOX" (incoming) or "Sent" (outgoing)
}

func main() {
	path, err := config.MirrorPath()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("mirror:", path)

	m, err := mirror.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer m.Close()

	inboxBase, err := nextUID(m, "INBOX")
	if err != nil {
		log.Fatal(err)
	}
	sentBase, err := nextUID(m, "Sent")
	if err != nil {
		log.Fatal(err)
	}

	now := time.Now().UTC()
	day := 24 * time.Hour
	subject := "Angebot Website-Relaunch"
	steps := []step{
		{them, meAddr, subject, "Hallo Max,\n\nwir würden unsere Website gerne überarbeiten lassen. " +
			"Hättest du Zeit, uns ein Angebot zu machen? Grob geht es um WordPress -> " +
			"etwas Modernes, responsive, mit neuem CMS.\n\nViele Grüße\nJamie", 4 * day, "INBOX"},
		{me, themAddr, "Re: " + subject, "Hallo Jamie,\n\ngerne! Damit ich das sauber kalkulieren kann: " +
			"wie viele Unterseiten sind es ungefähr, und soll der Blog mitkommen oder bleibt der " +
			"getrennt?\n\nViele Grüße\nMax", 3*day + 23*time.Hour, "Sent"},
		{them, meAddr, "Re: " + subject, "Hi Max,\n\nes sind aktuell 14 Unterseiten, der Blog soll " +
			"mit rein (aktuell ca. 60 Artikel). Wichtig wäre uns auch ein Kontaktformular mit " +
			"Spamschutz.\n\nGrüße\nJamie", 3*day + 18*time.Hour, "INBOX"},
		{me, themAddr, "Re: " + subject, "Hallo Jamie,\n\ndanke, das ergibt ein klares Bild. Für Relaunch " +
			"inkl. Blogmigration und Formular rechne ich mit 3-4 Wochen, Kostenrahmen 4.500-6.000 EUR " +
			"je nach Umfang der Individualkomponenten. Ich schicke dir ein schriftliches Angebot bis " +
			"Freitag.\n\nViele Grüße\nMax", 2*day + 21*time.Hour, "Sent"},
		{them, meAddr, "Re: " + subject, "Hi Max,\n\ndanke dir! Eine Frage noch vorab: könnten wir den " +
			"Starttermin auf Ende September legen? Wir warten noch auf neue Produktfotos.\n\nGrüße\nJamie",
			1*day + 4*time.Hour, "INBOX"},
		{me, themAddr, "Re: " + subject, "Hallo Jamie,\n\nEnde September passt gut, dann plane ich damit. " +
			"Ich melde mich, sobald das Angebot raus ist.\n\nViele Grüße\nMax", 2 * time.Hour, "Sent"},
		// A last reply that arrives with the parent quoted inline, Outlook-style
		// (Von/Gesendet/An/Betreff block, no "> " markers, no References the
		// mailbox mirror didn't already build) — this is what mail from a normal
		// client actually looks like, and the one the thread view should render
		// sanely rather than showing double.
		{them, meAddr, "Re: " + subject, fmt.Sprintf("Super, danke! Ich freue mich schon auf das Angebot. Bis dann!\n\n"+
			"Von: Max Mustermann <max@example.org>\nGesendet: %s\nAn: Jamie Roe <jamie@example.com>\n"+
			"Betreff: Re: %s\n\nHallo Jamie,\n\nEnde September passt gut, dann plane ich damit. Ich melde mich, "+
			"sobald das Angebot raus ist.\n\nViele Grüße\nMax", now.Add(-2*time.Hour).Format("02.01.2006 15:04"), subject),
			30 * time.Minute, "INBOX"},
	}

	var keys []string
	for i, s := range steps {
		key := fmt.Sprintf("seed-thread-%d@fake.example", i+1)
		keys = append(keys, key)

		tx, err := m.Begin("primary")
		if err != nil {
			log.Fatal(err)
		}
		var refs []string
		if i > 0 {
			refs = append(refs, keys[:i]...)
		}
		var inReplyTo []string
		if i > 0 {
			inReplyTo = []string{keys[i-1]}
		}
		date := now.Add(-s.offset)
		id, _, err := tx.UpsertMessage(mirror.Message{
			Key: key, Subject: s.subject, From: s.from, To: s.to,
			InReplyTo: inReplyTo, References: refs, Date: date,
		})
		if err != nil {
			log.Fatal(err)
		}
		if err := tx.SetBody(id, s.body, "", s.body); err != nil {
			log.Fatal(err)
		}
		var uid uint32
		if s.folder == "INBOX" {
			uid = inboxBase + uint32(i)
		} else {
			uid = sentBase + uint32(i)
		}
		// Everything's read except the last word, which is hers and still owed a reply.
		flags := []string{`\Seen`}
		if i == len(steps)-1 {
			flags = nil
		}
		if err := tx.PutPlacement(mirror.Placement{
			Folder: s.folder, UID: uid, MessageID: id, Flags: flags, InternalDate: date,
		}); err != nil {
			log.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("seeded %-6s uid=%-8d %s\n", s.folder, uid, s.subject)
	}

	fmt.Println("done — seven messages, one thread: three round trips plus a trailing, unread, " +
		"Outlook-quoted reply from her, ending 30m ago.")
	fmt.Println("now (re)launch mailbox-omarchy — it reloads fresh on startup. Don't restart the daemon.")
}

// nextUID picks a UID range for the simulated messages that cannot collide
// with anything the real server has assigned, now or for a very long time.
func nextUID(m *mirror.Mirror, folder string) (uint32, error) {
	f, err := m.Folder("primary", folder)
	if err != nil {
		return 0, err
	}
	return f.UIDNext + 5_000_000, nil
}
