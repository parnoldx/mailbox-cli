// Pure date and format math for the clock widget and its calendar panel.
// Everything here is locale- and Qt-free so it can be unit tested under node
// (test/shell.d/clock-test.sh); the QML owns month/weekday naming through
// Qt.locale().

var MS_PER_DAY = 86400000

// Weekday indices match both JS Date.getDay() and QML's Locale.Sunday…
// Locale.Saturday, so a locale's firstDayOfWeek can be passed straight in.
var WEEKDAY_NAMES = ["sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"]

// ---- Bar label formats. Right-clicking the clock walks these in order and
//      writes the result back to shell.json, so the label the bar shows and
//      the format the config stores are always the same thing.
//
// The locale-shaped time presets are each followed by their 12-hour twin, so
// the walk from a 24-hour label to the same label in AM/PM is a single right
// click rather than a lap of the ring. The ISO preset is deliberately left
// without one: ISO 8601 writes time on a 24-hour clock, so an AM/PM variant
// would contradict the only thing that format is for.
var CLOCK_FORMATS = [
  "dddd HH:mm",
  "dddd h:mm AP",
  "HH:mm",
  "h:mm AP",
  "ddd d MMM HH:mm",
  "ddd d MMM h:mm AP",
  "ddd d HH:mm",
  "ddd d h:mm AP",
  "d MMMM 'W'ww yyyy",
  "yyyy-MM-dd HH:mm"
]

// Vertical bars have room for a few stacked lines and nothing else, so the
// ring stays short. AM/PM costs a fourth line, which is why only the plain
// time carries it here.
var VERTICAL_CLOCK_FORMATS = [
  "HH\n—\nmm",
  "h\n—\nmm\nAP",
  "dd\nMMM\n'W'ww\n''yy",
  "HH\nmm"
]

function clockFormats(vertical) {
  return vertical ? VERTICAL_CLOCK_FORMATS.slice() : CLOCK_FORMATS.slice()
}

// The presets in a fixed order, plus the configured alternate and current
// format when they are something else. The order must not depend on which
// entry is current: cycling writes the result back to shell.json, and a ring
// that reshuffled itself around the current value would bounce between two
// entries instead of walking.
function clockFormatRing(configured, configuredAlt, presets) {
  var ring = []
  var candidates = (presets || []).concat([configuredAlt, configured])
  for (var i = 0; i < candidates.length; i++) {
    var format = String(candidates[i] === undefined || candidates[i] === null ? "" : candidates[i])
    if (format === "" || ring.indexOf(format) !== -1) continue
    ring.push(format)
  }
  return ring.length > 0 ? ring : ["HH:mm"]
}

// Next entry after `current`. An unknown current format (a hand-written one
// that is not in the ring) starts the walk at the top.
function nextClockFormat(ring, current) {
  if (!ring || ring.length === 0) return ""
  var index = ring.indexOf(String(current === undefined || current === null ? "" : current))
  return ring[(index + 1) % ring.length]
}

// Two-digit ISO week, substituted into a format's 'ww' token before Qt
// formats it -- Qt has no ISO week specifier of its own.
function isoWeekLiteral(year, month, day) {
  return pad2(isoWeek(year, month, day))
}

function pad2(value) {
  var n = Number(value)
  return (n < 10 ? "0" : "") + n
}

// Stable "yyyy-MM-dd" identity for a day, so a grid cell can be compared
// against today without dragging Date objects through bindings.
function dateKey(year, month, day) {
  return year + "-" + pad2(Number(month) + 1) + "-" + pad2(day)
}

function keyForDate(date) {
  return dateKey(date.getFullYear(), date.getMonth(), date.getDate())
}

function coerceWeekStart(value) {
  if (value === undefined || value === null) return null
  if (typeof value === "number")
    return isFinite(value) ? ((Math.round(value) % 7) + 7) % 7 : null

  var text = String(value).replace(/^\s+|\s+$/g, "").toLowerCase()
  if (text === "") return null

  for (var i = 0; i < WEEKDAY_NAMES.length; i++)
    if (WEEKDAY_NAMES[i] === text || WEEKDAY_NAMES[i].substr(0, 3) === text) return i

  var parsed = parseInt(text, 10)
  return isFinite(parsed) ? ((parsed % 7) + 7) % 7 : null
}

// Configured week start, falling back to the locale's own first day when
// the setting is missing or nonsense.
function normalizedWeekStart(value, fallback) {
  var configured = coerceWeekStart(value)
  if (configured !== null) return configured
  var fallbackStart = coerceWeekStart(fallback)
  return fallbackStart === null ? 1 : fallbackStart
}

function weekStartSettingName(index) {
  return WEEKDAY_NAMES[normalizedWeekStart(index, 1)]
}

// The toggle flips between the two conventions people actually switch
// between. A calendar configured to any other start (Saturday, say) is
// shown as-is and lands on Monday the first time it is toggled.
function toggledWeekStart(index) {
  return normalizedWeekStart(index, 1) === 1 ? 0 : 1
}

function weekdayOrder(weekStart) {
  var start = normalizedWeekStart(weekStart, 1)
  var out = []
  for (var i = 0; i < 7; i++) out.push((start + i) % 7)
  return out
}

// ISO-8601 week number: the week owning the Thursday of that date's
// Monday-based week. Mirrors the clock widget's 'ww' format token.
function isoWeek(year, month, day) {
  var date = new Date(Date.UTC(year, month, day))
  var weekday = date.getUTCDay() || 7
  date.setUTCDate(date.getUTCDate() + 4 - weekday)
  var yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1))
  return Math.ceil(((date.getTime() - yearStart.getTime()) / MS_PER_DAY + 1) / 7)
}

function dayOfYear(year, month, day) {
  return Math.round((Date.UTC(year, month, day) - Date.UTC(year, 0, 1)) / MS_PER_DAY) + 1
}

function daysInYear(year) {
  return dayOfYear(year, 11, 31)
}

// Share of the year already behind you: whole days completed over days in
// the year, so January 1 reads 0% and December 31 reads 100%.
function yearProgress(year, month, day) {
  var total = daysInYear(year)
  if (total <= 0) return 0
  return Math.max(0, Math.min(1, (dayOfYear(year, month, day) - 1) / total))
}

function yearProgressPercent(year, month, day) {
  return Math.round(yearProgress(year, month, day) * 100)
}

// Memento mori. The default span is a round number rather than anything from
// an actuarial table: the point of the bar is the reminder, not the
// arithmetic, and whoever wants a different number can say so.
var DEFAULT_LIFE_EXPECTANCY = 90

// A birth year rather than an age, so the bar keeps counting on its own
// instead of going stale the moment it is entered. 0 means "not set", which
// is also what a blank, malformed, future, or implausibly distant year means.
function parseBirthYear(value, currentYear) {
  var now = Math.round(Number(currentYear))
  if (!isFinite(now)) return 0
  var text = String(value === undefined || value === null ? "" : value).replace(/^\s+|\s+$/g, "")
  if (!/^\d{4}$/.test(text)) return 0
  var year = parseInt(text, 10)
  if (!isFinite(year) || year > now || year < now - 120) return 0
  return year
}

// Whole years, the way people say their age: born in 1979 makes you 47 for
// all of 2026, whichever side of your birthday today falls.
function ageFromBirthYear(birthYear, currentYear) {
  var born = parseBirthYear(birthYear, currentYear)
  if (born <= 0) return 0
  return Math.round(Number(currentYear)) - born
}

// 0 means "not set", which is also what a blank, negative, fractional, or
// absurd entry means — the life bar simply stays hidden.
function parseAge(value) {
  var text = String(value === undefined || value === null ? "" : value).replace(/^\s+|\s+$/g, "")
  if (!/^\d+$/.test(text)) return 0
  var years = parseInt(text, 10)
  if (!isFinite(years) || years <= 0 || years > 120) return 0
  return years
}

// Unset or nonsense falls back to the default rather than to zero, so the
// bar always has something to measure against.
function parseLifeExpectancy(value) {
  var text = String(value === undefined || value === null ? "" : value).replace(/^\s+|\s+$/g, "")
  if (!/^\d+$/.test(text)) return DEFAULT_LIFE_EXPECTANCY
  var years = parseInt(text, 10)
  if (!isFinite(years) || years <= 0 || years > 150) return DEFAULT_LIFE_EXPECTANCY
  return years
}

function lifeProgress(age, expectancy) {
  var years = parseAge(age)
  var span = parseLifeExpectancy(expectancy)
  if (years <= 0 || span <= 0) return 0
  return Math.max(0, Math.min(1, years / span))
}

function lifeProgressPercent(age, expectancy) {
  return Math.round(lifeProgress(age, expectancy) * 100)
}

// Always six rows of seven days. A fixed grid keeps the popup exactly the
// same height in every month, so stepping through the year never makes the
// panel jump under the pointer.
function monthGrid(year, month, weekStart, todayKey, eventIndex, taskIndex) {
  var start = normalizedWeekStart(weekStart, 1)
  var leading = (new Date(year, month, 1).getDay() - start + 7) % 7
  var cursor = new Date(year, month, 1 - leading)
  var today = String(todayKey || "")
  var weeks = []

  for (var w = 0; w < 6; w++) {
    var days = []
    var thursday = null
    for (var d = 0; d < 7; d++) {
      var cellYear = cursor.getFullYear()
      var cellMonth = cursor.getMonth()
      var cellDay = cursor.getDate()
      var weekday = cursor.getDay()
      var key = dateKey(cellYear, cellMonth, cellDay)
      if (weekday === 4) thursday = { year: cellYear, month: cellMonth, day: cellDay }
      days.push({
        key: key,
        year: cellYear,
        month: cellMonth,
        day: cellDay,
        weekday: weekday,
        inMonth: cellMonth === month && cellYear === year,
        weekend: weekday === 0 || weekday === 6,
        today: key === today,
        hasEvent: eventIndex ? !!eventIndex[key] : false,
        colors: eventIndex ? dayColors(eventIndex[key]) : [],
        hasTask: taskIndex ? hasOpenTask(taskIndex[key]) : false,
        taskColors: taskIndex ? taskColors(taskIndex[key]) : []
      })
      cursor.setDate(cursor.getDate() + 1)
    }
    // Number every row by the ISO week owning its Thursday. That is the
    // definition itself for Monday-start weeks, and the only answer that
    // stays stable for the other starts, where a row straddles two ISO
    // weeks but shares all of Monday through Thursday with one of them.
    var anchor = thursday || days[0]
    weeks.push({
      week: isoWeek(anchor.year, anchor.month, anchor.day),
      days: days
    })
  }
  return weeks
}

function stepMonth(year, month, delta) {
  var target = new Date(year, Number(month) + Number(delta), 1)
  return { year: target.getFullYear(), month: target.getMonth() }
}

function indexEventsByDate(events) {
  var index = {}
  if (!events || !events.length) return index
  for (var i = 0; i < events.length; i++) {
    var event = events[i]
    var key = event && event.dateKey
    if (!key) continue
    if (!index[key]) index[key] = []
    index[key].push(event)
  }
  return index
}

function eventsForDateKey(index, dateKey) {
  if (!index || !dateKey) return []
  var list = (index[dateKey] || []).slice()
  list.sort(function(a, b) {
    if (a.allDay && !b.allDay) return -1
    if (!a.allDay && b.allDay) return 1
    return String(a.start || a.time || "").localeCompare(String(b.start || b.time || ""))
  })
  return list
}

function dateFromKey(dateKey, fallback) {
  var parts = String(dateKey || "").split("-")
  if (parts.length !== 3) return fallback
  var year = parseInt(parts[0], 10)
  var month = parseInt(parts[1], 10)
  var day = parseInt(parts[2], 10)
  if (isNaN(year) || isNaN(month) || isNaN(day)) return fallback
  return new Date(year, month - 1, day)
}

