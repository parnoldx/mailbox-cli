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
    "block",
    "Model.feedItems",
    "root.feed",
    "service.setSeen",
    "service.setAside",
    "service.trashMessage"
  ]) {
    assert.ok(panelSrc.indexOf(needle) !== -1, "missing " + needle)
  }
})

test("Panel.qml screener offers only inbox / block / trash, not feed / paper trail", () => {
  assert.equal(panelSrc.indexOf('routeSender(feedRow.modelData.address, "feed")'), -1)
  assert.equal(panelSrc.indexOf('routeSender(feedRow.modelData.address, "paper trail")'), -1)
  assert.ok(panelSrc.indexOf('routeSender(feedRow.modelData.address, "inbox")') !== -1)
  assert.ok(panelSrc.indexOf('routeSender(feedRow.modelData.address, "block")') !== -1)
})

test("Panel.qml has one stream, no previously-seen tab", () => {
  assert.equal(panelSrc.indexOf('tabFilter'), -1)
  assert.equal(panelSrc.indexOf('"previous"'), -1)
  assert.equal(panelSrc.match(/id: feedRepeater/g).length, 1)
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
