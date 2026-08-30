const test = require("node:test")
const assert = require("node:assert/strict")
const Model = require("../Model.js")

const MOCK_BOXES = [
  { account: "primary", box: "inbox", count: 120, folder: "INBOX", unseen: 4, watched: true },
  { account: "primary", box: "feed", count: 45, folder: "INBOX/Feed", unseen: 0, watched: false },
  { account: "primary", box: "paper trail", count: 88, folder: "INBOX/Paper Trail", unseen: 0, watched: false },
  { account: "primary", box: "screener", count: 3, folder: "INBOX/Screener", unseen: 3, watched: true },
  { account: "work", box: "inbox", count: 15, folder: "INBOX", unseen: 1, watched: true }
]

const MOCK_SCREENER = [
  {
    address: "newsletter@sub.com",
    name: "Tech Digest",
    count: 2,
    unread: 2,
    newest: "2026-08-30 15:30",
    subject: "Weekly AI update",
    id: "Screener:42"
  },
  {
    address: "stranger@unknown.org",
    name: "Dr. Smith",
    count: 1,
    unread: 1,
    newest: "2026-08-30 16:15",
    subject: "Project proposal",
    id: "Screener:43"
  }
]

const MOCK_MESSAGES = [
  {
    id: "1001",
    uid: 1001,
    from: "Alice <alice@company.com>",
    subject: "Q3 Planning Meeting",
    date: "2026-08-30 16:20",
    seen: false,
    body_state: "Let us sync on Monday..."
  },
  {
    id: "1002",
    uid: 1002,
    from: "Bob <bob@vendor.com>",
    subject: "Contract Signed",
    date: "2026-08-30 14:00",
    seen: true,
    body_state: "Attached is the document..."
  }
]

test("account dropdown generates accounts list with unread totals", () => {
  const accounts = ["primary", "work"]
  const opts = Model.accountFilterOptions(accounts)
  assert.equal(opts.length, 3)
  assert.equal(opts[0].value, "")
  assert.equal(opts[0].label, "All accounts")
  assert.equal(opts[1].value, "primary")
  assert.equal(opts[2].value, "work")
})

test("screener cards are properly prepared for 1-click routing", () => {
  const cards = Model.screenerCards(MOCK_SCREENER, 8)
  assert.equal(cards.length, 2)
  assert.equal(cards[0].name, "Tech Digest")
  assert.equal(cards[0].address, "newsletter@sub.com")
  assert.equal(cards[0].count, 2)
  assert.equal(cards[0].initials, "TD")

  // Generate route commands for each destination
  const routeInbox = Model.requestToArgs({ kind: "route", target: cards[0].address, to: "inbox" })
  assert.deepEqual(routeInbox, { cmd: ["route"], args: { positional: "newsletter@sub.com", to: "inbox" } })

  const routeFeed = Model.requestToArgs({ kind: "route", target: cards[0].address, to: "feed" })
  assert.deepEqual(routeFeed, { cmd: ["route"], args: { positional: "newsletter@sub.com", to: "feed" } })

  const routeBlock = Model.requestToArgs({ kind: "route", target: cards[0].address, to: "block" })
  assert.deepEqual(routeBlock, { cmd: ["route"], args: { positional: "newsletter@sub.com", to: "block" } })
})

test("message seen and aside requests generate valid daemon commands", () => {
  const seenReq = Model.requestToArgs({ kind: "seen", id: MOCK_MESSAGES[0].id, seen: true })
  assert.deepEqual(seenReq, { cmd: ["seen"], args: { positional: "1001" } })

  const asideReq = Model.requestToArgs({ kind: "aside", id: MOCK_MESSAGES[0].id })
  assert.deepEqual(asideReq, { cmd: ["aside"], args: { positional: "1001" } })
})
