const test = require("node:test")
const assert = require("node:assert/strict")
const fs = require("node:fs")
const path = require("node:path")

const src = fs.readFileSync(path.join(__dirname, "..", "Panel.qml"), "utf8")

function count(ch) {
  return src.split(ch).length - 1
}

test("Panel.qml braces and parens are balanced", () => {
  assert.equal(count("{"), count("}"))
  assert.equal(count("("), count(")"))
  assert.equal(count("["), count("]"))
})

test("entry pane, Esc stack and silent create are wired", () => {
  for (const needle of [
    "entryOpen",
    "openEntry",
    "closeEntry",
    "MailboxService",
    "requestToArgs",
    "askCalendar",
    "commitEntry",
    "parseEventPhrase",
    "Qt.Key_Escape",
    "entryKind",
    "MailboxService {"
  ]) {
    assert.ok(src.indexOf(needle) !== -1, "missing " + needle)
  }
})

test("the panel reads and writes through the daemon socket, not scripts", () => {
  for (const needle of [
    "Model.dateKey(from.getFullYear(), from.getMonth(), from.getDate())",
    "mailbox.call(",
    "calendar.changed",
    "event.changed",
    "todo.changed"
  ]) {
    assert.ok(src.indexOf(needle) !== -1, "missing " + needle)
  }
  for (const gone of [
    "call([\"calendar\", \"list\"], []",
    "sync-calendar",
    "quick-add",
    "quickAddProc",
    "completeProc",
    "quickTodoProc",
    "taskSyncTimer",
    "state/mailbox/"
  ]) {
    assert.ok(src.indexOf(gone) === -1, "still references " + gone)
  }
})

test("the entry pane is the coloured-phrase screen, not the old chip form", () => {
  for (const needle of [
    "component PhraseField",   // plain editor under a painted overlay
    "component SlotEdit",      // click-to-edit title, days and times
    "component KindTab",       // EVENT / TASK
    "component EntryRow",      // one optional part, opened by its pill
    "phraseMarkup",            // Model.phraseHtml feeds the overlay
    "roleCalendarColor",       // /name paints in the calendar's own colour
    "rowOpen(",                // pills and rows are one switch
    "dayLabel(",               // Qt date formatting, not JS options
    "ensureCalendarForKind",   // a list that cannot hold this kind never sticks
    "formLink",                // the meeting link is a part of its own
    "linkProviderLabel",       // the pill names the service
    "readonly property bool formAllDay",  // all-day is derived, not a switch
    "component NotesField",    // notes get a five-line box, not a line
    "nlApplied",               // hand-typed parts survive an edit to the phrase
    "Model.mergeEntryDraft",   // and the rules for that live in Model, tested
    "suggest-address",         // the location row answers with whole addresses
    "handleLocationKey",       // and the list owns Enter/Esc while it is up
    "takeAddressSuggestion",
    "closeAddressSuggestions",
    "location-suggest.json"
  ]) {
    assert.ok(src.indexOf(needle) !== -1, "missing " + needle)
  }
  assert.ok(!/component HeroChip/.test(src), "the hero chips are still there")
  assert.ok(!/ToggleSwitch/.test(src), "the all-day switch is back")
  assert.ok(!/width: gridColumn\.width/.test(src.slice(src.indexOf("id: entryColumn"))),
    "entry rows still take their width off the month grid")
})

test("right-click opens the entry pane, not Thunderbird", () => {
  assert.match(src, /RightButton.*openEntry|openEntry\(modelData\.key\)/)
  assert.ok(!/RightButton.*newEventOn/.test(src), "right-click still calls newEventOn")
})

if (require.main === module || process.argv[1] && process.argv[1].endsWith("panel-static.test.js")) {
  // node --test prints its own summary; this token is for the gate.
}

process.on("exit", code => {
  if (code === 0) process.stdout.write("PANEL-STATIC-OK\n")
})
