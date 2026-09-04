package mirror

// schemaVersion is bumped whenever the schema below changes. On a mismatch the
// Mirror file is deleted and rebuilt rather than migrated (ADR-0013).
const schemaVersion = 13

const schema = `
CREATE TABLE meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE folders (
  account        TEXT    NOT NULL,
  name           TEXT    NOT NULL,
  uidvalidity    INTEGER NOT NULL DEFAULT 0,
  uidnext        INTEGER NOT NULL DEFAULT 0,
  highestmodseq  INTEGER NOT NULL DEFAULT 0,
  synced_at      TEXT,
  PRIMARY KEY (account, name)
);

CREATE TABLE messages (
  id          INTEGER PRIMARY KEY,
  account     TEXT NOT NULL,
  message_key TEXT NOT NULL,
  date        TEXT,
  subject     TEXT NOT NULL DEFAULT '',
  from_addr   TEXT NOT NULL DEFAULT '',
  to_addr     TEXT NOT NULL DEFAULT '',
  -- Cc is mirrored because a reply to everyone is built from it. Deriving it
  -- from the body would mean fetching the message again to answer it.
  cc_addr     TEXT NOT NULL DEFAULT '',
  in_reply_to TEXT NOT NULL DEFAULT '',
  references_ TEXT NOT NULL DEFAULT '',
  -- The Thread this Message belongs to: the id of its oldest known member.
  -- Threads are built here rather than asked of the server, because IMAP THREAD
  -- cannot see past the selected mailbox (ADR-0008).
  thread_id   INTEGER NOT NULL DEFAULT 0,
  text_plain  TEXT NOT NULL DEFAULT '',
  text_html   TEXT NOT NULL DEFAULT '',
  body_state  TEXT NOT NULL DEFAULT 'pending',
  UNIQUE (account, message_key)
);

CREATE INDEX messages_thread ON messages(account, thread_id);

-- One row per Message-ID a Message references, which is what makes the link
-- findable in both directions: a reply mirrored before its parent has to join
-- the Thread when the parent finally arrives.
CREATE TABLE message_refs (
  message_id INTEGER NOT NULL REFERENCES messages(id),
  ref_key    TEXT    NOT NULL,
  PRIMARY KEY (message_id, ref_key)
);

CREATE INDEX message_refs_key ON message_refs(ref_key);

CREATE TABLE placements (
  account       TEXT    NOT NULL,
  folder        TEXT    NOT NULL,
  uid           INTEGER NOT NULL,
  message_id    INTEGER NOT NULL REFERENCES messages(id),
  flags         TEXT    NOT NULL DEFAULT '',
  -- The wall-clock instant a bubbled thread is due back in the Inbox, as
  -- 'YYYY-MM-DDTHH:MM' local time with no zone, or NULL when the mail is not
  -- bubbled. It is a projection of the '$bubble-*' IMAP keyword in flags,
  -- derived wherever a placement's flags are written, so a Mirror rebuild
  -- repopulates it from the keyword for free (ADR-0013). The two Daemons act on
  -- the keyword; this column is only the "due now" scan and the soonest-first
  -- sort.
  bubble_at     TEXT,
  internaldate  TEXT,
  size          INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (account, folder, uid)
);

CREATE INDEX placements_message ON placements(message_id);
CREATE INDEX placements_bubble ON placements(account, folder, bubble_at);

-- What a Message carries besides the text: names, types and sizes, never bytes.
-- Listing them is therefore a Mirror read like any other, and only naming one
-- specific file goes to the server (ADR-0003).
CREATE TABLE parts (
  message_id  INTEGER NOT NULL REFERENCES messages(id),
  path        TEXT    NOT NULL,
  mime_type   TEXT    NOT NULL DEFAULT '',
  filename    TEXT    NOT NULL DEFAULT '',
  disposition TEXT    NOT NULL DEFAULT '',
  size        INTEGER NOT NULL DEFAULT 0,
  -- Content-ID without angle brackets, so an HTML body's cid: image src can be
  -- resolved to the part that carries the bytes.
  content_id  TEXT    NOT NULL DEFAULT '',
  PRIMARY KEY (message_id, path)
);

-- Search is answered here and nowhere else: there is no fall-through to IMAP
-- SEARCH (ADR-0009). The rowid is the message id, and the body column holds the
-- text a reader would see — the plain part, or the HTML rendered down to it, so
-- a query does not match on markup.
CREATE VIRTUAL TABLE messages_fts USING fts5(subject, addresses, body);

-- One CalDAV or CardDAV collection: one calendar, one task list, one address
-- book. Found by enumerating the server, never by a URL written down by hand —
-- that is how the address book ended up pointing at a 2-entry scratch list.
CREATE TABLE dav_collections (
  id         INTEGER PRIMARY KEY,
  account    TEXT NOT NULL,
  kind       TEXT NOT NULL,              -- events | tasks | cards
  url        TEXT NOT NULL,
  name       TEXT NOT NULL,              -- the display name, which is what a caller types
  color      TEXT NOT NULL DEFAULT '',
  sync_token TEXT NOT NULL DEFAULT '',
  synced_at  TEXT,
  UNIQUE (account, url)
);

-- One object on a collection. The raw text is the record and every column beside
-- it is a projection of it, rebuilt whenever that text changes (ADR-0010). A
-- recurring event is one row: expanding a rule with no end has no finite answer
-- to store, and the window is a property of the question.
CREATE TABLE dav_objects (
  id            INTEGER PRIMARY KEY,
  collection_id INTEGER NOT NULL REFERENCES dav_collections(id) ON DELETE CASCADE,
  href          TEXT    NOT NULL,
  etag          TEXT    NOT NULL DEFAULT '',
  raw           TEXT    NOT NULL,
  kind          TEXT    NOT NULL DEFAULT '',   -- event | todo | other
  uid           TEXT    NOT NULL DEFAULT '',
  summary       TEXT    NOT NULL DEFAULT '',
  location      TEXT    NOT NULL DEFAULT '',
  description   TEXT    NOT NULL DEFAULT '',
  status        TEXT    NOT NULL DEFAULT '',
  -- A contact's addresses and numbers, space separated. They are projected
  -- rather than parsed on every search: "who is this number" is a question
  -- worth answering from the Mirror.
  emails        TEXT    NOT NULL DEFAULT '',
  phones        TEXT    NOT NULL DEFAULT '',
  starts_at     TEXT,
  ends_at       TEXT,
  due_at        TEXT,
  -- Whether the due date has a clock on it. "By Friday" and "by Friday at
  -- 00:00" are the same instant and different promises, and only the second is
  -- worth showing a time for.
  due_all_day   INTEGER NOT NULL DEFAULT 0,
  -- A Todo's iCalendar PRIORITY: 0 for nobody having said, 1-4 high, 5 medium,
  -- 6-9 low. Projected rather than parsed on every listing, because a list
  -- sorted by what matters is the ordinary way to read one.
  priority      INTEGER NOT NULL DEFAULT 0,
  completed_at  TEXT,
  all_day       INTEGER NOT NULL DEFAULT 0,
  recurring     INTEGER NOT NULL DEFAULT 0,
  -- The last instant an occurrence can start. NULL means the rule never ends,
  -- which a window query has to treat as always possibly relevant.
  repeats_until TEXT,
  UNIQUE (collection_id, href)
);

CREATE INDEX dav_objects_when ON dav_objects(collection_id, starts_at);

-- The Routing: where the Primary Account's Sieve script sends each sender's
-- mail. The script is the record and these rows are a projection of it, rebuilt
-- whenever it is read or written -- the same rule the calendars follow
-- (ADR-0010), applied to the second format a server owns rather than us.
CREATE TABLE routing (
  account TEXT NOT NULL,
  address TEXT NOT NULL,
  dest    TEXT NOT NULL,             -- inbox | feed | paper | block
  box     TEXT NOT NULL DEFAULT '',  -- the Box it files into; empty when discarded
  PRIMARY KEY (account, address)
);

-- The script itself, so that "what does the routing actually say" is a Mirror
-- read like everything else and does not need the server to be reachable.
CREATE TABLE routing_script (
  account   TEXT PRIMARY KEY,
  name      TEXT NOT NULL,
  raw       TEXT NOT NULL,
  -- Whether the server has this script switched on. A routing script that is
  -- stored but not active routes nothing, and that is a thing a caller has to
  -- be able to find out without the server being reachable.
  active    INTEGER NOT NULL DEFAULT 0,
  synced_at TEXT
);

-- One address the mailbox has actually exchanged mail with, kept in step with
-- messages as they are mirrored (Tx.upsertCorrespondents) rather than parsed
-- from raw headers on every keystroke. This is the second layer of recipient
-- autocomplete: the address book first, "seen in mail" as the fallback.
CREATE TABLE correspondents (
  account   TEXT NOT NULL,
  email     TEXT NOT NULL,
  name      TEXT NOT NULL DEFAULT '',
  last_seen TEXT,
  count     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (account, email)
);

CREATE INDEX correspondents_name ON correspondents(account, name COLLATE NOCASE);

-- A sync step records its intent before touching the network, so a crash leaves
-- either the old state or the new one, never an advanced modseq over a
-- half-fetched folder (ADR-0015).
CREATE TABLE sync_journal (
  account    TEXT NOT NULL,
  folder     TEXT NOT NULL,
  intent     TEXT NOT NULL,
  started_at TEXT NOT NULL,
  PRIMARY KEY (account, folder)
);
`
