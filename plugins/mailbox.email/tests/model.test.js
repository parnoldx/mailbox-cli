const test = require("node:test")
const assert = require("node:assert/strict")
const Model = require("../Model.js")

test("cleanSenderName extracts clean names or emails", () => {
  assert.equal(Model.cleanSenderName('"Alice Smith" <alice@example.com>'), "Alice Smith")
  assert.equal(Model.cleanSenderName("Bob <bob@example.com>"), "Bob")
  assert.equal(Model.cleanSenderName("<support@company.com>"), "support")
  assert.equal(Model.cleanSenderName("dev@project.org"), "dev")
  assert.equal(Model.cleanSenderName("John Doe"), "John Doe")
  assert.equal(Model.cleanSenderName(""), "")
})

test("cleanAddress extracts valid email addresses", () => {
  assert.equal(Model.cleanAddress('"Alice Smith" <alice@example.com>'), "alice@example.com")
  assert.equal(Model.cleanAddress("<bob@example.com>"), "bob@example.com")
  assert.equal(Model.cleanAddress("carol@example.com"), "carol@example.com")
  assert.equal(Model.cleanAddress(""), "")
})

test("extractInitials generates 1-2 character uppercase initials", () => {
  assert.equal(Model.extractInitials("Alice Smith <alice@example.com>"), "AS")
  assert.equal(Model.extractInitials("Alice <alice@example.com>"), "AL")
  assert.equal(Model.extractInitials("A <a@example.com>"), "A")
  assert.equal(Model.extractInitials("support@company.com"), "SU")
  assert.equal(Model.extractInitials("john.doe@test.com"), "JD")
  assert.equal(Model.extractInitials(""), "?")
})

test("avatarColorIndex hashes deterministically within bounds", () => {
  const idx1 = Model.avatarColorIndex("alice@example.com", 8)
  const idx2 = Model.avatarColorIndex("alice@example.com", 8)
  assert.equal(idx1, idx2)
  assert.ok(idx1 >= 0 && idx1 < 8)

  const idx3 = Model.avatarColorIndex("bob@example.com", 8)
  assert.ok(idx3 >= 0 && idx3 < 8)
})

test("formatRelativeTime formats timestamps correctly", () => {
  const now = 1756560000000 // Fixed epoch for testing
  assert.equal(Model.formatRelativeTime(now - 10000, now), "just now")
  assert.equal(Model.formatRelativeTime(now - 5 * 60 * 1000, now), "5m ago")
  assert.equal(Model.formatRelativeTime(now - 45 * 60 * 1000, now), "45m ago")
})

test("filterMessages filters by account and seen status", () => {
  const msgs = [
    { id: "1", account: "primary", seen: false, subject: "M1" },
    { id: "2", account: "primary", seen: true, subject: "M2" },
    { id: "3", account: "work", seen: false, subject: "M3" },
    { id: "4", account: "work", seen: true, subject: "M4" }
  ]

  const unreadAll = Model.filterMessages(msgs, "", "unread")
  assert.deepEqual(unreadAll.map(m => m.id), ["1", "3"])

  const prevAll = Model.filterMessages(msgs, "", "previous")
  assert.deepEqual(prevAll.map(m => m.id), ["2", "4"])

  const unreadWork = Model.filterMessages(msgs, "work", "unread")
  assert.deepEqual(unreadWork.map(m => m.id), ["3"])
})

test("feedItems streams unread inbox mail, newest first", () => {
  const msgs = [
    { id: "m1", account: "primary", seen: false, subject: "M1", from: "a@x.com", date: "2026-08-30 09:00" },
    { id: "m2", account: "primary", seen: true, subject: "read", from: "b@x.com", date: "2026-08-30 15:00" },
    { id: "m3", account: "work", seen: false, subject: "M3", from: "c@x.com", date: "2026-08-30 12:00" }
  ]

  const all = Model.feedItems(msgs, "")
  assert.deepEqual(all.map(i => i.id), ["m3", "m1"])

  // Read mail never shows up in the stream.
  assert.equal(all.some(i => i.id === "m2"), false)

  assert.deepEqual(Model.feedItems(msgs, "work").map(i => i.id), ["m3"])

  // Undated entries sort last instead of jumping to the top.
  const undated = Model.feedItems(
    [{ id: "m9", seen: false, from: "d@x.com" },
     { id: "m8", seen: false, from: "e@x.com", date: "2026-08-30 09:00" }], "")
  assert.deepEqual(undated.map(i => i.id), ["m8", "m9"])
})

test("accountFilterOptions suffixes unread counts when messages are supplied", () => {
  const msgs = [
    { account: "primary", seen: false },
    { account: "primary", seen: true },
    { account: "work", seen: false }
  ]

  assert.deepEqual(
    Model.accountFilterOptions(["primary", "work"], msgs).map(o => o.label),
    ["All accounts (2)", "primary (1)", "work (1)"]
  )

  // No messages: plain labels, no "(0)" noise.
  assert.deepEqual(
    Model.accountFilterOptions(["primary"], []).map(o => o.label),
    ["All accounts", "primary"]
  )
})

test("buildOpenCommand always opens the desktop client, id shell-quoted", () => {
  assert.equal(Model.buildOpenCommand("123"), "mailbox-gui --open '123'")
  assert.equal(Model.buildOpenCommand("INBOX:456"), "mailbox-gui --open 'INBOX:456'")
  assert.equal(Model.buildOpenCommand("a'b"), "mailbox-gui --open 'a'\\''b'")
  assert.equal(Model.buildOpenCommand(""), "mailbox-gui --open ''")
})

// The Screener is a watched box, so a naive "unseen in watched boxes" count
// picks it up and raises the bar icon for mail that owes a decision rather than
// an answer. The widget shows no screener at all any more, so this filter is
// the only thing keeping that mail from arriving as an alarm by another name.
test("isScreenerFolder keeps screener mail out of the unread count", () => {
  assert.equal(Model.isScreenerFolder("INBOX/Screener"), true)
  assert.equal(Model.isScreenerFolder("INBOX/Screener/Block"), true)
  assert.equal(Model.isScreenerFolder("INBOX"), false)
  assert.equal(Model.isScreenerFolder("INBOX/Feed"), false)
  assert.equal(Model.isScreenerFolder(""), false)
  assert.equal(Model.isScreenerFolder(undefined), false)
})
