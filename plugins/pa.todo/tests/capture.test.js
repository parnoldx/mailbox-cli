const test = require("node:test")
const assert = require("node:assert/strict")
const fs = require("node:fs")
const path = require("node:path")

const Model = require("../Model.js")
const { todoAddArgv } = require("../Capture.js")

// 2026-08-23 is a Sunday; fixed now so "friday" etc. are deterministic.
const now = Date.parse("2026-08-23T10:00:00+02:00")
const argv = text => todoAddArgv(text, now, Model)

test("a bare line is still just a title, no due date", () => {
  assert.deepEqual(argv("call the plumber"), ["todo", "add", "call the plumber"])
  assert.equal(argv("   "), null)
})

test("a trailing ! sets priority but keeps the todo loose", () => {
  assert.deepEqual(argv("call the plumber !"),
    ["todo", "add", "call the plumber", "--priority", "low"])
  assert.deepEqual(argv("ship the release !!!"),
    ["todo", "add", "ship the release", "--priority", "high"])
})

test("a named day rides along as a date-only --due", () => {
  assert.deepEqual(argv("buy milk friday !"),
    ["todo", "add", "buy milk", "--due", "2026-08-28", "--priority", "low"])
  assert.deepEqual(argv("Rechnung zahlen morgen"),
    ["todo", "add", "Rechnung zahlen", "--due", "2026-08-24"])
})

test("a time on the day makes the due a datetime", () => {
  assert.deepEqual(argv("call mom tuesday 18:00"),
    ["todo", "add", "call mom", "--due", "2026-08-25 18:00"])
})

test("a /list in the phrase becomes --list", () => {
  assert.deepEqual(argv("einkaufen morgen /Arbeit"),
    ["todo", "add", "einkaufen", "--due", "2026-08-24", "--list", "Arbeit"])
})

test("Model.js is a verbatim copy of the calendar widget's", () => {
  const here = fs.readFileSync(path.join(__dirname, "..", "Model.js"), "utf8")
  const src = fs.readFileSync(
    path.join(__dirname, "..", "..", "mailbox.clock", "Model.js"), "utf8")
  assert.equal(here, src, "run: cp plugins/mailbox.clock/Model.js plugins/pa.todo/Model.js")
})
