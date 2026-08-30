const test = require("node:test")
const assert = require("node:assert/strict")
const Model = require("../Model.js")

const ROSTER = [
  { count: 1344, kind: "events", name: "Kalender", color: "#CEE7FFFF" },
  { color: "#6600cc", count: 301, kind: "events", name: "Work" },
  { count: 2, kind: "cards", name: "Gesammelte Adressen" },
  { count: 1, internal: true, kind: "events", name: "mailbox-habits" },
  { color: "#FF9500FF", count: 6, kind: "tasks", name: "Aufgaben" }
]

const AGENDA = [
  { all_day: true, calendar: "Work", date: "2026-08-31",
    end: "2026-09-01T00:00:00+02:00", id: 1469, recurring: false,
    start: "2026-08-31T00:00:00+02:00", summary: "Urlaub",
    time: "all day", uid: "all-day-1" },
  { all_day: false, calendar: "Work", date: "2026-08-31",
    end: "2026-08-31T09:00:00+02:00", id: 1488, location: "IILS",
    recurring: false, start: "2026-08-31T08:10:00+02:00",
    summary: "Standup https://meet.google.com/abc-defg-hij",
    time: "08:10–09:00", uid: "standup" }
]

const TODOS = [
  { done: false, id: 1796, list: "Aufgaben", summary: "read book", uid: "open-1" },
  { done: false, due: "2026-09-01", id: 1797, list: "Aufgaben",
    summary: "Wäsche", uid: "due-1" },
  { done: false, due: "2026-09-01 17:00", id: 1798, list: "Aufgaben",
    priority: "high", summary: "Abgabe", uid: "deadline-1" },
  { done: true, due: "2026-01-06", id: 1791, list: "Aufgaben",
    status: "COMPLETED", summary: "Pizza", uid: "done-1" }
]

test("the roster keeps event and task collections, never address books or internal ones", () => {
  const roster = Model.mailboxRoster(ROSTER)
  assert.deepEqual(roster.map(c => c.name), ["Kalender", "Work", "Aufgaben"])
  const work = roster.find(c => c.name === "Work")
  assert.equal(work.events, true)
  assert.equal(work.tasks, false)
})

test("the chooser offers calendars for an event and task lists for a todo", () => {
  const roster = Model.mailboxRoster(ROSTER)
  assert.deepEqual(Model.calendarOptions(roster, "event").map(o => o.value),
                   ["Kalender", "Work"])
  assert.deepEqual(Model.calendarOptions(roster, "task").map(o => o.value),
                   ["Aufgaben"])
})

test("an eight-digit colour loses its alpha, or QML paints orange violet", () => {
  const doc = Model.mailboxDocument(Model.mailboxRoster(ROSTER), AGENDA, [], 0)
  assert.equal(doc.events[0].color, "#6600cc")
})

test("agenda rows land on their day, with the time the day shows", () => {
  const doc = Model.mailboxDocument(Model.mailboxRoster(ROSTER), AGENDA, [], 0)
  const byUid = Object.fromEntries(doc.events.map(e => [e.id, e]))
  assert.equal(byUid["all-day-1"].dateKey, "2026-08-31")
  assert.equal(byUid["all-day-1"].start, "2026-08-31")
  assert.equal(byUid["all-day-1"].end, "2026-09-01")
  assert.equal(byUid["all-day-1"].time, "")
  assert.equal(byUid["standup"].time, "08:10")
  assert.equal(byUid["standup"].meetingUrl, "https://meet.google.com/abc-defg-hij")
})

test("tasks keep their day, their hour and their rank, and finished ones are gone", () => {
  const doc = Model.mailboxDocument(Model.mailboxRoster(ROSTER), [], TODOS, 0)
  assert.equal(doc.tasks.length, 3)
  const byUid = Object.fromEntries(doc.tasks.map(t => [t.uid, t]))
  assert.equal(byUid["due-1"].dueKey, "2026-09-01")
  // A due date is a day: no hour to show, so nothing is invented.
  assert.equal(byUid["due-1"].time, "")
  assert.equal(byUid["due-1"].allDay, true)
  assert.equal(byUid["due-1"].priority, "")
  // A deadline keeps the hour it was given, and the rank sorts the bucket.
  assert.equal(byUid["deadline-1"].dueKey, "2026-09-01")
  assert.equal(byUid["deadline-1"].time, "17:00")
  assert.equal(byUid["deadline-1"].allDay, false)
  assert.equal(byUid["deadline-1"].priority, "high")
  assert.equal(byUid["open-1"].dueKey, "")
  assert.ok(doc.tasks.every(t => !t.done))
})