function formatDateKey(dk) {
  var d = dateFromKey(dk, null)
  if (!d) return String(dk || "")
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
}

// ---- Tasks. A VTODO has at most a due date, and most have none at all, so
//      a month grid cannot place them the way it places events. Every task is
//      sorted into one of three places instead:
//
//        "day"  — due today or later: it sits on its due date, like an event
//        "week" — no due date, or due before today: it belongs to the loose
//                 pile of work for the current week
//        "done" — finished: it is drawn nowhere. Ticking a todo takes it off
//                 the panel, which is the whole reward for doing it; a struck
//                 line hanging around is just the list refusing to shrink.
//
//      "week" is the HEY calendar's "sometime this week", and the rollover
//      that carries an unfinished task into the next week is this function
//      and nothing else. Nothing is written back to the server: an overdue
//      task is *displayed* in the current week, every week, until it is
//      ticked off. That keeps the widget a reader of the calendar rather than
//      an editor of it, and there is no state to drift out of sync.
function taskPlacement(task, todayKey) {
  if (!task) return "week"
  if (task.done) return "done"
  var due = String(task.dueKey || "")
  if (!due) return "week"
  // Plain string compare: YYYY-MM-DD sorts chronologically.
  return due < String(todayKey || "") ? "week" : "day"
}

// Whole days between the due date and today, for the "2 days late" note on a
// rolled-forward task. 0 for anything not overdue.
function taskOverdueDays(task, todayKey) {
  if (!task || task.done || !task.dueKey) return 0
  var due = dateFromKey(task.dueKey)
  var today = dateFromKey(todayKey)
  if (!due || !today) return 0
  var days = Math.round((today.getTime() - due.getTime()) / MS_PER_DAY)
  return days > 0 ? days : 0
}

function tasksFromDocument(doc) {
  return doc && doc.tasks && doc.tasks.length ? doc.tasks : []
}

// Only the tasks that sit on a day of their own. The undated and the overdue
// are deliberately absent: they are drawn once, in the week bucket, rather
// than twice.
function indexTasksByDate(tasks, todayKey) {
  var index = {}
  if (!tasks || !tasks.length) return index
  for (var i = 0; i < tasks.length; i++) {
    var task = tasks[i]
    // Only the ones with a day of their own. The undated and the overdue are
    // drawn once, in the week bucket; the finished are not drawn at all.
    if (taskPlacement(task, todayKey) !== "day") continue
    var key = String(task.dueKey || "")
    if (!key) continue
    if (!index[key]) index[key] = []
    index[key].push(task)
  }
  return index
}

function tasksForDateKey(index, dateKey) {
  if (!index || !dateKey) return []
  var list = (index[dateKey] || []).slice()
  list.sort(function(a, b) {
    // By time of day, then by title. Everything here is open work.
    var byTime = String(a.time || "").localeCompare(String(b.time || ""))
    if (byTime !== 0) return byTime
    return String(a.title || "").localeCompare(String(b.title || ""))
  })
  return list
}

// Which week a date key falls in, named by the key of that week's first day.
// Takes the configured week start, so the answer always matches the row the
// day sits on in the grid rather than an ISO week that may split it.
function weekStartKeyOf(key, weekStart) {
  var date = dateFromKey(key, null)
  if (!date) return ""
  var start = normalizedWeekStart(weekStart, 1)
  var back = (date.getDay() - start + 7) % 7
  date.setDate(date.getDate() - back)
  return keyForDate(date)
}

// The bucket for one week of the grid.
//
// A loose task belongs to the week it was last carried into, which is the
// later of its own week and this one. And that is always this one: a task
// with a week of its own still ahead of it has a due date, which makes it a
// task with a day, drawn on that day. So the bucket has content on the
// current week and on no other — clicking into next week shows what is
// actually planned for it, not this week's leftovers showing up again under
// every row of the month.
function weekTasksFor(tasks, weekKey, todayKey, weekStart) {
  var wanted = String(weekKey || "")
  if (!wanted || weekStartKeyOf(todayKey, weekStart) !== wanted) return []
  return weekTasks(tasks, todayKey)
}

// The week bucket: everything with no day of its own, most overdue first so
// the oldest thing you have been avoiding is the one at the top.
function weekTasks(tasks, todayKey) {
  var list = []
  for (var i = 0; tasks && i < tasks.length; i++) {
    if (taskPlacement(tasks[i], todayKey) === "week") list.push(tasks[i])
  }
  list.sort(function(a, b) {
    var late = taskOverdueDays(b, todayKey) - taskOverdueDays(a, todayKey)
    if (late !== 0) return late
    var rank = { high: 0, medium: 1, low: 2 }
    var byPriority = (rank[a.priority] === undefined ? 3 : rank[a.priority])
      - (rank[b.priority] === undefined ? 3 : rank[b.priority])
    if (byPriority !== 0) return byPriority
    return String(a.title || "").localeCompare(String(b.title || ""))
  })
  return list
}

// The distinct calendar colours of the open tasks on one day, for the day
// cell's hollow rings. Finished tasks are left out: a ring is a thing still
// owed, and a day whose only task is done should read as clear.
function taskColors(tasks) {
  var colors = []
  for (var i = 0; tasks && i < tasks.length; i++) {
    if (tasks[i] && tasks[i].done) continue
    var color = eventColor(tasks[i])
    if (color && colors.indexOf(color) === -1) colors.push(color)
  }
  return colors
}

function hasOpenTask(tasks) {
  for (var i = 0; tasks && i < tasks.length; i++) {
    if (tasks[i] && !tasks[i].done) return true
  }
  return false
}

// What the widget hands `quick-add` to tick a task off. The mailbox needs
// the id the listing carried: todo done names it directly, and a uid would
// mean a lookup the CLI does not offer.
function buildCompleteRequest(task, nowMs) {
  if (!task || !task.id) return null
  return {
    kind: "complete",
    id: String(task.id || ""),
    uid: String(task.id || ""),
    calendarId: String(task.calendarId || ""),
    calendarName: String(task.calendarName || ""),
    done: !task.done,
    completedMs: isFinite(nowMs) ? Math.round(nowMs) : Date.now()
  }
}

function parseEventDocument(raw) {
  if (!raw) return null
  try {
    var parsed = JSON.parse(raw)
    if (parsed && parsed.version === 1) return parsed
  } catch (error) {
    return null
  }
  return null
}

var MINUTE_MS = 60 * 1000
var HOUR_MS = 60 * MINUTE_MS
var DAY_MS = 24 * HOUR_MS

function eventStartMs(event) {
  if (!event || !event.start) return NaN
  return Date.parse(event.start)
}

function eventEndMs(event) {
  if (!event) return NaN
  if (event.end) {
    var end = Date.parse(event.end)
    if (!isNaN(end)) return end
  }
  var start = eventStartMs(event)
  if (isNaN(start)) return NaN
  return start + HOUR_MS
}

// Timed events only. All-day rows stay on the calendar grid and never
// take the bar: they have no join link and no "in 4m" that would mean
// anything.
function isInProgress(event, nowMs) {
  if (!event || event.allDay) return false
  var start = eventStartMs(event)
  var end = eventEndMs(event)
  if (isNaN(start) || isNaN(end)) return false
  return nowMs >= start && nowMs < end
}

// Current meeting first, otherwise the soonest timed start still ahead.
// MeetingBar keeps the join target on the bar after the hour, so a click
// at 10:01 still opens the 10:00 standup rather than the 11:00 next.
function nextEvent(events, nowMs) {
  var current = null
  var currentStart = null
  var upcoming = null
  var upcomingStart = null

  for (var i = 0; i < (events || []).length; i++) {
    var event = events[i]
    if (!event || event.allDay) continue

    var startMs = eventStartMs(event)
    if (isNaN(startMs)) continue

    if (isInProgress(event, nowMs)) {
      if (currentStart === null || startMs < currentStart) {
        current = event
        currentStart = startMs
      }
      continue
    }

    if (startMs < nowMs) continue
    if (upcomingStart === null || startMs < upcomingStart) {
      upcoming = event
      upcomingStart = startMs
    }
  }

  return current || upcoming
}

function formatCountdown(deltaMs) {
  if (deltaMs === null || isNaN(deltaMs)) return null
  if (deltaMs >= DAY_MS || deltaMs < -DAY_MS) return null
  if (deltaMs < MINUTE_MS) return "now"

  var minutes = Math.floor(deltaMs / MINUTE_MS)
  if (minutes < 60) return "in " + minutes + "m"

  var hours = Math.floor(minutes / 60)
  var rest = minutes % 60
  return rest === 0 ? "in " + hours + "h" : "in " + hours + "h " + rest + "m"
}

// "starts in 5 minutes", then "starts now" in the last minute.
function formatStartsIn(deltaMs) {
  if (deltaMs === null || isNaN(deltaMs)) return ""
  if (deltaMs < MINUTE_MS) return "starts now"
  var minutes = Math.floor(deltaMs / MINUTE_MS)
  if (minutes <= 0) return "starts now"
  if (minutes === 1) return "starts in 1 minute"
  if (minutes < 60) return "starts in " + minutes + " minutes"
  var hours = Math.floor(minutes / 60)
  var rest = minutes % 60
  if (rest === 0) return hours === 1 ? "starts in 1 hour" : "starts in " + hours + " hours"
  return "starts in " + hours + "h " + rest + "m"
}

// Red only in the last minute, including "starts now".
function isImminent(deltaMs) {
  if (deltaMs === null || isNaN(deltaMs)) return false
  return deltaMs < MINUTE_MS
}

function joinButtonLabel(url) {
  var text = String(url || "").toLowerCase()
  if (text.indexOf("zoom.") !== -1) return "Join Zoom"
  if (text.indexOf("meet.google.") !== -1) return "Join Meet"
  if (text.indexOf("teams.") !== -1) return "Join Teams"
  if (text.indexOf("jit.si") !== -1 || text.indexOf("jitsi") !== -1) return "Join Jitsi"
  return "Join"
}

var MAX_ANNOUNCE_TITLE = 28

function truncateTitle(title, limit) {
  var text = String(title === undefined || title === null ? "" : title)
  var max = limit || MAX_ANNOUNCE_TITLE
  if (text.length <= max) return text
  return text.substring(0, max - 1).replace(/\s+$/, "") + "…"
}

function announceLabel(clockText, title, countdown, limit) {
  if (!countdown) return clockText
  var shown = truncateTitle(title, limit)
  if (!shown) return clockText
  return clockText + "  ·  " + shown + " " + countdown
}

function millisUntil(event, nowMs) {
  if (!event) return null
  var startMs = Date.parse(event.start)
  if (isNaN(startMs)) return null
  return startMs - nowMs
}

function shouldAnnounce(event, nowMs, leadMinutes, startedLeadMinutes) {
  if (!event) return false
  var delta = millisUntil(event, nowMs)
  if (delta === null) return false

  if (delta <= 0) {
    if (!isInProgress(event, nowMs)) return false
    var startedLead = startedLeadMinutes === undefined || startedLeadMinutes === null
      ? 5
      : Number(startedLeadMinutes)
    if (!isFinite(startedLead) || startedLead <= 0) return false
    return -delta <= startedLead * MINUTE_MS
  }

  var lead = Number(leadMinutes)
  if (!isFinite(lead) || lead <= 0) return false
  return delta <= lead * MINUTE_MS
}

