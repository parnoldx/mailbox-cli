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

test("Screening and routing are wired in Panel.qml", () => {
  for (const needle of [
    "service.routeSender",
    "inbox",
    "feed",
    "paper trail",
    "block",
    "screenerCards",
    "filteredMessages",
    "service.setSeen",
    "service.setAside",
    "service.trashMessage"
  ]) {
    assert.ok(panelSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})

test("Dynamic visibility is implemented in BarWidget.qml", () => {
  for (const needle of [
    "hideWhenEmpty",
    "totalAlertCount",
    "widgetVisible",
    "BarIconButton"
  ]) {
    assert.ok(barSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})

test("MailboxService.qml speaks daemon socket protocol", () => {
  for (const needle of [
    "mailbox.sock",
    "box",
    "list",
    "screener",
    "route",
    "seen",
    "mail.changed"
  ]) {
    assert.ok(serviceSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})
