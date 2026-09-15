// Quick-capture line -> `mailbox todo add` argv, using the calendar widget's
// phrase parser so Super+N understands the same words its entry pane does
// ("buy milk friday !", "einkaufen morgen /Arbeit"). Model.js here is the slice
// of ../mailbox.clock/Model.js this capture path calls, and tests/capture.test.js
// fails if a line of it is not the calendar widget's own. `Model` is passed in
// so this file needs no import of its own — QML hands it the namespace, node
// hands it the require().
//
// ponytail: a subset copied in, not a shared module — Omarchy installs each
// plugin on its own, so an import across plugins would break one of them. The
// drift test is the guard; extract a shared parse module if a third caller ever
// needs it.

function todoAddArgv(text, nowMs, Model) {
  var raw = String(text === undefined || text === null ? "" : text).trim()
  if (!raw) return null

  var now = isFinite(nowMs) ? nowMs : Date.now()
  var today = Model.keyForDate(new Date(now))

  var draft = Model.parseEventPhrase(raw, today, now, [])
  if (!draft) return ["todo", "add", raw]
  draft.kind = "task"

  var built = Model.buildQuickAddRequest(draft, now)
  if (!built || !built.ok) return ["todo", "add", draft.title || raw]

  var req = built.request
  // No date word in the line -> stay a loose todo, the way a bare Super+N
  // capture always has. A named day ("friday", "15.3.") rides along as --due.
  if (!Model.phraseHasRole(draft.segments, "date")) {
    req.dueMs = null
    req.dueHasTime = false
  }

  var mapped = Model.requestToArgs(req)
  if (!mapped || !mapped.cmd) return ["todo", "add", draft.title || raw]

  var argv = mapped.cmd.slice()
  var a = mapped.args || {}
  if (a.positional) argv.push(String(a.positional))
  if (a.due) argv.push("--due", String(a.due))
  if (a.priority) argv.push("--priority", String(a.priority))
  if (a.list) argv.push("--list", String(a.list))
  return argv
}

if (typeof module !== "undefined") module.exports = { todoAddArgv: todoAddArgv }