// A join button left unclicked: the meeting is running, it still has a
// Join link, and between graceMs and windowMs has passed since it started
// without one. Past the window the meeting counts as deliberately skipped,
// not forgotten -- and a shell restarted hours into it stays quiet instead
// of punishing the restart. Fires on every tick while true; the widget
// supplies the once-per-occurrence memory so the sound plays a single time.
function shouldNudge(event, nowMs, graceMs, windowMs) {
  if (!event) return false
  if (meetingUrlFor(event) === "") return false
  if (!isInProgress(event, nowMs)) return false
  var start = eventStartMs(event)
  if (isNaN(start)) return false
  var grace = Number(graceMs)
  if (!isFinite(grace) || grace < 0) grace = MINUTE_MS
  var window = Number(windowMs)
  if (!isFinite(window) || window < grace) window = 5 * MINUTE_MS
  var late = nowMs - start
  return late >= grace && late <= window
}

function occurrenceKey(event) {
  if (!event) return ""
  var id = String(event.id || "")
  var start = String(event.start || "")
  if (!id && !start) return ""
  return id + "|" + start
}

function isDismissed(event, dismissedKey) {
  var key = occurrenceKey(event)
  return key !== "" && key === String(dismissedKey || "")
}

function joinTooltip(event) {
  var title = String(event && event.title ? event.title : "").replace(/^\s+|\s+$/g, "")
  return title ? "Join " + title : "Join meeting"
}

function eventDisplayTime(event) {
  if (!event) return ""
  if (event.allDay) return "All day"
  if (event.time) return event.time
  if (!event.start) return ""
  var start = new Date(event.start)
  if (isNaN(start.getTime())) return ""
  var h = start.getHours()
  var m = start.getMinutes()
  return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m
}

