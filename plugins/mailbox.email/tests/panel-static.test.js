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

// Unread inbox mail raises the icon, and so does a Pickup the daemon is still
// holding. A Pickup can never be counted as unread — the daemon marks it read
// when it takes the code out — so without its own term the one thing that stops
// being useful in fifteen minutes would be the one thing the bar never showed.
// Everything else stays out, and the Screener above all.
test("BarWidget.qml is raised by unread mail and held pickups", () => {
  assert.ok(
    /hasNew:\s*unseenCount > 0/.test(barSrc),
    "hasNew must be driven by unread mail alone"
  )
  assert.ok(barSrc.indexOf("pickupReady") !== -1, "a held Pickup must raise the icon")
  assert.ok(
    barSrc.indexOf("service.pickupCount") !== -1,
    "the icon must read the held pickups from the service"
  )
  assert.equal(
    barSrc.indexOf("screenerCount"), -1,
    "the widget must not track a screener count any more"
  )
})

// A toast is silenced by Do Not Disturb and the code is on the clipboard with
// nothing on screen saying so; the panel is where the code is found again once
// the clipboard has moved on. Both the row and its click have to hand it over
// again, and neither may show a link's token.
test("Held pickups are listed and copyable in Panel.qml", () => {
  for (const needle of [
    "service.pickups",
    "service.copyPickup",
    "READY TO PASTE"
  ]) {
    assert.ok(panelSrc.indexOf(needle) !== -1, "missing " + needle)
  }
  assert.equal(
    panelSrc.indexOf("modelData.link"), -1,
    "the row must show the host the daemon chose, not the raw URL"
  )
})

test("MailboxService.qml speaks daemon socket protocol", () => {
  for (const needle of [
    "mailbox.sock",
    "box",
    "list",
    "seen",
    "mail.changed",
    "pickup",
    "copyPickup"
  ]) {
    assert.ok(serviceSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})
