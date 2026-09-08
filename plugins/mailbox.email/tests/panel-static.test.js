const test = require("node:test")
const assert = require("node:assert/strict")
const fs = require("node:fs")
const path = require("node:path")

const panelSrc = fs.readFileSync(path.join(__dirname, "..", "Panel.qml"), "utf8")
const barSrc = fs.readFileSync(path.join(__dirname, "..", "BarWidget.qml"), "utf8")
const serviceSrc = fs.readFileSync(path.join(__dirname, "..", "MailboxService.qml"), "utf8")

function count(str, ch) {
  return str.split(ch).length - 1
}

test("Panel.qml braces and parens are balanced", () => {
  assert.equal(count(panelSrc, "{"), count(panelSrc, "}"))
  assert.equal(count(panelSrc, "("), count(panelSrc, ")"))
  assert.equal(count(panelSrc, "["), count(panelSrc, "]"))
})

test("BarWidget.qml braces and parens are balanced", () => {
  assert.equal(count(barSrc, "{"), count(barSrc, "}"))
  assert.equal(count(barSrc, "("), count(barSrc, ")"))
  assert.equal(count(barSrc, "["), count(barSrc, "]"))
})

test("MailboxService.qml braces and parens are balanced", () => {
  assert.equal(count(serviceSrc, "{"), count(serviceSrc, "}"))
  assert.equal(count(serviceSrc, "("), count(serviceSrc, ")"))
  assert.equal(count(serviceSrc, "["), count(serviceSrc, "]"))
})

test("Mail actions are wired in Panel.qml", () => {
  for (const needle of [
    "Model.feedItems",
    "root.feed",
    "service.setSeen",
    "service.setAside",
    "service.trashMessage"
  ]) {
    assert.ok(panelSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})

// The widget is a notification for new inbox mail and nothing else. Screening
// is a decision owed when you next sit down, so it belongs to the desktop
// client — a notification panel that also asks you to triage is two jobs.
test("Panel.qml carries no screener UI at all", () => {
  // Only the header comment may still say the word, explaining the absence.
  const code = panelSrc.split("\n").filter(l => l.trim().indexOf("//") !== 0).join("\n")
  for (const needle of ["screener", "Screener", "SCREENER", "routeSender", "filterMode"]) {
    assert.equal(code.indexOf(needle), -1, "still references " + needle)
  }
})

test("Panel.qml has one stream, no previously-seen tab", () => {
  assert.equal(panelSrc.indexOf('tabFilter'), -1)
  assert.equal(panelSrc.indexOf('"previous"'), -1)
  assert.equal(panelSrc.match(/id: feedRepeater/g).length, 1)
})

test("Dynamic visibility is implemented in BarWidget.qml", () => {
  for (const needle of [
    "hideWhenEmpty",
    "widgetVisible",
    "BarIconButton"
  ]) {
    assert.ok(barSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})

// Unread inbox mail is the only thing that raises the icon. Login codes, the
// one genuinely urgent thing that used to land in the screener, are collected
// by the daemon before the widget sees them (Pickups).
test("BarWidget.qml is raised by unread inbox mail alone", () => {
  assert.ok(
    /hasNew:\s*unseenCount > 0/.test(barSrc),
    "hasNew must be driven by unread mail alone"
  )
  assert.equal(
    barSrc.indexOf("screenerCount"), -1,
    "the widget must not track a screener count any more"
  )
})

test("MailboxService.qml speaks daemon socket protocol", () => {
  for (const needle of [
    "mailbox.sock",
    "box",
    "list",
    "seen",
    "mail.changed"
  ]) {
    assert.ok(serviceSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})