test("the link on an agenda row is the one the entry carries", () => {
  const rows = [Object.assign({}, AGENDA[1], {
    summary: "Standup", url: "https://meet.example.org/r?a=1,2" })]
  const doc = Model.mailboxDocument(Model.mailboxRoster(ROSTER), rows, [], 0)
  assert.equal(doc.events[0].meetingUrl, "https://meet.example.org/r?a=1,2")
})

test("meeting links come from the title and the location, hosts first", () => {
  assert.equal(
    Model.meetingUrlIn("Standup https://meet.google.com/abc-defg-hij"),
    "https://meet.google.com/abc-defg-hij")
  assert.equal(
    Model.meetingUrlIn("join https://zoom.us/j/9", "see https://example.com/doc"),
    "https://zoom.us/j/9")
  assert.equal(Model.meetingUrlIn("read https://github.com/meet.the-team/notes"), "")
  assert.equal(
    Model.meetingUrlIn("join https://us02web.zoom.us/meeting/abc/ics?icsToken=tok"),
    "")
})

test("a wire request becomes the daemon's command", () => {
  assert.deepEqual(
    Model.requestToArgs({ kind: "task", title: "Wäsche", calendarName: "Aufgaben", dueMs: null }),
    { cmd: ["todo", "add"], args: { positional: "Wäsche", list: "Aufgaben" } })
  assert.deepEqual(
    Model.requestToArgs({ kind: "complete", id: 1796, done: false }),
    { cmd: ["todo", "undone"], args: { positional: "1796" } })
  const event = Model.requestToArgs({
    kind: "event", title: "Lunch", startMs: 1756634400000,
    endMs: 1756638000000, allDay: false, calendarName: "Work",
    location: "Cafe", description: "with Ana", link: "https://meet.google.com/abc" })
  assert.deepEqual(event, {
    cmd: ["event", "add"],
    args: {
      positional: "Lunch", start: "2025-08-31 12:00", end: "2025-08-31 13:00",
      calendar: "Work", location: "Cafe", notes: "with Ana",
      // A link is a link, not a line of the description.
      url: "https://meet.google.com/abc"
    }
  })
  const allDay = Model.requestToArgs({
    kind: "event", title: "Urlaub", startMs: 1756634400000,
    endMs: 1756807200000, allDay: true })
  assert.equal(allDay.args.start, "2025-08-31")
  assert.equal(allDay.args.end, "2025-09-02")
  assert.equal(allDay.args.all_day, true)
})

test("the rule, the reminder and the rank reach the daemon", () => {
  const repeating = Model.requestToArgs({
    kind: "event", title: "Standup", startMs: 1756634400000, endMs: 1756638000000,
    alertMinutes: 15, recurrence: { freq: "weekly", interval: 2 } })
  assert.equal(repeating.args.repeat, "FREQ=WEEKLY;INTERVAL=2")
  assert.equal(repeating.args.alarm, "15")

  // Every week is a rule without an interval, and a frequency nothing can read
  // back is no rule at all.
  assert.equal(Model.repeatRule({ freq: "daily", interval: 1 }), "FREQ=DAILY")
  assert.equal(Model.repeatRule({ freq: "fortnightly", interval: 1 }), "")
  assert.equal(Model.repeatRule(null), "")

  const timed = Model.requestToArgs({
    kind: "task", title: "Abgabe", dueMs: 1756654200000, dueHasTime: true, priority: 1 })
  assert.deepEqual(timed.args,
    { positional: "Abgabe", due: "2025-08-31 17:30", priority: "high" })

  // A due date without an hour stays a date: "by Friday" is not a promise
  // about midnight.
  const dated = Model.requestToArgs({ kind: "task", title: "Rechnung", dueMs: 1756634400000 })
  assert.equal(dated.args.due, "2025-08-31")
  assert.equal(dated.args.priority, undefined)
  assert.deepEqual([1, 5, 9, null].map(Model.priorityWord), ["high", "medium", "low", ""])
})

test("a tick names the todo by id, and an untick undoes it", () => {
  assert.deepEqual(
    Model.requestToArgs({ kind: "complete", id: 1796, done: true }),
    { cmd: ["todo", "done"], args: { positional: "1796" } })
  assert.deepEqual(
    Model.requestToArgs({ kind: "complete", id: 1796, done: false }),
    { cmd: ["todo", "undone"], args: { positional: "1796" } })
  assert.equal(Model.requestToArgs({ kind: "complete", done: true }), null)
})
