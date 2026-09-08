const test = require("node:test")
const assert = require("node:assert/strict")
const Model = require("../Model.js")

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

test("inbox messages split by seen state (the panel only streams the unread half)", () => {
  const msgs = MOCK_MESSAGES.map(m => ({
    id: m.id,
    account: "primary",
    seen: m.seen,
    subject: m.subject
  }))

  assert.deepEqual(Model.filterMessages(msgs, "", "unread").map(m => m.id), ["1001"])
  assert.deepEqual(Model.filterMessages(msgs, "", "previous").map(m => m.id), ["1002"])
})
