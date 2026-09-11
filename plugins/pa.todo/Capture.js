// Quick-capture line -> `mailbox todo add` argv, using the calendar widget's
// phrase parser so Super+N understands the same words its entry pane does
// ("buy milk friday !", "einkaufen morgen /Arbeit"). Model.js here is a
// verbatim copy of ../mailbox.clock/Model.js; tests/capture.test.js fails if
// the two drift. `Model` is passed in so this file needs no import of its
// own — QML hands it the namespace, node hands it the require().
//
// ponytail: whole Model.js copied in for ~5 functions. Re-copy on change;
// the drift test is the guard. Extract a shared parse module if a third
// caller ever needs it.

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