// Only https is ever launched. A meeting link is supplied by whoever sent
// the invitation, so treating it as trusted input would be a mistake.
function safeUrl(url) {
  var text = String(url || "").trim()
  // http as well as https — the entry pane lets a link be typed by hand, and
  // refusing to open what it just stored would be the odder of the two. Every
  // other scheme stays out: this string is handed to a browser.
  if (!/^https?:\/\//i.test(text)) return ""
  if (/[\s"'<>]/.test(text)) return ""
  return text
}

// The server keeps a colour per calendar and the sync script carries it onto
// every event, so a work event and a personal one on the same day can be told
// apart at a glance instead of both reading as the theme accent. Validated
// rather than trusted: the value comes out of the profile prefs, and QML turns
// anything it cannot parse into black rather than falling back.
function safeColor(value) {
  var text = String(value || "").trim()
  return /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(text) ? text : ""
}

// A link to store on an item. Wider than safeUrl — which guards what we hand
// to a browser — because http links are worth keeping in the calendar even
// though the join button will not launch them.
function safeLinkUrl(url) {
  var text = String(url || "").trim()
  if (!/^https?:\/\//i.test(text)) return ""
  if (/[\s"'<>]/.test(text)) return ""
  if (text.length > 2000) return ""
  return text
}

// What to call a meeting link in one word, for the pill that holds it.
function linkProviderLabel(url) {
  var text = String(url || "").toLowerCase()
  if (!text) return "Link"
  if (text.indexOf("zoom.") !== -1) return "Zoom"
  if (text.indexOf("meet.google.") !== -1) return "Meet"
  if (text.indexOf("teams.") !== -1) return "Teams"
  if (text.indexOf("jitsi") !== -1) return "Jitsi"
  if (text.indexOf("whereby.") !== -1) return "Whereby"
  if (text.indexOf("webex.") !== -1) return "Webex"
  return "Link"
}

function eventColor(event) {
  return event ? safeColor(event.color) : ""
}

// The distinct calendar colours on one day, in the order their first event
// falls. A day with three events out of one calendar gets one dot; a day
// split across two calendars gets two.
function dayColors(events) {
  var colors = []
  for (var i = 0; events && i < events.length; i++) {
    var color = eventColor(events[i])
    if (color && colors.indexOf(color) === -1) colors.push(color)
  }
  return colors
}

function isCalendarFileUrl(url) {
  var text = String(url || "")
  return /\/ics(\?|$)/i.test(text) || /\.ics(\?|$)/i.test(text) || /icsToken=/i.test(text)
}

function meetingUrlFor(event) {
  var text = event ? safeUrl(event.meetingUrl) : ""
  if (!text || isCalendarFileUrl(text)) return ""
  return text
}

// ---- Natural-language quick-add. Bilingual by table, not by setting: EN
//      and DE keywords are matched simultaneously, so "lunch mit Ana morgen
//      12:30" parses the same way either language wins a token. The start
//      day is never parsed from the text — it comes from the day the user
//      right-clicked — so the grammar only adds times, ends and flags on top
//      of it.
var NL_WEEKDAYS = [
  { index: 0, names: ["sunday", "sonntag", "so."] },
  { index: 1, names: ["monday", "montag", "mo."] },
  { index: 2, names: ["tuesday", "dienstag", "di."] },
  { index: 3, names: ["wednesday", "mittwoch", "mi."] },
  { index: 4, names: ["thursday", "donnerstag", "do."] },
  { index: 5, names: ["friday", "freitag", "fr."] },
  { index: 6, names: ["saturday", "samstag", "sonnabend", "sa."] }
]

var NL_WORDS = {
  today: ["today", "heute"],
  tomorrow: ["tomorrow", "morgen"],
  next: ["next", "nächste", "nächsten", "nächster", "kommende", "kommenden", "kommender"],
  till: ["till", "until", "bis"],
  for: ["for", "für"],
  place: ["at", "bei", "beim"]
}

function nlIs(word, group) {
  return NL_WORDS[group].indexOf(String(word || "").toLowerCase()) !== -1
}

function nlWeekdayIndex(word) {
  var text = String(word || "").toLowerCase()
  for (var i = 0; i < NL_WEEKDAYS.length; i++)
    if (NL_WEEKDAYS[i].names.indexOf(text) !== -1) return NL_WEEKDAYS[i].index
  return -1
}

// Strict times need an anchor — a colon, an am/pm suffix, a following
// "uhr", or the noon words — so a bare "15" stays part of the title.
function nlParseStrictTime(tokens, i) {
  var token = String(tokens[i] || "").toLowerCase().replace(/[,.;]$/, "")
  if (token === "noon" || token === "mittag") return { minutes: 12 * 60, used: 1 }

  var match = /^(\d{1,2})(?:[:.](\d{2}))?(am|pm)?$/.exec(token)
  if (!match) return null
  var hours = parseInt(match[1], 10)
  var minutes = match[2] ? parseInt(match[2], 10) : 0
  var meridiem = match[3] || ""
  var anchored = !!match[2] || !!meridiem

  var used = 1
  if (!anchored && i + 1 < tokens.length && String(tokens[i + 1]).toLowerCase() === "uhr") {
    anchored = true
    used = 2
  } else if (!anchored && !meridiem && token.indexOf("uhr") === token.length - 3 && /\d+uhr/.test(token)) {
    // "10uhr" written as one word.
    anchored = true
    hours = parseInt(/^(\d+)/.exec(token)[1], 10)
  }
  if (!anchored) return null

  if (meridiem === "pm" && hours < 12) hours += 12
  if (meridiem === "am" && hours === 12) hours = 0
  if (hours > 24 || minutes > 59) return null
  return { minutes: hours * 60 + minutes, used: used }
}

// Loose time: accepts what the strict matcher does plus bare "15 30"
// pairs? No — deliberately not. Loose here only means "hh:mm or hh.mm",
// used inside ranges where the dashes already signal intent.
function nlParseRangeTime(text) {
  var match = /^(\d{1,2})[:.]?(\d{2})$/.exec(String(text || ""))
  if (!match) return null
  var hours = parseInt(match[1], 10)
  var minutes = parseInt(match[2], 10)
  if (hours > 23 || minutes > 59) return null
  return hours * 60 + minutes
}

function nlTimeLabel(minutesOfDay) {
  return pad2(Math.floor(minutesOfDay / 60)) + ":" + pad2(minutesOfDay % 60)
}

// One side of a range: bare hour or hh:mm, minutes optional. Used for
// "12-13", "12 to 13" and friends — deliberately looser than the strict
// time matcher, because the range dash or connector supplies the anchor.
function nlRangeSide(text) {
  var match = /^(\d{1,2})(?:[:.](\d{2}))?$/.exec(String(text || "").toLowerCase())
  if (!match) return null
  var hours = parseInt(match[1], 10)
  var minutes = match[2] ? parseInt(match[2], 10) : 0
  if (hours > 23 || minutes > 59) return null
  return hours * 60 + minutes
}

// Words that join two times into a range: "to", plus the till family,
// which doubles as an end marker when no start time precedes it.
function nlConnector(text) {
  var token = String(text || "").toLowerCase().replace(/[,.;]$/, "")
  return token === "to" || nlIs(token, "till") ? token : null
}

// Next occurrence of a weekday on or after the base day (same day counts:
// "standup friday" clicked on friday means today).
function nlNextWeekday(base, weekday) {
  var d = new Date(base.getFullYear(), base.getMonth(), base.getDate())
  d.setDate(d.getDate() + ((weekday - d.getDay() + 7) % 7))
  return d
}

function nlExplicitDate(text, baseYear) {
  var iso = /^(20\d{2})-(\d{1,2})-(\d{1,2})$/.exec(text)
  if (iso) return { date: new Date(parseInt(iso[1], 10), parseInt(iso[2], 10) - 1, parseInt(iso[3], 10)), yearless: false }
  // German-style day.month, with or without a trailing dot, with optional year:
  // "15.3" / "15.3." / "15.3.2026". The trailing-dot strip on tokens leaves "15.3".
  var dm = /^(\d{1,2})\.(\d{1,2})\.?(?:(20\d{2}))?\.?$/.exec(text)
  if (dm) return {
    date: new Date(dm[3] ? parseInt(dm[3], 10) : baseYear, parseInt(dm[2], 10) - 1, parseInt(dm[1], 10)),
    yearless: !dm[3]
  }
  return null
}

function emptyDraft(kind, dayKey) {
  return {
    kind: kind,
    title: "",
    dateKey: String(dayKey || ""),
    endDateKey: null,
    startTime: null,
    endTime: null,
    endNextDay: false,
    durationMinutes: null,
    allDay: kind !== "task",
    location: null,
    description: null,
    calendarName: null,
    alertMinutes: null,
    recurrence: null,
    priority: null,
    link: null,
    segments: []
  }
}

// Everything the natural-language path can express, in one flat object the
// form reads and edits. `knownCalendars` (from the mirror's own list) lets
// bare "in Work" resolve without the slash-flag syntax.
function parseEventPhrase(text, dayKey, nowMs, knownCalendars) {
  var raw = String(text === undefined || text === null ? "" : text).replace(/^\s+|\s+$/g, "")
  if (!raw) return null
  var now = new Date(isFinite(nowMs) ? nowMs : Date.now())
  var base = dateFromKey(dayKey, null) || new Date(now.getFullYear(), now.getMonth(), now.getDate())

  var draft = emptyDraft("event", keyForDate(base))
  var known = []
  for (var k = 0; knownCalendars && k < knownCalendars.length; k++)
    known.push(String(knownCalendars[k] || "").toLowerCase())

  // Every token keeps its offset in `raw` and the role it ends up playing,
  // so the entry field can paint the phrase in the colours of the parts it
  // understood while it is being typed. `mark` takes positions in `rest`
  // (the post-flag token list) and writes the role back to the offsets.
  var spans = []
  var scan = /\S+/g
  var found
  while ((found = scan.exec(raw)) !== null)
    spans.push({ start: found.index, end: found.index + found[0].length, role: null })

  var rest = []
  var restIndex = []

  function mark(restFrom, restTo, role) {
    for (var s = restFrom; s <= restTo && s < restIndex.length; s++)
      if (s >= 0) spans[restIndex[s]].role = role
  }

  // ---- Flags first: /calendar, -aN[mhd], -rN[dwmy], trailing !{1,3}.
  var tokens = raw.split(/\s+/)
  for (var t = 0; t < tokens.length; t++) {
    var token = tokens[t]
    var flag
    if ((flag = /^\/(.+)$/.exec(token))) {
      draft.calendarName = flag[1].replace(/[,.;]$/, "")
      spans[t].role = "calendar"
      continue
    }
    if ((flag = /^-a(\d+)([mhd])$/i.exec(token))) {
      var factor = flag[2].toLowerCase() === "d" ? 1440 : flag[2].toLowerCase() === "h" ? 60 : 1
      draft.alertMinutes = parseInt(flag[1], 10) * factor
      spans[t].role = "alert"
      continue
    }
    if ((flag = /^-r(\d+)([dwmy])$/i.exec(token))) {
      var unit = flag[2].toLowerCase()
      draft.recurrence = {
        freq: unit === "d" ? "daily" : unit === "w" ? "weekly" : unit === "m" ? "monthly" : "yearly",
        interval: parseInt(flag[1], 10)
      }
      spans[t].role = "repeat"
      continue
    }
    // A pasted meeting link is a part of its own: it never belongs in the
    // title, and the bar's join button reads it back off the created event.
    if (/^https?:\/\//i.test(token)) {
      var link = safeLinkUrl(token.replace(/[),.;]+$/, ""))
      if (link) {
        draft.link = link
        spans[t].role = "link"
        continue
      }
    }
    // Duration flag from the reference quick-add: "-120" is two hours,
    // "-90m" and "-2h" say the same thing with the unit spelled out.
    if ((flag = /^-(\d+)(m|min|h|std)?$/i.exec(token))) {
      var durationUnit = (flag[2] || "m").toLowerCase().charAt(0)
      draft.durationMinutes = parseInt(flag[1], 10) * (durationUnit === "h" ? 60 : 1)
      spans[t].role = "duration"
      continue
    }
    if (/^!{1,3}$/.test(token)) {
      draft.priority = token.length === 1 ? "low" : token.length === 2 ? "medium" : "high"
      spans[t].role = "priority"
      continue
    }
    rest.push(token)
    restIndex.push(t)
  }

  // ---- Walk the remainder for temporal expressions. Anything recognized
  //      is consumed; what survives becomes title/location.
  var kept = []
  var keptIndex = []
  var pendingNext = false
  var pendingIn = false
  var sawTime = false
  var locFrom = -1
  var locWord = ""
  var locWordIndex = -1
  for (var p = 0; p < rest.length; p++) {
    var word = rest[p]
    var lower = word.toLowerCase().replace(/[,.;]$/, "")

    // Stray "uhr" left behind by a consumed range ("12 bis 13 Uhr").
    if (lower === "uhr" && sawTime) { mark(p, p, "time"); continue }

    // Prepositions lean on the time that follows them: before a real time
    // or range ("um 15 Uhr", "um 12-13") they simply step aside; before a
    // bare hour ("um 8", "von 12") they anchor it, since a lone "8" would
    // otherwise stay part of the title.
    if ((lower === "um" || lower === "at" || lower === "von" || lower === "from") &&
        p + 1 < rest.length) {
      var nxt = String(rest[p + 1]).toLowerCase().replace(/[,.;]$/, "")
      if (/\d[-–]\d/.test(nxt) || nlParseStrictTime(rest, p + 1) !== null) { mark(p, p, "time"); continue }
      var bareHour = nlRangeSide(nxt)
      if (bareHour !== null) {
        draft.startTime = nlTimeLabel(bareHour)
        draft.allDay = false
        sawTime = true
        mark(p, p + 1, "time")
        p += 1
        continue
      }
    }

    // ---- Place keyword. An "at"/"bei" that no time followed (the block
    //      above would have taken it) names a location, and the word itself
    //      is consumed: "Essen at Garbe Biegarten" is titled "Essen", not
    //      "Essen at". Only the marker is fixed here — what the place is
    //      settles below, so a date may still trail it ("bei Anna morgen").
    if (locFrom === -1 && nlIs(lower, "place") && p + 1 < rest.length) {
      locFrom = kept.length
      locWord = word
      locWordIndex = restIndex[p]
      continue
    }

    // Calendar by bare name after "in": only fires on names the mirror
    // actually has, so "12:30 in Café Central" stays a location.
    if ((pendingIn || lower === "in" || lower === "im") && known.length > 0) {
      var candidate = lower === "in" || lower === "im" ? (rest[p + 1] || "") : word
      var probe = String(candidate).toLowerCase()
      var hit = -1
      for (var n = 0; n < known.length; n++) {
        if (known[n] === probe || (probe.length >= 2 && known[n].indexOf(probe) === 0)) { hit = n; break }
      }
      if (hit !== -1) {
        draft.calendarName = knownCalendars[hit]
        mark(p, lower === "in" || lower === "im" ? p + 1 : p, "calendar")
        if (lower === "in" || lower === "im") p += 1
        pendingIn = false
        continue
      }
      if (lower === "in" || lower === "im") pendingIn = true
    } else if (lower === "in" || lower === "im") {
      pendingIn = true
    }

    if (nlIs(lower, "next")) { pendingNext = true; mark(p, p, "date"); continue }

    if (nlIs(lower, "today") || nlIs(lower, "tomorrow")) {
      var d = new Date(base.getFullYear(), base.getMonth(), base.getDate())
      if (nlIs(lower, "tomorrow")) d.setDate(d.getDate() + 1)
      draft.dateKey = keyForDate(d)
      pendingNext = false
      mark(p, p, "date")
      continue
    }

    var weekday = nlWeekdayIndex(lower)
    if (weekday !== -1) {
      var wd = nlNextWeekday(base, weekday)
      if (pendingNext) wd.setDate(wd.getDate() + 7)
      draft.dateKey = keyForDate(wd)
      pendingNext = false
      mark(p, p, "date")
      continue
    }

    var explicit = nlExplicitDate(lower, base.getFullYear())
    if (explicit && !isNaN(explicit.date.getTime())) {
      var edate = explicit.date
      // "15.3." said in August means the March that is coming, not the one
      // that went. Only yearless dates roll forward; ISO means what it says.
      if (explicit.yearless && edate.getTime() < new Date(base.getFullYear(), base.getMonth(), base.getDate()).getTime())
        edate.setFullYear(edate.getFullYear() + 1)
      if (pendingNext) edate.setDate(edate.getDate() + 7)
      draft.dateKey = keyForDate(edate)
      pendingNext = false
      mark(p, p, "date")
      continue
    }

    // Ranges. Connector form first: "12 to 13" / "12 bis 13 Uhr" with the
    // start side on this token, and "… 9am to 5pm" when the start was
    // consumed earlier and only the connector remains.
    var conn = nlConnector(lower)
    if (conn && sawTime && p + 1 < rest.length) {
      var toMin = nlRangeSide(String(rest[p + 1]).toLowerCase())
      if (toMin === null) {
        var toTime = nlParseStrictTime(rest, p + 1)
        if (toTime) toMin = toTime.minutes
      }
      if (toMin !== null) {
        draft.endTime = nlTimeLabel(toMin)
        draft.endNextDay = toMin <= (nlRangeSide(draft.startTime) || 0)
        draft.allDay = false
        mark(p, p + 1, "time")
        p += 1
        continue
      }
    }
    var fromMin = nlRangeSide(lower)
    if (fromMin !== null && p + 2 < rest.length && nlConnector(rest[p + 1])) {
      var otherMin = nlRangeSide(String(rest[p + 2]).toLowerCase().replace(/[,.;]$/, ""))
      if (otherMin === null) {
        var otherTime = nlParseStrictTime(rest, p + 2)
        if (otherTime) otherMin = otherTime.minutes
      }
      if (otherMin !== null) {
        draft.startTime = nlTimeLabel(fromMin)
        draft.endTime = nlTimeLabel(otherMin)
        draft.endNextDay = otherMin <= fromMin
        draft.allDay = false
        sawTime = true
        mark(p, p + 2, "time")
        p += 2
        continue
      }
    }
    // Dash shorthand "22:00-02:00" (also tolerates "22.00-02.00" and bare
    // hours: "12-13"); same midnight-wrap rule as the connector form.
    var dashParts = lower.split(/[-–]/)
    if (dashParts.length === 2) {
      var dFrom = nlRangeSide(dashParts[0])
      var dTo = nlRangeSide(dashParts[1])
      if (dFrom !== null && dTo !== null) {
        draft.startTime = nlTimeLabel(dFrom)
        draft.endTime = nlTimeLabel(dTo)
        draft.endNextDay = dTo <= dFrom
        draft.allDay = false
        sawTime = true
        mark(p, p, "time")
        continue
      }
    }

    if (nlIs(lower, "till")) {
      var q = p + 1
      var endDate = null
      if (q < rest.length) {
        var tillLower = String(rest[q]).toLowerCase().replace(/[,.;]$/, "")
        if (nlIs(tillLower, "today") || nlIs(tillLower, "tomorrow")) {
          endDate = new Date(base.getFullYear(), base.getMonth(), base.getDate())
          if (nlIs(tillLower, "tomorrow")) endDate.setDate(endDate.getDate() + 1)
          q += 1
        } else {
          var tw = nlWeekdayIndex(tillLower)
          if (tw !== -1) { endDate = nlNextWeekday(base, tw); q += 1 }
          else {
            var tillExplicit = nlExplicitDate(tillLower, base.getFullYear())
            if (tillExplicit && !isNaN(tillExplicit.date.getTime())) {
              endDate = tillExplicit.date
              if (tillExplicit.yearless && endDate.getTime() < new Date(base.getFullYear(), base.getMonth(), base.getDate()).getTime())
                endDate.setFullYear(endDate.getFullYear() + 1)
              q += 1
            }
          }
        }
      }
      var tillTime = q < rest.length ? nlParseStrictTime(rest, q) : null
      if (!tillTime && q < rest.length && /^\d{1,2}$/.test(String(rest[q]).replace(/[,.;]$/, ""))) {
        // Relaxed just here: "bis 11" / "till 18" name an end hour without
        // needing "uhr", where a bare number anywhere else stays title.
        var tillHour = parseInt(rest[q], 10)
        if (tillHour >= 0 && tillHour <= 24) tillTime = { minutes: (tillHour % 24) * 60, used: 1 }
      }
      if (tillTime) {
        draft.endTime = nlTimeLabel(tillTime.minutes)
        q += tillTime.used
        if (endDate) {
          draft.endDateKey = keyForDate(endDate)
          draft.endNextDay = false
        } else {
          draft.endDateKey = null
          draft.endNextDay = !sawTime || draft.endTime <= (draft.startTime || "")
        }
        draft.allDay = false
        mark(p, q - 1, "time")
        p = q - 1
        continue
      }
      if (endDate) {
        draft.endDateKey = keyForDate(endDate)
        mark(p, q - 1, "date")
        p = q - 1
        continue
      }
      // Nothing consumable after "till" — let the word fall into the title
      // rather than silently eating it.
    }

    if (nlIs(lower, "for") && p + 1 < rest.length) {
      var durTok = String(rest[p + 1]).toLowerCase()
      var dur = /^(\d+)(h|m|min|hrs?|std)?$/.exec(durTok)
      var durUnit = dur && dur[2] ? dur[2] : ""
      var durUsed = 1
      if (dur && !durUnit && p + 2 < rest.length && /^(h|m|min|hrs?|std)$/i.test(String(rest[p + 2]))) {
        durUnit = String(rest[p + 2]).toLowerCase()
        durUsed = 2
      }
      if (dur && durUnit) {
        var unitChar = durUnit.charAt(0)
        draft.durationMinutes = parseInt(dur[1], 10) * (unitChar === "h" ? 60 : 1)
        if (draft.startTime || sawTime) draft.allDay = false
        mark(p, p + durUsed, "duration")
        p += durUsed
        continue
      }
    }

    var time = nlParseStrictTime(rest, p)
    if (time) {
      // A noon word reads as a sensible title word too ("mittag", "lunch"):
      // take its time, but let the word survive into the title.
      if (/^(noon|mittag)[,.;]?$/.test(String(rest[p]).toLowerCase())) {
        kept.push(word)
        keptIndex.push(restIndex[p])
      }
      draft.startTime = nlTimeLabel(time.minutes)
      draft.allDay = false
      sawTime = true
      mark(p, p + time.used - 1, "time")
      p += time.used - 1
      continue
    }

    kept.push(word)
    keptIndex.push(restIndex[p])
    if (pendingNext) pendingNext = false
  }

  // A place keyword only holds if words survived after it; otherwise the
  // word goes back where it stood and the tail heuristic decides as usual.
  if (locFrom !== -1 && kept.length > locFrom) {
    draft.location = kept.slice(locFrom).join(" ").replace(/[,.;]$/, "")
    spans[locWordIndex].role = "location"
    for (var lk = locFrom; lk < keptIndex.length; lk++) spans[keptIndex[lk]].role = "location"
    kept = kept.slice(0, locFrom)
    keptIndex = keptIndex.slice(0, locFrom)
  } else if (locFrom !== -1) {
    kept.splice(locFrom, 0, locWord)
    keptIndex.splice(locFrom, 0, locWordIndex)
  }

  // ---- Title vs. location. Names travel with their preposition ("lunch
  //      with Ana" is a title), longer capitalized tails are places
  //      ("12:30 Café Central"). A lone capitalized word only counts as a
  //      location when something stands before it.
  // Only the name sitting on a preposition is protected ("with Sarah").
  // A later capitalized tail is the location ("Café Central").
  var PROTECT = { with: 1, mit: 1, to: 1, zu: 1, nach: 1, von: 1, from: 1 }
  var guarded = []
  for (var g = 0; g < kept.length; g++) {
    var gl = String(kept[g]).toLowerCase().replace(/[,.;]$/, "")
    var prevPrep = g > 0 && PROTECT[String(kept[g - 1]).toLowerCase().replace(/[,.;]$/, "")]
    guarded.push(PROTECT[gl] ? 1 : (prevPrep && /^[A-ZÄÖÜ]/.test(kept[g]) ? 1 : 0))
  }
  var locStart = -1
  var cursor = kept.length - 1
  while (cursor >= 0 && /^[A-ZÄÖÜ]/.test(kept[cursor]) && !guarded[cursor]) {
    locStart = cursor
    cursor -= 1
  }
  if (!draft.location && locStart !== -1 && (cursor + 1 < locStart || locStart > 0)) {
    draft.location = kept.slice(locStart).join(" ").replace(/[,.;]$/, "")
    for (var lm = locStart; lm < keptIndex.length; lm++) spans[keptIndex[lm]].role = "location"
    kept = kept.slice(0, locStart)
    keptIndex = keptIndex.slice(0, locStart)
  }
  for (var km = 0; km < keptIndex.length; km++)
    if (!spans[keptIndex[km]].role) spans[keptIndex[km]].role = "title"

  draft.title = kept.join(" ").replace(/^\s+|\s+$/g, "")
  if (!draft.title) {
    // Never dead-end: whatever could not be parsed becomes the title whole.
    draft.title = raw.replace(/\s*\/[^ ]+|\s*-a\d+[mhd]|\s*-r\d+[dwmy]/gi, "").replace(/^\s+|\s+$/g, "")
  }
  if (!draft.title) draft.title = raw
  draft.segments = mergeSegments(spans)
  return draft
}

// Adjacent tokens playing the same role read as one coloured run, gaps
// included — "next monday" is one date, not two words that happen to agree.
function mergeSegments(spans) {
  var out = []
  for (var i = 0; i < spans.length; i++) {
    var role = spans[i].role || "title"
    var last = out.length ? out[out.length - 1] : null
    if (last && last.role === role) last.end = spans[i].end
    else out.push({ start: spans[i].start, end: spans[i].end, role: role })
  }
  return out
}

// A draft for text the parser cannot improve on: raw text as title, clicked
// day as date, everything else default. Quick-add must never dead-end.
function fallbackDraft(text, dayKey, kind) {
  var draft = emptyDraft(kind === "task" ? "task" : "event", dayKey)
  draft.title = String(text === undefined || text === null ? "" : text).replace(/^\s+|\s+$/g, "")
  draft.segments = draft.title ? [{ start: 0, end: draft.title.length, role: "title" }] : []
  return draft
}

// What the entry pane shows first when a day-list row is clicked open for
// editing: the fields already known from the agenda row it was drawn from.
// It is missing the description, link, alarms and repeat rule -- those live
// only on the full object -- so the caller follows up with `event view` and
// applies a second, fuller draft on top once that answers.
function draftFromAgendaEvent(event) {
  var draft = emptyDraft("event", (event && event.dateKey) || "")
  if (!event) return draft
  draft.title = event.title || ""
  draft.calendarName = event.calendarName || null
  draft.location = event.location || null
  draft.allDay = !!event.allDay
  draft.link = event.meetingUrl || null
  var endStr = String(event.end || "")
  if (!event.allDay) {
    draft.startTime = event.time || null
    draft.endTime = endStr.length > 10 ? endStr.slice(11, 16) : null
    var endDay = endStr.slice(0, 10)
    // endNextDay is buildQuickAddRequest's shorthand for "wrap to the next
    // calendar day" when there is no explicit end date; an explicit
    // endDateKey already says which day, so setting both would add the
    // wrap twice.
    if (endDay && endDay !== draft.dateKey) draft.endDateKey = endDay
  } else if (endStr) {
    // The wire end date is exclusive (the day after the last day the event
    // covers); the form wants the last day itself, the way a person would
    // read "through the 12th".
    var lastDay = dateFromKey(endStr.slice(0, 10), null)
    if (lastDay) {
      lastDay.setDate(lastDay.getDate() - 1)
      var lastKey = keyForDate(lastDay)
      if (lastKey !== draft.dateKey) draft.endDateKey = lastKey
    }
  }
  return draft
}

// The rest of draftFromAgendaEvent's picture, once `event view` answers:
// the fields that never made it into an agenda row.
function draftFromEventDetail(detail, dayKey) {
  var draft = emptyDraft("event", dayKey || "")
  if (!detail) return draft
  draft.description = detail.description || null
  draft.link = safeLinkUrl(detail.url) || null
  if (detail.alarms && detail.alarms.length) draft.alertMinutes = Number(detail.alarms[0]) || null
  if (detail.repeat) {
    var rule = parseRepeatRuleForForm(detail.repeat)
    if (rule) draft.recurrence = rule
  }
  return draft
}

// The daemon hands back an RRULE; the form's recurrence pill only knows
// FREQ and INTERVAL, so anything richer (BYDAY and the like) is left for
// the raw rule to keep meaning rather than guessed at.
function parseRepeatRuleForForm(rrule) {
  var freqMatch = /FREQ=([A-Z]+)/.exec(String(rrule || ""))
  if (!freqMatch) return null
  var freq = freqMatch[1].toLowerCase()
  if (["daily", "weekly", "monthly", "yearly"].indexOf(freq) === -1) return null
  var intervalMatch = /INTERVAL=(\d+)/.exec(rrule)
  return { freq: freq, interval: intervalMatch ? parseInt(intervalMatch[1], 10) || 1 : 1 }
}

var NL_MAX_EPOCH_DAYS = 2 * 366

function nlValidMs(ms, nowMs) {
  if (typeof ms !== "number" || !isFinite(ms)) return false
  return Math.abs(ms - nowMs) <= NL_MAX_EPOCH_DAYS * DAY_MS
}

function nlTimeToMinutes(value) {
  var match = /^(\d{1,2}):(\d{2})$/.exec(String(value || ""))
  if (!match) return null
  var hours = parseInt(match[1], 10)
  var minutes = parseInt(match[2], 10)
  if (hours > 23 || minutes > 59) return null
  return hours * 60 + minutes
}

function nlMsFor(dateKeyStr, timeLabel) {
  var d = dateFromKey(dateKeyStr, null)
  if (!d) return NaN
  if (timeLabel === null || timeLabel === undefined) {
    return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
  }
  var minutes = nlTimeToMinutes(timeLabel)
  if (minutes === null) return NaN
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() + minutes * MINUTE_MS
}

// Turn an editable draft into the wire object quick-add drops
// for the add-on. Returns {ok:true, request} or {ok:false, error}. All rules
// the add-on re-validates are decided here first, so the form can show them
// before anything is written.
// The one-line add behind the + on the week bucket. A title and nothing
// else: no due date, so it lands in the bucket it was typed into, and the
// first list that can hold a todo, so there is no chooser to answer. Anything
// more than that is what the entry pane is for.
function buildQuickTodoRequest(title, calendarName) {
  var text = String(title || "").replace(/^\s+|\s+$/g, "")
  if (!text) return { ok: false, error: "nothing to add" }
  if (text.length > 300) return { ok: false, error: "title is too long" }
  return {
    ok: true,
    request: {
      kind: "task",
      title: text,
      dueMs: null,
      description: null,
      calendarName: String(calendarName || "") || null,
      priority: null,
      recurrence: null,
      link: null
    }
  }
}

// How many open todos of exactly this title the calendar already holds. The
// count, not a yes/no: adding a second "Wäsche" has to be visible even though
// the first one is still sitting there.
function countOpenTodos(tasks, title, calendarName) {
  var wantedTitle = String(title || "")
  var wantedList = String(calendarName || "")
  var n = 0
  for (var i = 0; tasks && i < tasks.length; i++) {
    var t = tasks[i]
    if (!t || t.done) continue
    if (String(t.title || "") !== wantedTitle) continue
    if (wantedList && String(t.calendarName || "") !== wantedList) continue
    n += 1
  }
  return n
}

// A todo typed into the + is shown before the server has it. The write goes
// out, the calendar is re-read a few seconds later, and until that read lands
// the chip has to come from somewhere: this is that somewhere. It carries the
// count of same-titled todos at the moment it was made, so it stands down as
// soon as the calendar comes back holding one more of them than it did.
var PENDING_TODO_TTL_MS = 90 * 1000

function pendingTodo(title, calendarName, nowMs, tasks) {
  var text = String(title || "").replace(/^\s+|\s+$/g, "")
  if (!text) return null
  var list = String(calendarName || "")
  var colors = calendarColors(tasks)
  return {
    id: "pending:" + Math.round(nowMs) + ":" + text,
    calendarId: "",
    calendarName: list,
    color: colors[list] || "",
    dueKey: "",
    due: "",
    time: "",
    allDay: true,
    title: text,
    priority: "",
    done: false,
    completedAt: "",
    location: "",
    meetingUrl: "",
    pending: true,
    createdMs: Math.round(nowMs),
    seen: countOpenTodos(tasks, text, list)
  }
}

// Drop the placeholders the sync has caught up with, and the ones that have
// been waiting long enough that the write plainly did not land — better a
// chip that quietly goes away than one that lies about being filed.
function prunePendingTodos(pending, tasks, nowMs, ttlMs) {
  var ttl = isFinite(ttlMs) ? ttlMs : PENDING_TODO_TTL_MS
  var now = Math.round(nowMs)
  var out = []
  for (var i = 0; pending && i < pending.length; i++) {
    var p = pending[i]
    if (!p || !p.title) continue
    if (now - p.createdMs > ttl) continue
    if (countOpenTodos(tasks, p.title, p.calendarName) > p.seen) continue
    out.push(p)
  }
  return out
}

// The list a bare todo goes to: the first one in the roster that can hold
// one. The roster keeps the server's own order, so this is the same list
// the CLI would put it in.
function firstTaskCalendarName(calendars) {
  var options = calendarOptions(calendars, "task")
  return options.length ? options[0].value : ""
}

function buildQuickAddRequest(draft, nowMs) {
  var now = isFinite(nowMs) ? nowMs : Date.now()
  var kind = draft && draft.kind === "task" ? "task" : "event"
  if (!draft) return { ok: false, error: "nothing entered" }

  var title = String(draft.title || "").replace(/^\s+|\s+$/g, "")
  if (!title) return { ok: false, error: "title is empty" }
  if (title.length > 300) return { ok: false, error: "title is too long" }

  var startDate = dateFromKey(draft.dateKey, null)
  if (!startDate) return { ok: false, error: "no valid date" }

  var recurrence = null
  if (draft.recurrence && draft.recurrence.interval >= 1 && draft.recurrence.interval <= 366)
    recurrence = { freq: draft.recurrence.freq, interval: Math.round(draft.recurrence.interval) }

  if (kind === "task") {
    // A task is due when a time was set on it, and loose otherwise — that is
    // what the week bucket holds. The hour that was typed is part of the
    // deadline: "by Friday" and "by 17:00 on Friday" are different promises.
    var dueMs = null
    var dueHasTime = false
    if (draft.startTime !== null && draft.startTime !== undefined && draft.startTime !== "") {
      dueMs = nlMsFor(draft.dateKey, draft.startTime)
      dueHasTime = true
      if (!nlValidMs(dueMs, now)) return { ok: false, error: "due date out of range" }
    }
    var prio = draft.priority === "high" ? 1 : draft.priority === "medium" ? 5 : draft.priority === "low" ? 9 : null
    return {
      ok: true,
      request: {
        kind: "task",
        title: title,
        dueMs: dueMs,
        dueHasTime: dueHasTime,
        description: String(draft.description || "").replace(/^\s+|\s+$/g, "").slice(0, 8000) || null,
        calendarName: draft.calendarName || null,
        priority: prio,
        recurrence: recurrence,
        link: safeLinkUrl(draft.link) || null
      }
    }
  }

  var startMs = nlMsFor(draft.dateKey, draft.allDay ? null : draft.startTime || "00:00")
  if (!nlValidMs(startMs, now)) return { ok: false, error: "date out of range" }

  var endMs = null
  if (!draft.allDay) {
    if (draft.endTime) {
      endMs = nlMsFor(draft.endDateKey || draft.dateKey, draft.endTime)
      if (draft.endNextDay) endMs += DAY_MS
    } else if (draft.durationMinutes) {
      endMs = startMs + Math.round(draft.durationMinutes) * MINUTE_MS
    } else {
      endMs = startMs + HOUR_MS
    }
    if (!nlValidMs(endMs, now)) return { ok: false, error: "date out of range" }
  } else if (draft.endDateKey) {
    // All-day multi-day: the document wants an exclusive end date.
    endMs = nlMsFor(draft.endDateKey, null)
    if (isNaN(endMs)) return { ok: false, error: "end date out of range" }
    endMs += DAY_MS
  }
  if (endMs !== null && endMs <= startMs) return { ok: false, error: "end is not after start" }

  var location = String(draft.location || "").replace(/^\s+|\s+$/g, "")
  var description = String(draft.description || "").replace(/^\s+|\s+$/g, "").slice(0, 8000)
  var request = {
    kind: "event",
    title: title,
    startMs: startMs,
    endMs: endMs,
    allDay: !!draft.allDay,
    location: location || null,
    description: description || null,
    calendarName: draft.calendarName || null,
    alertMinutes: draft.alertMinutes > 0 ? Math.round(draft.alertMinutes) : null,
    recurrence: recurrence,
    link: safeLinkUrl(draft.link) || null
  }
  // An id on the draft means the pane is editing an event already on the
  // server, not making a new one -- requestToArgs reads it to send `event
  // edit` instead of `event add`.
  if (draft.editingId) request.id = draft.editingId
  return { ok: true, request: request }
}

// One-line confirmation for the entry pane's status row: what Create would
// write, phrased the way the panel lists events everywhere else.
function formatEntrySummary(request) {
  if (!request) return ""
  var bits = []
  var anchorMs = request.kind === "task" ? request.dueMs : request.startMs
  if (request.kind === "task" && anchorMs === null) {
    bits.push("no due date")
  } else if (request.kind === "task") {
    var dueAt = new Date(anchorMs)
    bits.push("due " + nlShortDayLabel(dueAt) +
      (request.dueHasTime ? " · " + nlTimeLabel(dueAt.getHours() * 60 + dueAt.getMinutes()) : ""))
  } else if (isFinite(anchorMs)) {
    var d = new Date(anchorMs)
    var dayLabel = nlShortDayLabel(d)
    if (request.allDay) {
      if (isFinite(request.endMs) && request.endMs !== null)
        dayLabel += "–" + nlShortDayLabel(new Date(request.endMs - DAY_MS))
      bits.push(dayLabel + " · all day")
    } else {
      var from = nlTimeLabel(d.getHours() * 60 + d.getMinutes())
      if (isFinite(request.endMs) && request.endMs !== null) {
        var e = new Date(request.endMs)
        var endLabel = nlTimeLabel(e.getHours() * 60 + e.getMinutes())
        if (keyForDate(e) !== keyForDate(d)) endLabel = "+1d " + endLabel
        from += "–" + endLabel
      }
      bits.push(dayLabel + " · " + from)
    }
  }
  if (request.location) bits.push(request.location)
  if (request.calendarName) bits.push("/" + request.calendarName)
  if (request.recurrence)
    bits.push("every " + (request.recurrence.interval > 1 ? request.recurrence.interval + " " : "") +
      ({ daily: "day", weekly: "week", monthly: "month", yearly: "year" }[request.recurrence.freq] || request.recurrence.freq) +
      (request.recurrence.interval > 1 ? "s" : ""))
  if (request.alertMinutes) bits.push("alert " + request.alertMinutes + "m before")
  if (request.priority === 1) bits.push("!!!")
  else if (request.priority === 5) bits.push("!!")
  else if (request.priority === 9) bits.push("!")
  var head = truncateTitle(request.title, 40)
  return head + (bits.length ? "  ·  " + bits.join(" · ") : "")
}

// Weekday/month short forms, fixed English like the rest of this file so
// the summary stays testable under node.
function nlShortDayLabel(d) {
  var WD = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
  var MO = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
  return WD[d.getDay()] + " " + d.getDate() + " " + MO[d.getMonth()]
}

function calendarOptions(calendars, kind) {
  var wantTasks = kind === "task"
  var out = []
  for (var i = 0; calendars && i < calendars.length; i++) {
    var c = calendars[i]
    if (!c || !c.name) continue
    if (wantTasks ? c.tasks === false : c.events === false) continue
    out.push({ value: c.name, label: c.name })
  }
  return out
}

// ---- The mailbox daemon's answers, shaped for the panel -------------------

// The daemon speaks #RRGGBBAA; an 8-digit hex reads as #AARRGGBB in QML and
// would paint orange violet. The alpha is always opaque, so it goes.
function normalizeColor(color) {
  var text = String(color || "").trim()
  return /^#[0-9a-fA-F]{8}$/.test(text) ? text.slice(0, 7) : text
}

// The best join link in free text, meeting hosts first. A link on an unknown
// host is a document, not a room to join, so it never wins.
var MAILBOX_MEETING_HOST = /meet\.|zoom\.|teams\.|jitsi|whereby|webex|gotomeeting|skype\.|discord\.gg/i
var MAILBOX_MEETING_URL = /https?:\/\/[^\s<>"')]+/g

function meetingUrlIn() {
  for (var i = 0; i < arguments.length; i++) {
    var text = String(arguments[i] || "")
    if (!text) continue
    var matches = text.match(/https?:\/\/[^\s<>")']+/g) || []
    for (var j = 0; j < matches.length; j++) {
      var url = matches[j].replace(/[).,;]+$/, "")
      if (!isCalendarFileUrl(url)) {
        var host = url.split("//").pop().split("/")[0]
        if (MAILBOX_MEETING_HOST.test(host)) return url
      }
    }
  }
  return ""
}

// One agenda/todo row's time fields read back as local wall clock. The
// daemon answers ISO with an offset; Date.parse takes it from there.
function pad2(n) { return (n < 10 ? "0" : "") + n }

function stampLocal(ms, withTime) {
  var d = new Date(ms)
  var day = d.getFullYear() + "-" + pad2(d.getMonth() + 1) + "-" + pad2(d.getDate())
  if (!withTime) return day
  return day + " " + pad2(d.getHours()) + ":" + pad2(d.getMinutes())
}

// The roster the entry pane chooses a calendar from. Cards are an address
// book, not a calendar: the panel has nothing to draw them on. A collection the
// daemon marks internal -- the habits record, which is storage shaped like a
// calendar -- is left out for the other reason: nothing can be added to it, so
// offering it in a chooser would only be a way to fail.
function mailboxRoster(rows) {
  var out = []
  for (var i = 0; rows && i < rows.length; i++) {
    var row = rows[i]
    if (!row || !row.name) continue
    if (row.internal) continue
    var kind = String(row.kind || "")
    if (kind !== "events" && kind !== "tasks") continue
    out.push({
      id: String(row.name),
      name: String(row.name),
      color: normalizeColor(row.color),
      events: kind === "events",
      tasks: kind === "tasks"
    })
  }
  return out
}

// Agenda rows are already expanded over the window, so a repeating entry
// comes back once per day it touches; the panel files each row by its day.
function eventsFromAgenda(rows, colors) {
  var seen = {}
  var out = []
  for (var i = 0; rows && i < rows.length; i++) {
    var row = rows[i]
    if (!row || !row.start) continue
    var cal = String(row.calendar || "")
    var allDay = !!row.all_day
    var start = String(row.start || "")
    var event = {
      id: String(row.uid || row.id || row.summary || ""),
      // The mirror's numeric id, which is what `event edit`/`event delete`
      // take — the uid above is for de-duplicating recurring instances, not
      // for addressing the object on the server.
      objectId: row.id !== undefined && row.id !== null ? Number(row.id) : null,
      recurring: !!row.recurring,
      calendarId: cal,
      calendarName: cal,
      color: colors[cal] || "",
      dateKey: String(row.date || "").slice(0, 10),
      start: allDay ? String(row.start || "").slice(0, 10) : String(row.start || ""),
      end: allDay ? String(row.end || "").slice(0, 10) : String(row.end || ""),
      allDay: allDay,
      title: String(row.summary || "").trim() || "(no title)",
      location: String(row.location || ""),
      time: allDay ? "" : String(row.start || "").slice(11, 16),
      meetingUrl: safeLinkUrl(row.url) || meetingUrlIn(row.summary, row.location, row.notes)
    }
    var key = event.id + "|" + event.dateKey
    if (seen[event.id + "|" + event.dateKey]) continue
    seen[event.id + "|" + event.dateKey] = true
    out.push(event)
  }
  out.sort(function (a, b) {
    if (a.dateKey !== b.dateKey) return a.dateKey < b.dateKey ? -1 : 1
    if ((a.start || "") !== (b.start || "")) return (a.start || "") < (b.start || "") ? -1 : 1
    return a.title < b.title ? -1 : a.title > b.title ? 1 : 0
  })
  return out
}

// Open todos only: the panel takes a ticked one off itself the moment it is
// written, so a finished task in the answer is a row nothing would draw.
function tasksFromMailbox(rows, colors) {
  var out = []
  for (var i = 0; rows && i < rows.length; i++) {
    var row = rows[i]
    if (!row || row.done) continue
    var list = String(row.list || "")
    var stamp = String(row.due || "")
    var due = stamp.slice(0, 10)
    // A deadline with an hour on it comes back as "2026-09-01 17:00"; a due
    // date is the day on its own, and stays a day.
    var dueTime = stamp.length > 10 ? stamp.slice(11, 16) : ""
    out.push({
      id: String(row.id || row.uid || ""),
      uid: String(row.uid || ""),
      calendarId: "",
      calendarName: list,
      color: colors[list] || "",
      dueKey: due,
      due: due,
      time: dueTime,
      allDay: dueTime === "",
      title: String(row.summary || "(no title)"),
      priority: String(row.priority || ""),
      done: false,
      completedAt: "",
      location: "",
      meetingUrl: ""
    })
  }
  out.sort(function (a, b) {
    if (a.dueKey === "" !== (b.dueKey === "")) return a.dueKey === "" ? 1 : -1
    if (a.dueKey !== b.dueKey) return a.dueKey < b.dueKey ? -1 : 1
    return a.title < b.title ? -1 : a.title > b.title ? 1 : 0
  })
  return out
}

// The three daemon answers as one document, the shape parseEventDocument
// reads. The panel owns the asking; Model owns the shaping.
function mailboxDocument(roster, agenda, todos, nowMs) {
  var eventColors = {}
  var taskColors = {}
  for (var i = 0; roster && i < roster.length; i++) {
    var c = roster[i]
    if (!c || !c.color) continue
    if (c.events !== false) eventColors[c.name] = c.color
    if (c.tasks !== false) taskColors[c.name] = c.color
  }
  var doc = {
    version: 1,
    syncedAt: new Date(isFinite(nowMs) ? nowMs : Date.now()).toISOString(),
    source: "mailbox-daemon",
    events: eventsFromAgenda(agenda, eventColors),
    tasks: tasksFromMailbox(todos, taskColors)
  }
  return doc
}

// A wire request from the entry pane becomes one socket command. The keys are
// the daemon's arg names, which the CLI passes through unchanged.
function requestToArgs(request) {
  if (!request || !request.kind) return null
  if (request.kind === "complete") {
    if (!request.id) return null
    return { cmd: ["todo", request.done ? "done" : "undone"], args: { positional: String(request.id) } }
  }
  if (request.kind === "task") {
    var taskArgs = { positional: String(request.title || "") }
    if (request.calendarName) taskArgs.list = String(request.calendarName)
    if (request.dueMs) taskArgs.due = stampLocal(request.dueMs, !!request.dueHasTime)
    if (request.priority) taskArgs.priority = priorityWord(request.priority)
    return { cmd: ["todo", "add"], args: taskArgs }
  }
  if (request.action === "delete") {
    if (!request.id) return null
    return { cmd: ["event", "delete"], args: { positional: String(request.id) } }
  }
  if (!request.title || !request.startMs) return null
  var editing = !!request.id
  var args = editing
    ? { positional: String(request.id), title: String(request.title) }
    : { positional: String(request.title) }
  args.start = stampLocal(request.startMs, !request.allDay)
  if (request.endMs) args.end = stampLocal(request.endMs, !request.allDay)
  // An edit cannot move an event to another calendar -- the daemon has no
  // verb for that -- so naming one only matters when this is a new event.
  if (request.calendarName && !editing) args.calendar = String(request.calendarName)
  if (request.location) args.location = String(request.location)
  // The link is a link, not a line of the description: written as --url it is
  // one field every client shows as one, instead of a URL glued to the notes.
  if (request.link) args.url = String(request.link)
  if (request.description) args.notes = String(request.description)
  if (request.alertMinutes > 0) args.alarm = String(Math.round(request.alertMinutes))
  var rule = repeatRule(request.recurrence)
  if (rule) args.repeat = rule
  if (request.allDay) args.all_day = true
  return { cmd: ["event", editing ? "edit" : "add"], args: args }
}

// The three words the daemon takes, from the number the pane picked.
function priorityWord(priority) {
  return priority === 1 ? "high" : priority === 5 ? "medium" : priority === 9 ? "low" : ""
}

// The recurrence the pane picked, as the rule the daemon takes. A frequency
// this does not know is no rule at all: a repeat nobody can read back is worse
// than an entry that happens once.
function repeatRule(recurrence) {
  if (!recurrence || !recurrence.freq) return ""
  var freq = String(recurrence.freq).toUpperCase()
  if (["DAILY", "WEEKLY", "MONTHLY", "YEARLY"].indexOf(freq) === -1) return ""
  var interval = Math.round(recurrence.interval || 1)
  if (!(interval >= 1)) interval = 1
  return "FREQ=" + freq + (interval > 1 ? ";INTERVAL=" + interval : "")
}

// ---- Entry-field rendering -------------------------------------------------

// StyledText escaping. Runs of spaces collapse in styled text the way they do
// in HTML, so every space but the last in a run becomes a hard one — the
// painted overlay then sits glyph-for-glyph on the plain text underneath it,
// with the last space left soft so the line can still wrap there.
function escapePhrase(text) {
  return String(text)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/ {2,}/g, function(run) { return new Array(run.length).join("&#160;") + " " })
}

// The typed phrase as styled text: each parsed part wrapped in the colour its
// role was given. `colors` maps role -> "#rrggbb"; a role with no colour is
// painted in whatever the surrounding Text already uses.
function phraseHtml(text, segments, colors) {
  var raw = String(text === undefined || text === null ? "" : text)
  if (!raw) return ""
  var list = []
  for (var i = 0; segments && i < segments.length; i++) {
    var s = segments[i]
    if (!s || !(s.end > s.start)) continue
    list.push(s)
  }
  list.sort(function(a, b) { return a.start - b.start })

  var out = ""
  var cursor = 0
  for (var k = 0; k < list.length; k++) {
    var seg = list[k]
    var from = Math.max(cursor, Math.min(seg.start, raw.length))
    var to = Math.max(from, Math.min(seg.end, raw.length))
    if (from > cursor) out += escapePhrase(raw.slice(cursor, from))
    var body = escapePhrase(raw.slice(from, to))
    // A link is underlined as well as coloured: on a theme whose foreground
    // is already blue-grey, colour alone does not say "this is a link".
    // Underlining leaves glyph advances alone, so the painted overlay still
    // lands exactly on the editor underneath.
    if (seg.role === "link") body = "<u>" + body + "</u>"
    var color = colors ? colors[seg.role] : ""
    out += color ? '<font color="' + color + '">' + body + "</font>" : body
    cursor = to
  }
  if (cursor < raw.length) out += escapePhrase(raw.slice(cursor))
  return out
}

// How a day reads against today. The words are put together in QML, where
// Qt's own date formatting lives — JavaScript's toLocaleDateString options
// are ignored by the QML engine, so a month name has to come from Qt.
function relativeDayKind(dateKey, todayKey) {
  var d = dateFromKey(dateKey, null)
  if (!d) return ""
  var t = dateFromKey(todayKey, null)
  if (!t) return "date"
  var days = Math.round((new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime()
    - new Date(t.getFullYear(), t.getMonth(), t.getDate()).getTime()) / MS_PER_DAY)
  if (days === 0) return "today"
  if (days === 1) return "tomorrow"
  if (days === -1) return "yesterday"
  if (days > 1 && days < 7) return "weekday"
  return "date"
}

// A day typed into the date chip. Deliberately the same vocabulary as the
// phrase parser — "tomorrow", "next friday", "15.3.", "2026-03-15" — so the
// two entry paths never disagree about what a word means.
function parseDateInput(text, baseKey, nowMs) {
  var t = String(text === undefined || text === null ? "" : text).trim().toLowerCase().replace(/[,;]$/, "")
  if (!t) return null
  var now = new Date(isFinite(nowMs) ? nowMs : Date.now())
  var base = dateFromKey(baseKey, null) || new Date(now.getFullYear(), now.getMonth(), now.getDate())
  base = new Date(base.getFullYear(), base.getMonth(), base.getDate())

  var words = t.split(/\s+/)
  var next = nlIs(words[0], "next")
  if (next && words.length > 1) words = words.slice(1)
  var head = words[0]

  if (nlIs(head, "today")) return keyForDate(base)
  if (nlIs(head, "tomorrow")) {
    var tm = new Date(base.getTime())
    tm.setDate(tm.getDate() + 1)
    return keyForDate(tm)
  }
  var weekday = nlWeekdayIndex(head)
  if (weekday !== -1) {
    var wd = nlNextWeekday(base, weekday)
    if (next) wd.setDate(wd.getDate() + 7)
    return keyForDate(wd)
  }
  var explicit = nlExplicitDate(head, base.getFullYear())
  if (explicit && !isNaN(explicit.date.getTime())) {
    var d = explicit.date
    if (explicit.yearless && d.getTime() < base.getTime()) d.setFullYear(d.getFullYear() + 1)
    if (next) d.setDate(d.getDate() + 7)
    return keyForDate(d)
  }
  return null
}

// A time typed into a time chip. The chip itself is the anchor, so a bare
// hour is enough here where the phrase parser would leave it in the title.
function parseTimeInput(text) {
  var t = String(text === undefined || text === null ? "" : text)
    .trim().toLowerCase().replace(",", ":").replace(/\s*uhr$/, "").replace(/\s+/g, "")
  if (!t) return null
  var m = /^(\d{1,2})(?:[:.](\d{2}))?(am|pm)?$/.exec(t)
  if (!m) return null
  var hours = parseInt(m[1], 10)
  var minutes = m[2] ? parseInt(m[2], 10) : 0
  if (m[3] === "pm" && hours < 12) hours += 12
  if (m[3] === "am" && hours === 12) hours = 0
  if (hours > 23 || minutes > 59) return null
  return pad2(hours) + ":" + pad2(minutes)
}

// Calendar name -> the colour the server paints the calendar in, learned from the
// events already synced. The chooser and the "/name" flag then read in the
// same colour as the day dots in the grid.
function calendarColors(events) {
  var out = {}
  for (var i = 0; events && i < events.length; i++) {
    var event = events[i]
    if (!event || !event.calendarName) continue
    var color = eventColor(event)
    if (color && !out[event.calendarName]) out[event.calendarName] = color
  }
  return out
}

// The alert dropdown's rows. A phrase can name any lead time ("-a40m"), so
// whatever is set joins the standard list rather than being rounded to it.
function alertLabelFor(minutes) {
  var value = Math.round(minutes)
  if (!value) return "No alert"
  if (value % 1440 === 0) {
    var days = value / 1440
    return days + (days === 1 ? " day before" : " days before")
  }
  if (value % 60 === 0) {
    var hours = value / 60
    return hours + (hours === 1 ? " hour before" : " hours before")
  }
  return value + (value === 1 ? " minute before" : " minutes before")
}

function alertOptions(minutes) {
  var presets = [0, 5, 15, 30, 60, 1440]
  var current = Math.round(minutes) || 0
  if (current > 0 && presets.indexOf(current) === -1) {
    presets.push(current)
    presets.sort(function(a, b) { return a - b })
  }
  var out = []
  for (var i = 0; i < presets.length; i++)
    out.push({ value: String(presets[i]), label: alertLabelFor(presets[i]) })
  return out
}

// Same idea for repeats: "-r2w" is a real answer, so "every 2 weeks" has to
// be a row the dropdown can show.
function repeatLabelFor(value) {
  var parts = String(value || "").split(":")
  var freq = parts[0]
  if (!freq) return "Does not repeat"
  var interval = parseInt(parts[1], 10) || 1
  var plural = { daily: "days", weekly: "weeks", monthly: "months", yearly: "years" }
  var single = { daily: "Daily", weekly: "Weekly", monthly: "Monthly", yearly: "Yearly" }
  if (interval === 1) return single[freq] || freq
  return "Every " + interval + " " + (plural[freq] || freq)
}

function repeatOptions(value) {
  var presets = ["", "daily:1", "weekly:1", "monthly:1", "yearly:1"]
  var current = String(value || "")
  if (current && presets.indexOf(current) === -1) presets.push(current)
  var out = []
  for (var i = 0; i < presets.length; i++)
    out.push({ value: presets[i], label: repeatLabelFor(presets[i]) })
  return out
}

// ---- Address suggestions ---------------------------------------------------
//
// Photon (photon.komoot.io) answers a half-typed name with OSM places; the
// script hands the raw reply over and everything the panel shows is built
// here, so the shapes stay tested. What a pick writes into the field is a
// postal address and nothing else — "Garbe Biergarten, Garbenstraße,
// 70599 Stuttgart" — never coordinates.

// A query is worth sending once it is a word rather than a keystroke, and
// only the panel's own field ever asks — the phrase never goes to the net.
function shouldSuggestAddress(text) {
  return String(text || "").replace(/^\s+|\s+$/g, "").length >= 3
}

// House number after the street, the way German addresses are written; a
// place name leads when OSM has one, since that is what was typed.
function addressLines(props) {
  var p = props || {}
  var name = String(p.name || "").replace(/^\s+|\s+$/g, "")
  var street = String(p.street || "").replace(/^\s+|\s+$/g, "")
  var number = String(p.housenumber || "").replace(/^\s+|\s+$/g, "")
  var postcode = String(p.postcode || "").replace(/^\s+|\s+$/g, "")
  var city = String(p.city || p.locality || p.district || p.county || "").replace(/^\s+|\s+$/g, "")
  var state = String(p.state || "").replace(/^\s+|\s+$/g, "")
  var country = String(p.country || "").replace(/^\s+|\s+$/g, "")

  var line = street ? (number ? street + " " + number : street) : ""
  var town = postcode && city ? postcode + " " + city : (city || postcode)
  // A place named the same as its street would read twice ("Garbenstraße,
  // Garbenstraße 5") — the street line already says it.
  var head = name && name !== line ? name : ""
  var parts = []
  if (head) parts.push(head)
  if (line) parts.push(line)
  if (town) parts.push(town)
  // Only say the country when it is the only thing placing the address.
  if (!town && (state || country)) parts.push(state || country)
  return parts
}

// One row: what was typed on top, the rest of the address under it, and the
// whole thing as the single line the field receives.
function addressSuggestion(feature) {
  var props = feature && feature.properties ? feature.properties : null
  if (!props) return null
  var parts = addressLines(props)
  if (!parts.length) return null
  var country = String(props.country || "").replace(/^\s+|\s+$/g, "")
  var detail = parts.slice(1)
  if (country && parts.length > 1 && detail.join(", ").indexOf(country) === -1) detail.push(country)
  return {
    value: parts.join(", "),
    primary: parts[0],
    secondary: detail.join(", "),
    countryCode: String(props.countrycode || "").toUpperCase()
  }
}

// Home country first. Photon ranks by its own idea of relevance and has no
// country filter, so "Garbe" can come back led by Ohio. The script asks for
// more rows than the list shows; here the ones at home float up, each group
// keeping the order Photon gave it, and only then is the list cut to size.
function preferCountryFirst(items, country) {
  var wanted = String(country || "").toUpperCase()
  if (!wanted) return items
  var home = []
  var away = []
  for (var i = 0; i < items.length; i++)
    (items[i].countryCode === wanted ? home : away).push(items[i])
  return home.concat(away)
}

// The script wraps the reply as { query, features }. The query travels with
// it so a slow answer to an abandoned word can be dropped rather than shown.
function parseAddressSuggestions(raw, limit, country) {
  var doc = null
  try { doc = typeof raw === "string" ? JSON.parse(raw) : raw } catch (e) { return { query: "", items: [] } }
  if (!doc || typeof doc !== "object") return { query: "", items: [] }
  var features = doc.features && doc.features.length ? doc.features : []
  var max = limit === undefined ? 6 : limit
  var home = country === undefined ? String(doc.country || "DE") : country
  var seen = {}
  var items = []
  for (var i = 0; i < features.length; i++) {
    var row = addressSuggestion(features[i])
    if (!row) continue
    var key = row.value.toLowerCase()
    if (seen[key]) continue
    seen[key] = 1
    items.push(row)
  }
  return { query: String(doc.query || ""), items: preferCountryFirst(items, home).slice(0, max) }
}

// ---- Merging a parse into what is already on screen ------------------------

function phraseHasRole(segments, role) {
  for (var i = 0; segments && i < segments.length; i++)
    if (segments[i] && segments[i].role === role) return true
  return false
}

function defaultEndTime(startTime, durationMinutes) {
  var parts = /^(\d{1,2}):(\d{2})$/.exec(String(startTime || ""))
  if (!parts) return ""
  var span = durationMinutes > 0 ? Math.round(durationMinutes) : 60
  var minutes = (parseInt(parts[1], 10) * 60 + parseInt(parts[2], 10) + span) % 1440
  return nlTimeLabel(minutes)
}

// The phrase owns what it says; the pane owns the rest. `current` is what is
// on screen, `applied` is what the previous parse put there — the difference
// between the two is what somebody typed into a chip or a row by hand, and
// that has to survive another word being typed in the phrase.
//
// Everything derived is recorded as applied too (the hour an open-ended start
// gets, say), or the next parse would mistake it for a hand edit and keep it:
// "at 1" gives 01:00-02:00, and typing the 8 has to make that 18:00-19:00,
// not 18:00-02:00.
function mergeEntryDraft(parsed, current, applied, kind) {
  var was = applied || {}
  var now = {}
  var out = {}
  var isTask = kind === "task"

  function take(name, value, blank) {
    now[name] = value
    if (value !== blank) return value
    var before = was[name]
    if (before !== undefined && before !== blank && current[name] === before) return blank
    return current[name] === undefined ? blank : current[name]
  }

  // The parser hands back a day either way — the clicked one when nothing was
  // said — so only a labelled date segment counts as the phrase speaking.
  var saidDate = phraseHasRole(parsed.segments, "date") ? (parsed.dateKey || "") : ""
  out.date = take("date", saidDate, "") || parsed.dateKey || current.date || ""
  out.start = take("start", parsed.startTime || "", "")
  out.end = take("end", parsed.endTime || "", "")
  out.endDate = take("endDate", parsed.endDateKey || "", "")
  out.endNextDay = !!parsed.endNextDay
  out.location = take("location", parsed.location || "", "")
  out.notes = take("notes", parsed.description || "", "")
  out.link = take("link", parsed.link || "", "")
  out.alert = take("alert", parsed.alertMinutes || 0, 0)
  out.repeat = take("repeat",
    parsed.recurrence && parsed.recurrence.freq
      ? parsed.recurrence.freq + ":" + (parsed.recurrence.interval || 1) : "", "")
  out.priority = take("priority", parsed.priority || "", "")

  // A task has one day, not a span: "einkaufen bis freitag" names when it is
  // due, so the end day becomes the day.
  if (isTask && parsed.endDateKey) {
    out.date = parsed.endDateKey
    out.endDate = ""
    out.endNextDay = false
  }

  // An end with no start is an event that finishes without starting.
  if (!out.start) {
    out.end = ""
    now.end = ""
  } else if (!isTask && !out.end) {
    // An open-ended start still means an hour — or the duration the phrase
    // gave ("für 2 h", "-120").
    out.end = defaultEndTime(out.start, parsed.durationMinutes)
    now.end = out.end
  }
  if (out.start && out.end && out.end <= out.start && !out.endDate) out.endNextDay = true

  return { values: out, applied: now }
}

if (typeof module !== "undefined") {
  module.exports = {
    dateKey: dateKey,
    keyForDate: keyForDate,
    normalizedWeekStart: normalizedWeekStart,
    weekStartSettingName: weekStartSettingName,
    toggledWeekStart: toggledWeekStart,
    weekdayOrder: weekdayOrder,
    isoWeek: isoWeek,
    dayOfYear: dayOfYear,
    daysInYear: daysInYear,
    yearProgress: yearProgress,
    yearProgressPercent: yearProgressPercent,
    parseAge: parseAge,
    parseBirthYear: parseBirthYear,
    ageFromBirthYear: ageFromBirthYear,
    parseLifeExpectancy: parseLifeExpectancy,
    lifeProgress: lifeProgress,
    lifeProgressPercent: lifeProgressPercent,
    monthGrid: monthGrid,
    stepMonth: stepMonth,
    clockFormats: clockFormats,
    clockFormatRing: clockFormatRing,
    nextClockFormat: nextClockFormat,
    isoWeekLiteral: isoWeekLiteral,
    indexEventsByDate: indexEventsByDate,
    eventsForDateKey: eventsForDateKey,
    taskPlacement: taskPlacement,
    taskOverdueDays: taskOverdueDays,
    tasksFromDocument: tasksFromDocument,
    indexTasksByDate: indexTasksByDate,
    tasksForDateKey: tasksForDateKey,
    weekTasks: weekTasks,
    weekStartKeyOf: weekStartKeyOf,
    weekTasksFor: weekTasksFor,
    taskColors: taskColors,
    hasOpenTask: hasOpenTask,
    buildCompleteRequest: buildCompleteRequest,
    dateFromKey: dateFromKey,
    formatDateKey: formatDateKey,
    parseEventDocument: parseEventDocument,
    nextEvent: nextEvent,
    eventStartMs: eventStartMs,
    eventEndMs: eventEndMs,
    isInProgress: isInProgress,
    formatCountdown: formatCountdown,
    formatStartsIn: formatStartsIn,
    isImminent: isImminent,
    joinButtonLabel: joinButtonLabel,
    truncateTitle: truncateTitle,
    announceLabel: announceLabel,
    millisUntil: millisUntil,
    shouldAnnounce: shouldAnnounce,
    shouldNudge: shouldNudge,
    occurrenceKey: occurrenceKey,
    isDismissed: isDismissed,
    joinTooltip: joinTooltip,
    eventDisplayTime: eventDisplayTime,
    safeUrl: safeUrl,
    safeLinkUrl: safeLinkUrl,
    linkProviderLabel: linkProviderLabel,
    safeColor: safeColor,
    eventColor: eventColor,
    dayColors: dayColors,
    isCalendarFileUrl: isCalendarFileUrl,
    meetingUrlFor: meetingUrlFor,
    parseEventPhrase: parseEventPhrase,
    fallbackDraft: fallbackDraft,
    draftFromAgendaEvent: draftFromAgendaEvent,
    draftFromEventDetail: draftFromEventDetail,
    buildQuickAddRequest: buildQuickAddRequest,
    buildQuickTodoRequest: buildQuickTodoRequest,
    firstTaskCalendarName: firstTaskCalendarName,
    countOpenTodos: countOpenTodos,
    pendingTodo: pendingTodo,
    prunePendingTodos: prunePendingTodos,
    formatEntrySummary: formatEntrySummary,
    calendarOptions: calendarOptions,
    mailboxRoster: mailboxRoster,
    mailboxDocument: mailboxDocument,
    requestToArgs: requestToArgs,
    repeatRule: repeatRule,
    priorityWord: priorityWord,
    meetingUrlIn: meetingUrlIn,
    calendarColors: calendarColors,
    mergeSegments: mergeSegments,
    escapePhrase: escapePhrase,
    phraseHtml: phraseHtml,
    relativeDayKind: relativeDayKind,
    parseDateInput: parseDateInput,
    parseTimeInput: parseTimeInput,
    alertLabelFor: alertLabelFor,
    alertOptions: alertOptions,
    repeatLabelFor: repeatLabelFor,
    repeatOptions: repeatOptions,
    phraseHasRole: phraseHasRole,
    defaultEndTime: defaultEndTime,
    mergeEntryDraft: mergeEntryDraft,
    shouldSuggestAddress: shouldSuggestAddress,
    addressLines: addressLines,
    addressSuggestion: addressSuggestion,
    parseAddressSuggestions: parseAddressSuggestions,
    preferCountryFirst: preferCountryFirst
  }
}
