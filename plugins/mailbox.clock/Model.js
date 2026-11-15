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
  "HH\n,\nmm",
  "h\n,\nmm\nAP",
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

  var text = String(value).trim().toLowerCase()
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

// A whole number in range, or the fallback. Blank, malformed, negative,
// fractional and out-of-range all mean the same thing: the setting is not set.
function parseWholeNumber(value, min, max, fallback) {
  var text = String(value === undefined || value === null ? "" : value).trim()
  if (!/^\d+$/.test(text)) return fallback
  var n = parseInt(text, 10)
  return n >= min && n <= max ? n : fallback
}

// A birth year rather than an age, so the bar keeps counting on its own
// instead of going stale the moment it is entered. 0 means "not set".
function parseBirthYear(value, currentYear) {
  var now = Math.round(Number(currentYear))
  if (!isFinite(now)) return 0
  return parseWholeNumber(value, now - 120, now, 0)
}

// Whole years, the way people say their age: born in 1979 makes you 47 for
// all of 2026, whichever side of your birthday today falls.
function ageFromBirthYear(birthYear, currentYear) {
  var born = parseBirthYear(birthYear, currentYear)
  if (born <= 0) return 0
  return Math.round(Number(currentYear)) - born
}

// 0 means "not set", the life bar simply stays hidden.
function parseAge(value) {
  return parseWholeNumber(value, 1, 120, 0)
}

// Unset or nonsense falls back to the default rather than to zero, so the
// bar always has something to measure against.
function parseLifeExpectancy(value) {
  return parseWholeNumber(value, 1, 150, DEFAULT_LIFE_EXPECTANCY)
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

// ---- Tasks. A VTODO has at most a due date, and most have none at all, so
//      a month grid cannot place them the way it places events. Every task is
//      sorted into one of three places instead:
//
//        "day" , due today or later: it sits on its due date, like an event
//        "week", no due date, or due before today: it belongs to the loose
//                 pile of work for the current week
//        "done", finished: it is drawn nowhere. Ticking a todo takes it off
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
// current week and on no other, clicking into next week shows what is
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

var MINUTE_MS = 60 * 1000
var HOUR_MS = 60 * MINUTE_MS
var DAY_MS = MS_PER_DAY

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

// "starts in 5 minutes", then "starts now" once the start time is reached.
// Minutes round up so the last partial minute still reads "starts in 1
// minute" rather than jumping to "now" while there is time left.
function formatStartsIn(deltaMs) {
  if (deltaMs === null || isNaN(deltaMs)) return ""
  if (deltaMs <= 0) return "starts now"
  var minutes = Math.ceil(deltaMs / MINUTE_MS)
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
  var provider = linkProviderLabel(url)
  return provider === "Link" ? "Join" : "Join " + provider
}

var MAX_ANNOUNCE_TITLE = 28

function truncateTitle(title, limit) {
  var text = String(title === undefined || title === null ? "" : title)
  var max = limit || MAX_ANNOUNCE_TITLE
  if (text.length <= max) return text
  return text.substring(0, max - 1).replace(/\s+$/, "") + "…"
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
  if (!isFinite(lead) || lead < 0) lead = 0
  if (lead > 0 && delta <= lead * MINUTE_MS) return true

  // The event's own reminders (--alarm / VALARM) each fire once and briefly:
  // shown for blipMinutes and then gone, with no running countdown. The bar
  // is the only reminder the desktop has, so it mirrors the phone's single
  // buzz rather than sitting on screen for the whole hour before the start.
  return inAlarmBlip(event, nowMs, leadMinutes, startedLeadMinutes)
}

var DEFAULT_BLIP_MINUTES = 1

// True while an alarm the event carries is inside its brief on-screen window
// and the normal lead countdown has not taken over yet. The widget reads this
// to drop the countdown phrase and give the label one shake as it appears.
function inAlarmBlip(event, nowMs, leadMinutes, blipMinutes) {
  if (!event) return false
  var delta = millisUntil(event, nowMs)
  if (delta === null || delta <= 0) return false

  var lead = Number(leadMinutes)
  if (isFinite(lead) && lead > 0 && delta <= lead * MINUTE_MS) return false

  var blip = Number(blipMinutes)
  if (!isFinite(blip) || blip <= 0) blip = DEFAULT_BLIP_MINUTES
  var alarms = event.alarms
  for (var i = 0; alarms && i < alarms.length; i++) {
    var at = Number(alarms[i]) * MINUTE_MS
    if (isFinite(at) && at > 0 && delta <= at && delta > at - blip * MINUTE_MS) return true
  }
  return false
}

// A join button left unclicked: the meeting is running, it still has a
// Join link, and between graceMs and windowMs has passed since it started
// without one. Past the window the meeting counts as deliberately skipped,
// not forgotten -- and a shell restarted hours into it stays quiet instead
// of punishing the restart. Fires on every tick while true; the widget
// supplies the once-per-occurrence memory so the sound plays a single time.
function shouldNudge(event, nowMs, graceMs, windowMs) {
  if (!event) return false
  if (!isMeetingUrl(meetingUrlFor(event))) return false
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

// A link is supplied by whoever sent the invitation, so treating it as
// trusted input would be a mistake. http as well as https, the entry pane
// lets a link be typed by hand, and refusing to open what it just stored
// would be the odder of the two. Every other scheme stays out: this string
// is handed to a browser.
function safeUrl(url) {
  var text = String(url || "").trim()
  if (!/^https?:\/\//i.test(text)) return ""
  if (/[\s"'<>]/.test(text)) return ""
  if (text.length > 2000) return ""
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

// The same guard under the name the storing paths call it by.
function safeLinkUrl(url) {
  return safeUrl(url)
}

// What to call a meeting link in one word, for the pill that holds it.
function linkProviderLabel(url) {
  var text = String(url || "").toLowerCase()
  if (!text) return "Link"
  if (text.indexOf("zoom.") !== -1) return "Zoom"
  if (text.indexOf("meet.google.") !== -1) return "Meet"
  if (text.indexOf("teams.") !== -1) return "Teams"
  if (text.indexOf("jitsi") !== -1 || text.indexOf("jit.si") !== -1) return "Jitsi"
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

// A room to join rather than a page to read. Same host rule meetingUrlIn
// applies: a link on an unknown host, the pool's homepage sitting in the
// URL field, is a document, and gets a Link button, never a Join.
function isMeetingUrl(url) {
  var text = safeUrl(url)
  if (!text || isCalendarFileUrl(text)) return false
  var host = text.split("//").pop().split("/")[0]
  return MAILBOX_MEETING_HOST.test(host)
}

// ---- Natural-language quick-add -------------------------------------------
//
// One line of typing becomes a draft. The reading is a scanner over a folded
// copy of the text (lower case, umlauts flattened, same length), so a pattern
// matches the same phrase whatever the spelling, and every offset it finds is
// still an offset into what the user typed. What a pattern accepts is blanked
// out of both copies: the blanks are the runs the entry field paints in their
// role colour, and what no read took is the title, in the user's own spelling.
// `nlTake` walks every match of a pattern until the reader accepts one, and
// `nlSnapshot`/`nlRestore` let a read put back what it took when the rest of
// its phrase does not hold. `nlTakeAdjacent` is the same for a phrase that has
// to sit right after the word that introduced it ("till morgen", not "till"
// plus a date three words later).
//
// The reading order is the grammar: flags, calendar, repeat, relative, time
// ranges, till, days, day part, times, durations, place. An earlier read owns
// the words it needs, which is why "15.3." is a date before "15.3" could be a
// time, and why "every monday" hands the weekday to the day read and only
// takes the "every".
//
// The scanner shape (a folded copy of the same length, blanks, take with
// snapshot/restore) is the one in tmn73/omarchy-calendar's QuickAddParser
// (MIT); the vocabulary, the draft and the colours are this widget's own.
var NL_FOLDED_LETTERS = {
  "ä": "a", "á": "a", "à": "a", "â": "a", "ã": "a", "å": "a",
  "é": "e", "è": "e", "ê": "e", "ë": "e",
  "í": "i", "ì": "i", "î": "i", "ï": "i",
  "ó": "o", "ò": "o", "ô": "o", "õ": "o", "ö": "o", "ø": "o",
  "ú": "u", "ù": "u", "û": "u", "ü": "u",
  "ç": "c", "ñ": "n"
}

// Full names always count as a day. A two or three letter one ("so", "mi",
// "fr") is also an ordinary word, so it only counts with a day word in front
// of it ("next fr", "bis fr") or a time right after it ("fr 9:00").
var NL_WEEKDAYS = [
  { index: 0, names: ["sunday", "sonntag"], shorts: ["sun", "so"] },
  { index: 1, names: ["monday", "montag"], shorts: ["mon", "mo"] },
  { index: 2, names: ["tuesday", "dienstag"], shorts: ["tue", "tues", "di"] },
  { index: 3, names: ["wednesday", "mittwoch"], shorts: ["wed", "mi"] },
  { index: 4, names: ["thursday", "donnerstag"], shorts: ["thu", "thur", "thurs", "do"] },
  { index: 5, names: ["friday", "freitag"], shorts: ["fri", "fr"] },
  { index: 6, names: ["saturday", "samstag", "sonnabend"], shorts: ["sat", "sa"] }
]

// Folded spellings: "März" arrives as "marz", "Mär." as "mar".
var NL_MONTHS = [
  ["january", "januar", "jan"],
  ["february", "februar", "feb"],
  ["march", "marz", "mar"],
  ["april", "apr"],
  ["may", "mai"],
  ["june", "juni", "jun"],
  ["july", "juli", "jul"],
  ["august", "aug"],
  ["september", "sept", "sep"],
  ["october", "oktober", "oct", "okt"],
  ["november", "nov"],
  ["december", "dezember", "dec", "dez"]
]

var NL_WORDS = {
  today: ["today", "heute"],
  tomorrow: ["tomorrow", "morgen"],
  next: ["next", "nachste", "nachsten", "nachster", "kommende", "kommenden", "kommender",
    "nächste", "nächsten", "nächster"],
  nextWeek: ["next week", "nachste woche", "nachsten woche", "kommende woche"],
  till: ["till", "until", "bis", "through", "thru", "to"],
  place: ["at", "bei", "beim"]
}

// Words that introduce the day they sit in front of. Only these let a short
// weekday name through on their own.
var NL_DAY_LEAD_WORDS = ["on", "am", "by", "bis", "till", "until", "to", "through",
  "von", "from", "the", "den", "dem"]

// Repeats by our own presets, folded. "every monday" is not here: it is an
// "every" plus a day, and the day read owns the weekday.
var NL_REPEAT_PHRASES = {
  daily: ["every day", "everyday", "each day", "daily", "jeden tag", "taglich"],
  weekly: ["every week", "each week", "weekly", "jede woche", "jeden woche", "wochentlich"],
  monthly: ["every month", "each month", "monthly", "jeden monat", "jedes monat", "monatlich"],
  yearly: ["every year", "each year", "yearly", "annually", "jedes jahr", "jeden jahr", "jahrlich"]
}
// As the first word these name the event ("Weekly review"), not a repeat.
var NL_REPEAT_TITLE_WORDS = ["daily", "weekly", "monthly", "yearly"]
var NL_EVERY_WORDS = ["every", "each", "jeden", "jede", "jedes", "jeder", "alle"]

// Where a day part starts, in minutes after midnight. A phrase in
// NL_DAY_PART_PHRASES always counts; a bare word only right after the day it
// belongs to ("friday evening"), so "Morning run" keeps its title.
var NL_DAY_PART_START = { morning: 9 * 60, afternoon: 14 * 60, evening: 19 * 60 }
var NL_DAY_PART_PHRASES = {
  morning: ["this morning", "in the morning", "morgens", "vormittags", "in der fruh"],
  afternoon: ["this afternoon", "in the afternoon", "nachmittags", "am nachmittag"],
  evening: ["this evening", "tonight", "tonite", "at night", "abends", "am abend", "heute nacht"]
}
var NL_DAY_PART_WORDS = {
  morning: ["morning", "vormittag", "fruh"],
  afternoon: ["afternoon", "nachmittag"],
  evening: ["evening", "night", "abend", "nacht"]
}

// A time as written: "9:30", "9.30", "9am", "9 Uhr", "9uhr". A bare "9" is
// only a time with a preposition in front of it, or inside a till phrase.
var NL_CLOCK = "\\d{1,2}:\\d{2}|\\d{1,2}\\.\\d{2}|\\d{1,2}\\s?(?:am|pm)|\\d{1,2}\\s?uhr"
var NL_CLOCK_SIDE = "\\d{1,2}(?::\\d{2})?\\s?(?:am|pm)?"
var NL_AMOUNT = "(?:\\d+\\s?(?:m|min|mins|minuten?|h|hr|hrs|std|stunden?|stunde)"
  + "|half an hour|eine halbe stunde|halbe stunde|an hour|eine stunde|one hour)"
var NL_RELATIVE_UNIT = "min|mins|minute|minuten|m|h|std|stunde|stunden|hr|hrs|hour|hours"
  + "|tag|tage|tagen|day|days"

// Hours written out, for "half past three" and "drei Uhr". Folded spellings.
var NL_HOUR_WORDS = {
  one: 1, two: 2, three: 3, four: 4, five: 5, six: 6, seven: 7, eight: 8, nine: 9, ten: 10,
  eleven: 11, twelve: 12,
  ein: 1, eins: 1, eine: 1, zwei: 2, drei: 3, vier: 4, funf: 5, sechs: 6, sieben: 7, acht: 8,
  neun: 9, zehn: 10, elf: 11, zwolf: 12
}
var NL_HOUR_ALT = nlAlternation(Object.keys(NL_HOUR_WORDS))

function nlAlternation(list) {
  var words = list.slice().sort(function (a, b) { return b.length - a.length })
  return "(?:" + words.join("|") + ")"
}

function nlWeekdayWords(kind) {
  var words = []
  for (var i = 0; i < NL_WEEKDAYS.length; i++)
    words = words.concat(NL_WEEKDAYS[i][kind])
  return words
}

function nlMonthWords() {
  var words = []
  for (var i = 0; i < NL_MONTHS.length; i++) words = words.concat(NL_MONTHS[i])
  return words
}

var NL_WEEKDAY_ALT = nlAlternation(nlWeekdayWords("names").concat(nlWeekdayWords("shorts")))
var NL_WEEKDAY_FULL_ALT = nlAlternation(nlWeekdayWords("names"))
var NL_WEEKDAY_SHORT = nlWeekdayWords("shorts")
var NL_MONTH_ALT = nlAlternation(nlMonthWords())
var NL_DAY_LEAD_ALT = nlAlternation(NL_DAY_LEAD_WORDS.concat(NL_WORDS.next))

var NL_PATTERNS = {
  // Flags, in the order they can be confused with one another.
  calendarFlag: /\s(\/[^\s]+)(?=\s)/,
  alertFlag: /\s(-a\d+[mhd])(?=\s)/,
  repeatFlag: /\s(-r\d+[dwmy])(?=\s)/,
  durationFlag: /\s-(\d+)(m|min|h|std)?(?=\s)/,
  priorityFlag: /\s(!{1,3})(?=\s)/,
  link: /\s(https?:\/\/[^\s]+)(?=\s)/,

  // A calendar by the name the mirror gave it, after "in"/"im" only, so
  // "12:30 in Café Central" stays a location.
  calendarName: /\s(?:in|im)\s([^\s]+)(?=\s)/,

  repeatPhrase: new RegExp("\\s(" + nlAlternation(nlWordsOf(NL_REPEAT_PHRASES)) + ")(?=\\s)"),
  every: new RegExp("\\s" + nlAlternation(NL_EVERY_WORDS) + "(?=\\s)"),
  weekdayAfter: new RegExp("^\\s(?:(?:" + NL_DAY_LEAD_ALT + ")\\s+)?(" + NL_WEEKDAY_ALT + ")(?=\\s)"),

  relative: new RegExp("\\s(?:in)\\s(\\d{1,2})\\s?(" + NL_RELATIVE_UNIT + ")(?=\\s)"),
  now: /\s(?:now|jetzt)(?=\s)/,

  clockRange: new RegExp("\\s(?:(?:" + NL_DAY_LEAD_ALT + "|um|at|von|from|das|ab|entre|@)\\s)?("
    + NL_CLOCK_SIDE + ")\\s?(?:to|bis|till|until|[-\u2013])\\s?(" + NL_CLOCK_SIDE + ")(?:\\s?uhr)?(?=\\s)"),

  till: new RegExp("\\s" + nlAlternation(NL_WORDS.till) + "(?=\\s)"),
  clock: new RegExp("\\s(" + NL_CLOCK + ")(?=\\s)"),
  clockPrefixed: new RegExp("\\s(?:(?:um|at|von|from|@)\\s)?(" + NL_CLOCK + ")(?=\\s)"),
  bareHour: /\s(\d{1,2})(?=\s)/,
  bareHourPrefixed: /\s(?:um|at|von|from|@)\s(\d{1,2})(?=\s)/,
  // "half past three", "quarter to four", "halb vier", "drei Uhr". The hour
  // word needs the phrase around it: a bare "four" is a title word.
  wordClock: new RegExp("\\s(?:(?:um|at|@)\\s)?(?:(half past|quarter past|quarter to|halb|viertel nach|viertel vor)\\s("
    + NL_HOUR_ALT + ")|(\\d{1,2}|" + NL_HOUR_ALT + ")\\s?(?:o'clock|oclock|uhr))(?=\\s)"),
  noon: /\s(noon|mittag)(?=\s)/,

  isoDate: /\s(?:(?:am|on|den)\s)?(\d{4})-(\d{1,2})-(\d{1,2})(?=\s)/,
  dayMonth: /\s(?:(?:am|on|den)\s)?(\d{1,2})\.(\d{1,2})\.?(?:\s?(20\d{2}))?(?=\s)/,
  dayMonthName: new RegExp("\\s(\\d{1,2})\\.?\\s*(" + NL_MONTH_ALT + ")(?:\\s(\\d{4}))?(?=\\s)"),
  monthDay: new RegExp("\\s(" + NL_MONTH_ALT + ")\\s(\\d{1,2})(?:st|nd|rd|th)?(?:,?\\s(\\d{4}))?(?=\\s)"),
  dayAfterTomorrow: /\s(?:day after tomorrow|ubermorgen)(?=\s)/,
  tomorrow: /\s(?:tomorrow|morgen)(?=\s)/,
  today: /\s(?:today|heute)(?=\s)/,
  yesterday: /\s(?:yesterday|gestern)(?=\s)/,
  nextWeek: new RegExp("\\s" + nlAlternation(NL_WORDS.nextWeek) + "(?=\\s)"),
  weekday: new RegExp("\\s(?:(?:" + NL_DAY_LEAD_ALT + ")\\s+)?(" + NL_WEEKDAY_ALT + ")(?=\\s)"),
  // The same, with the day word in front of the name kept apart: "next
  // friday" is a week on, "on friday" is not.
  weekdayLead: new RegExp("\\s((?:" + NL_DAY_LEAD_ALT + "))\\s+(" + NL_WEEKDAY_ALT + ")(?=\\s)"),
  // "mondays", "freitags": the adverbial form is a weekly repeat on that day.
  weekdayPlural: new RegExp("\\s(?:(?:on|am|an)\\s)?(" + NL_WEEKDAY_FULL_ALT + ")s(?=\\s)"),
  monthDayOnly: /\s(?:am|on the|den)\s(\d{1,2})(?:st|nd|rd|th)?\.?(?=\s)/,
  weekend: /\s(?:this|next|am|dieses|kommendes)\s(?:weekend|wochenende)(?=\s)/,

  dayPartPhrase: new RegExp("\\s(" + nlAlternation(nlWordsOf(NL_DAY_PART_PHRASES)) + ")(?=\\s)"),
  dayPartWord: new RegExp("\\s(" + nlAlternation(nlWordsOf(NL_DAY_PART_WORDS)) + ")(?=\\s)"),

  durationPrefixed: new RegExp("\\s(?:for|fur)\\s(" + NL_AMOUNT + ")(?=\\s)"),
  durationPlain: /\s(\d+)\s?(m|min|mins|minuten?|h|hr|hrs|std|stunden?|stunde)(?=\s)/
}

// Every word of a { key: [words] } table, in one list.
function nlWordsOf(table) {
  var words = []
  for (var key in table) words = words.concat(table[key])
  return words
}

// The key of `table` whose list holds `phrase`.
function nlKeyOf(phrase, table) {
  for (var key in table)
    if (table[key].indexOf(phrase) !== -1) return key
  return null
}

// Lower case, umlauts flattened, sentence punctuation turned into word
// breaks. Same length as the input, always: "1,5h" and "14:30" keep their
// comma and colon, "morgen," loses its.
function nlFold(text) {
  var lower = text.toLowerCase()
  if (lower.length !== text.length) lower = text
  return lower.replace(/[^\x00-\x7f]/g, function (c) { return NL_FOLDED_LETTERS[c] || c })
    .replace(/[;?()\[\]"]/g, " ")
    .replace(/[.,:](?=\s|$)/g, " ")
}

function nlScanner(text) {
  var padded = " " + text + " "
  var roles = []
  for (var i = 0; i < padded.length; i++) roles.push(null)
  return {
    original: padded,
    folded: " " + nlFold(text) + " ",
    roles: roles,
    lastFrom: -1,
    lastTo: -1,
    // Set by a day read that is also a rule ("freitags").
    weekdayRepeat: false
  }
}

function nlSnapshot(scanner) {
  return { original: scanner.original, folded: scanner.folded, lastFrom: scanner.lastFrom, lastTo: scanner.lastTo }
}

function nlRestore(scanner, saved) {
  scanner.original = saved.original
  scanner.folded = saved.folded
  scanner.lastFrom = saved.lastFrom
  scanner.lastTo = saved.lastTo
}

function nlBlank(scanner, from, to) {
  var spaces = new Array(to - from + 1).join(" ")
  scanner.original = scanner.original.substring(0, from) + spaces + scanner.original.substring(to)
  scanner.folded = scanner.folded.substring(0, from) + spaces + scanner.folded.substring(to)
}

// Marks a stretch of the folded text as playing `role`. Coordinates are the
// padded ones the scanner works in.
function nlNoteRange(scanner, from, to, role) {
  for (var i = Math.max(0, from); i < Math.min(to, scanner.roles.length); i++) scanner.roles[i] = role
}

// The phrase `take` or `peek` accepted last.
function nlNote(scanner, role) {
  nlNoteRange(scanner, scanner.lastFrom, scanner.lastTo, role)
}

// Where a phrase ends in what was typed. The fold turns a sentence full stop
// into a space, so a match can stop one character short of the dot that closed
// it ("15.3."); the dot belongs to the phrase, not to the title.
function nlPhraseEnd(scanner, index) {
  var end = index
  while (end < scanner.original.length && /[.,;:]/.test(scanner.original.charAt(end))) end += 1
  return end
}

// Finds `pattern` (which starts with a whitespace character) anywhere in the
// folded text and hands each match, and where it starts, to `read` until one
// is accepted (anything but null); only that one is blanked out.
function nlTake(scanner, pattern, read) {
  for (var offset = 0; ; ) {
    var match = scanner.folded.substring(offset).match(pattern)
    if (!match) return null
    var at = offset + match.index
    var value = read(match, at)
    if (value !== null && value !== undefined) {
      var end = nlPhraseEnd(scanner, at + match[0].length)
      scanner.lastFrom = at + 1
      scanner.lastTo = end
      nlBlank(scanner, at + 1, end)
      return value
    }
    offset = at + 1
  }
}

// The same, but only where the phrase actually starts at `from`: the word
// the previous take ended in front of. "till" takes what follows it, never a
// date three words down the line.
function nlTakeAdjacent(scanner, from, pattern, read) {
  if (from < 0 || from >= scanner.folded.length) return null
  var match = scanner.folded.substring(from).match(pattern)
  if (!match || match.index !== 0) return null
  var value = read(match, from)
  if (value === null || value === undefined) return null
  var end = nlPhraseEnd(scanner, from + match[0].length)
  scanner.lastFrom = from + 1
  scanner.lastTo = end
  nlBlank(scanner, from + 1, end)
  return value
}

// Reads a phrase without blanking it: the word plays a role but stays in the
// title, because it is also an ordinary word ("mittag" is both lunch and noon).
function nlPeek(scanner, pattern, read) {
  var match = scanner.folded.match(pattern)
  if (!match) return null
  var value = read(match, match.index)
  if (value === null || value === undefined) return null
  scanner.lastFrom = match.index + 1
  scanner.lastTo = match.index + match[0].length
  return value
}

// The text no read took, as tokens with their offsets in what was typed. A
// token that is nothing but punctuation is what a blanked phrase left behind
// ("Flight to Berlin [15.3]."), so it goes with the phrase.
function nlLeftover(scanner) {
  var all = []
  var out = []
  var scan = /\S+/g
  var found
  while ((found = scan.exec(scanner.original)) !== null) {
    var token = { text: found[0], from: found.index - 1, to: found.index + found[0].length - 1 }
    all.push(token)
    if (!/^[.,;:!?]+$/.test(token.text)) out.push(token)
  }
  return out.length ? out : all
}

// The colours, one run per role, whitespace between two runs of the same role
// included, "next monday" is one date, not two words that happen to agree.
function nlSegments(scanner) {
  var out = []
  var last = null
  for (var i = 0; i < scanner.roles.length; i++) {
    var role = scanner.roles[i]
    if (role === null) continue
    if (last && last.role === role) last.end = i
    else {
      last = { start: i - 1, end: i, role: role }
      out.push(last)
    }
  }
  return mergeSegments(out)
}

function nlWeekdayIndex(word) {
  var text = String(word || "").toLowerCase()
  for (var i = 0; i < NL_WEEKDAYS.length; i++)
    if (NL_WEEKDAYS[i].names.indexOf(text) !== -1 || NL_WEEKDAYS[i].shorts.indexOf(text) !== -1)
      return NL_WEEKDAYS[i].index
  return -1
}

function nlMonthIndex(word) {
  var text = String(word || "").toLowerCase()
  for (var i = 0; i < NL_MONTHS.length; i++)
    if (NL_MONTHS[i].indexOf(text) !== -1) return i
  return -1
}

function nlIs(word, group) {
  return NL_WORDS[group].indexOf(String(word || "").toLowerCase()) !== -1
}

function nlTimeLabel(minutesOfDay) {
  return pad2(Math.floor(minutesOfDay / 60)) + ":" + pad2(minutesOfDay % 60)
}

function nlValidDay(year, month, day) {
  var date = new Date(year, month, day)
  return date.getFullYear() === year && date.getMonth() === month && date.getDate() === day ? date : null
}

function nlOffset(date, days) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate() + days)
}

// A date given without a year means the next time it comes round.
function nlUpcoming(today, month, day, year) {
  if (year !== undefined && year !== null && year !== "") {
    var y = parseInt(year, 10)
    return nlValidDay(y < 100 ? 2000 + y : y, month, day)
  }
  var date = nlValidDay(today.getFullYear(), month, day)
  if (date && date < today) date = nlValidDay(today.getFullYear() + 1, month, day)
  return date
}

// "am 15.": this month, or the next month that has that day once it has gone.
function nlNextMonthDay(today, day) {
  for (var ahead = 0; ahead <= 12 && day >= 1 && day <= 31; ahead++) {
    var month = new Date(today.getFullYear(), today.getMonth() + ahead, 1)
    var candidate = nlValidDay(month.getFullYear(), month.getMonth(), day)
    if (candidate && candidate >= today) return candidate
  }
  return null
}

// Next occurrence of a weekday on or after the base day (same day counts:
// "standup friday" clicked on friday means today).
function nlNextWeekday(base, weekday) {
  var d = new Date(base.getFullYear(), base.getMonth(), base.getDate())
  d.setDate(d.getDate() + ((weekday - d.getDay() + 7) % 7))
  return d
}

// A single day word, "today", "morgen", a weekday name, "15.3." or an ISO
// date, as a Date, or null when the word is not one of those. `next` pushes
// a named weekday or an explicit date a week on; "next today" is not a thing,
// so the relative words ignore it.
function nlDayFrom(word, base, next) {
  var lower = String(word || "").toLowerCase().replace(/[,.;]$/, "")
  var midnight = new Date(base.getFullYear(), base.getMonth(), base.getDate())

  if (nlIs(lower, "today")) return midnight
  if (nlIs(lower, "tomorrow")) {
    midnight.setDate(midnight.getDate() + 1)
    return midnight
  }

  var weekday = nlWeekdayIndex(lower)
  if (weekday !== -1) {
    var wd = nlNextWeekday(base, weekday)
    if (next) wd.setDate(wd.getDate() + 7)
    return wd
  }

  var explicit = nlExplicitDate(lower, base.getFullYear())
  if (!explicit || isNaN(explicit.date.getTime())) return null
  var d = explicit.date
  // "15.3." said in August means the March that is coming, not the one that
  // went. Only yearless dates roll forward; ISO means what it says.
  if (explicit.yearless && d.getTime() < midnight.getTime()) d.setFullYear(d.getFullYear() + 1)
  if (next) d.setDate(d.getDate() + 7)
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

// A clock as written ("9:30", "9.30", "9am", "9 Uhr", "9uhr"), in minutes
// after midnight, or null. A bare hour is no clock: it needs a preposition,
// a range or a till phrase to say it is one.
function nlClockMinutes(phrase) {
  var text = String(phrase === undefined || phrase === null ? "" : phrase)
    .toLowerCase().replace(/\s+/g, "").replace(/uhr$/, "")
  var match = /^(\d{1,2})(?:[:.](\d{2}))?(am|pm)?$/.exec(text)
  if (!match) return null
  var hours = parseInt(match[1], 10)
  var minutes = match[2] ? parseInt(match[2], 10) : 0
  if (match[3] === "pm" && hours < 12) hours += 12
  if (match[3] === "am" && hours === 12) hours = 0
  if (hours > 24 || minutes > 59) return null
  return hours * 60 + minutes
}

// "45m", "2h", "2 h", "half an hour", "eine halbe stunde": minutes, or null.
function nlDurationMinutes(phrase) {
  var text = String(phrase || "").trim()
  if (/^(?:half an hour|halbe stunde|eine halbe stunde)$/.test(text)) return 30
  if (/^(?:an hour|eine stunde|one hour)$/.test(text)) return 60
  var match = /^(\d+)\s?(.*)$/.exec(text)
  if (!match) return null
  var unit = match[2]
  return parseInt(match[1], 10) * (/^(?:h|hr|hrs|std|stunde|stunden)$/.test(unit) ? 60 : 1)
}

// "20 min", "2 hours", "3 days": minutes from now, or null.
function nlRelativeMinutes(number, unit) {
  var count = parseInt(number, 10)
  if (!(count > 0)) return null
  if (/^(?:min|mins|minute|minuten|m)$/.test(unit)) return count
  if (/^(?:h|std|stunde|stunden|hr|hrs|hour|hours)$/.test(unit)) return count * 60
  if (/^(?:tag|tage|tagen|day|days)$/.test(unit)) return count * 24 * 60
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
    // Teams and invites only mean something on a Microsoft 365 calendar, so
    // they stay empty until one is picked, buildQuickAddRequest gates them
    // on the roster row, and requestToArgs sends only what is there.
    teams: false,
    // An event that is already a Teams meeting stays one: there is no off
    // (a Microsoft 365 rule), so the pane locks the switch instead.
    teamsLocked: false,
    invitees: [],
    originalInvitees: [],
    segments: []
  }
}


// ---- The reads. Each takes what it understands out of the scanner and hands
//      back the value the draft wants; the caller paints the role.

function nlReadFlags(scanner, draft) {
  var name = nlTake(scanner, NL_PATTERNS.calendarFlag, function (m, at) {
    return scanner.original.substring(at + 1, at + m[0].length).replace(/^\//, "").replace(/[,.;]$/, "")
  })
  if (name !== null) {
    draft.calendarName = name
    nlNote(scanner, "calendar")
  }

  var alert = nlTake(scanner, NL_PATTERNS.alertFlag, function (m, at) {
    return scanner.original.substring(at + 1, at + m[0].length).toLowerCase()
  })
  if (alert !== null) {
    var alertUnit = alert.charAt(alert.length - 1)
    draft.alertMinutes = parseInt(alert.replace(/[^\d]/g, ""), 10)
      * (alertUnit === "d" ? 1440 : alertUnit === "h" ? 60 : 1)
    nlNote(scanner, "alert")
  }

  var repeat = nlTake(scanner, NL_PATTERNS.repeatFlag, function (m, at) {
    return scanner.original.substring(at + 1, at + m[0].length).toLowerCase()
  })
  if (repeat !== null) {
    var repeatUnit = repeat.charAt(repeat.length - 1)
    draft.recurrence = {
      freq: repeatUnit === "d" ? "daily" : repeatUnit === "w" ? "weekly" : repeatUnit === "m" ? "monthly" : "yearly",
      interval: parseInt(repeat.replace(/[^\d]/g, ""), 10)
    }
    nlNote(scanner, "repeat")
  }

  var length = nlTake(scanner, NL_PATTERNS.durationFlag, function (m, at) {
    return scanner.original.substring(at + 1, at + m[0].length).toLowerCase()
  })
  if (length !== null) {
    var lengthUnit = (length.replace(/^-\d+/, "").charAt(0) || "m")
    draft.durationMinutes = parseInt(length.replace(/[^\d]/g, ""), 10) * (lengthUnit === "h" ? 60 : 1)
    nlNote(scanner, "duration")
  }

  var priority = nlTake(scanner, NL_PATTERNS.priorityFlag, function (m) { return m[1].length })
  if (priority !== null) {
    draft.priority = priority === 1 ? "low" : priority === 2 ? "medium" : "high"
    nlNote(scanner, "priority")
  }

  // A pasted meeting link is a part of its own: it never belongs in the
  // title, and the bar's join button reads it back off the created event.
  var link = nlTake(scanner, NL_PATTERNS.link, function (m, at) {
    return safeLinkUrl(scanner.original.substring(at + 1, at + m[0].length).replace(/[),.;]+$/, ""))
  })
  if (link !== null) {
    draft.link = link
    nlNote(scanner, "link")
  }
}

// A calendar named after "in"/"im", but only one the mirror actually has, so
// "12:30 in Café Central" stays a location. A prefix of a real name counts.
function nlReadCalendarName(scanner, draft, knownCalendars) {
  if (!knownCalendars || !knownCalendars.length) return
  var name = nlTake(scanner, NL_PATTERNS.calendarName, function (m) {
    var probe = m[1]
    for (var i = 0; i < knownCalendars.length; i++) {
      var folded = String(knownCalendars[i] || "").toLowerCase()
      if (!folded) continue
      if (folded === probe || (probe.length >= 2 && folded.indexOf(probe) === 0)) return knownCalendars[i]
    }
    return null
  })
  if (name !== null) {
    draft.calendarName = name
    nlNote(scanner, "calendar")
  }
}

// "every monday" takes only the "every" and returns true, so the day read can
// still see the weekday and the event lands on the right day. A preset phrase
// ("every day", "weekly") sets the rule itself and returns false.
function nlReadRepeat(scanner, draft) {
  var preset = nlTake(scanner, NL_PATTERNS.repeatPhrase, function (m, at) {
    var phrase = m[1]
    if (at === 0 && NL_REPEAT_TITLE_WORDS.indexOf(phrase) !== -1) return null
    return nlKeyOf(phrase, NL_REPEAT_PHRASES)
  })
  if (preset !== null) {
    draft.recurrence = { freq: preset, interval: 1 }
    nlNote(scanner, "repeat")
    return false
  }
  var every = nlTake(scanner, NL_PATTERNS.every, function (m, at) {
    return NL_PATTERNS.weekdayAfter.test(scanner.folded.substring(at + m[0].length)) ? true : null
  })
  if (every === null) return false
  nlNote(scanner, "repeat")
  return true
}

// "in 20 min", "in 2 hours", "in 3 days", "now": { at } for a start, { date }
// for a day, or null.
function nlReadRelative(scanner, now, base) {
  var found = nlTake(scanner, NL_PATTERNS.relative, function (m) {
    var minutes = nlRelativeMinutes(m[1], m[2])
    if (minutes === null) return null
    if (minutes < 1440) return { at: new Date(now.getTime() + minutes * 60000) }
    return { date: nlOffset(new Date(base.getFullYear(), base.getMonth(), base.getDate()), Math.round(minutes / 1440)) }
  })
  if (found !== null) return found
  return nlTake(scanner, NL_PATTERNS.now, function () { return { at: now } })
}

// "9am to 5pm", "12 bis 13 Uhr", "21-2", "von 12 bis 13": both sides in one
// phrase, so a range can never be read as a start plus a stray end.
function nlReadClockRange(scanner) {
  return nlTake(scanner, NL_PATTERNS.clockRange, function (m) {
    var from = nlClockMinutes(m[1])
    var to = nlClockMinutes(m[2])
    if (from === null || to === null) return null
    return { start: from, end: to }
  })
}

// The end of a till phrase: a clock as written, or the bare hour "till 11"
// that only a till phrase may read as a time.
function nlReadEndClock(scanner, from) {
  var strict = nlTakeAdjacent(scanner, from, NL_PATTERNS.clock, function (m) { return nlClockMinutes(m[1]) })
  if (strict !== null) return strict
  return nlTakeAdjacent(scanner, from, NL_PATTERNS.bareHour, function (m) { return nlClockMinutes(m[1]) })
}

// One day phrase, in the order that keeps "15.3." a date. `from` makes it
// adjacent to a word already taken; `role` paints it, or null when the caller
// paints the whole phrase itself.
function nlReadDay(scanner, base, role, from) {
  var today = new Date(base.getFullYear(), base.getMonth(), base.getDate())
  var steps = [
    [NL_PATTERNS.isoDate, function (m) {
      return nlValidDay(parseInt(m[1], 10), parseInt(m[2], 10) - 1, parseInt(m[3], 10))
    }],
    [NL_PATTERNS.dayMonth, function (m) {
      return nlUpcoming(today, parseInt(m[2], 10) - 1, parseInt(m[1], 10), m[3])
    }],
    [NL_PATTERNS.dayMonthName, function (m) {
      return nlUpcoming(today, nlMonthIndex(m[2]), parseInt(m[1], 10), m[3])
    }],
    [NL_PATTERNS.monthDay, function (m) {
      return nlUpcoming(today, nlMonthIndex(m[1]), parseInt(m[2], 10), m[3])
    }],
    [NL_PATTERNS.dayAfterTomorrow, function () { return nlOffset(today, 2) }],
    [NL_PATTERNS.tomorrow, function () { return nlOffset(today, 1) }],
    [NL_PATTERNS.today, function () { return today }],
    [NL_PATTERNS.yesterday, function () { return nlOffset(today, -1) }],
    // Next week starts on the coming Monday, never today.
    [NL_PATTERNS.nextWeek, function () { return nlOffset(today, (1 - today.getDay() + 7) % 7 || 7) }],
    // "mondays", "freitags", "on fridays": a weekly repeat on that day, so
    // the day read names both the rule and the first occurrence.
    [NL_PATTERNS.weekdayPlural, function (m) {
      var plural = nlWeekdayIndex(m[1])
      if (plural === -1) return null
      scanner.weekdayRepeat = true
      return nlNextWeekday(today, plural)
    }],
    // A weekday is the next one coming, today included; "next friday" skips
    // today, so on a Friday it means a week from now.
    [NL_PATTERNS.weekdayLead, function (m, at) {
      return nlWeekdayAt(scanner, m[2], m[1], at + m[0].length, today)
    }],
    [NL_PATTERNS.weekday, function (m, at) {
      return nlWeekdayAt(scanner, m[1], "", at + m[0].length, today)
    }],
    [NL_PATTERNS.monthDayOnly, function (m) { return nlNextMonthDay(today, parseInt(m[1], 10)) }]
  ]
  for (var i = 0; i < steps.length; i++) {
    var day = from === undefined
      ? nlTake(scanner, steps[i][0], steps[i][1])
      : nlTakeAdjacent(scanner, from, steps[i][0], steps[i][1])
    if (day) {
      if (role) nlNote(scanner, role)
      return day
    }
  }
  return null
}

// One weekday name as the day it next falls on. A short name is also an
// ordinary word, so it only counts with a day word in front of it or a number
// right after it.
function nlWeekdayAt(scanner, name, prefix, after, today) {
  var word = String(name || "").toLowerCase()
  var lead = String(prefix || "").toLowerCase()
  var index = nlWeekdayIndex(word)
  if (index === -1) return null
  if (NL_WEEKDAY_SHORT.indexOf(word) !== -1) {
    var said = NL_DAY_LEAD_WORDS.indexOf(lead) !== -1 || nlIs(lead, "next")
    if (!said && !/^\s\d/.test(scanner.folded.substring(after))) return null
  }
  var day = nlNextWeekday(today, index)
  if (nlIs(lead, "next")) day.setDate(day.getDate() + 7)
  return day
}

// "this weekend": the coming Saturday and Sunday, or what is left of it.
function nlReadWeekend(scanner, base) {
  var today = new Date(base.getFullYear(), base.getMonth(), base.getDate())
  return nlTake(scanner, NL_PATTERNS.weekend, function () {
    var weekday = today.getDay()
    if (weekday === 0) return { start: today, end: today }
    if (weekday === 6) return { start: today, end: nlOffset(today, 1) }
    var start = nlOffset(today, 6 - weekday)
    return { start: start, end: nlOffset(start, 1) }
  })
}

// "till 25.8.", "bis morgen 11 Uhr", "trip friday to sunday", "until 3pm".
// The day and the clock after the word both have to sit right behind it.
function nlReadTill(scanner, base, draft) {
  return nlTake(scanner, NL_PATTERNS.till, function (m, at) {
    var from = at + m[0].length
    var saved = nlSnapshot(scanner)
    var endDay = nlReadDay(scanner, base, null, from)
    var endClock = endDay ? nlReadEndClock(scanner, scanner.lastTo) : nlReadEndClock(scanner, from)
    if (!endDay && endClock === null) {
      nlRestore(scanner, saved)
      return null
    }
    nlNoteRange(scanner, at + 1, scanner.lastTo, endClock === null ? "date" : "time")
    if (endClock !== null) {
      draft.endTime = nlTimeLabel(endClock)
      draft.allDay = false
    }
    if (endDay) draft.endDateKey = keyForDate(endDay)
    return true
  })
}

// "morning", "tonight", "am abend": a NL_DAY_PART_START key, or null. A bare
// word only counts right after the day, which ended at `dateEnd`.
function nlReadDayPart(scanner, dateEnd) {
  var part = nlTake(scanner, NL_PATTERNS.dayPartPhrase, function (m) {
    return nlKeyOf(m[1], NL_DAY_PART_PHRASES)
  })
  if (part !== null) return part
  if (dateEnd < 0) return null
  return nlTake(scanner, NL_PATTERNS.dayPartWord, function (m, at) {
    if (at < dateEnd || scanner.folded.substring(dateEnd, at).trim() !== "") return null
    return nlKeyOf(m[1], NL_DAY_PART_WORDS)
  })
}

// One clock: a time as written, else an hour said in words, else a bare hour
// a preposition anchors ("um 8", "at 9").
function nlReadTime(scanner) {
  var words = nlReadWordClock(scanner)
  if (words !== null) return words
  var strict = nlTake(scanner, NL_PATTERNS.clockPrefixed, function (m) { return nlClockMinutes(m[1]) })
  if (strict !== null) return strict
  return nlTake(scanner, NL_PATTERNS.bareHourPrefixed, function (m) { return nlClockMinutes(m[1]) })
}

// "half past three" (3:30), "quarter to four" (3:45), "halb vier" (3:30, the
// German way round: half an hour before four), "drei Uhr" (3:00).
function nlReadWordClock(scanner) {
  return nlTake(scanner, NL_PATTERNS.wordClock, function (m) {
    var said = String(m[2] || m[3] || "").toLowerCase()
    var hour = /^\d+$/.test(said) ? parseInt(said, 10) : NL_HOUR_WORDS[said]
    if (!hour) return null
    if (!m[1]) return hour * 60
    if (m[1] === "half past") return hour * 60 + 30
    if (m[1] === "quarter past" || m[1] === "viertel nach") return hour * 60 + 15
    if (m[1] === "halb") return (hour - 1) * 60 + 30
    return (hour - 1) * 60 + 45
  })
}

// A length: "for 90m", "für 2 h", "for half an hour", "45m", "2h".
function nlReadDuration(scanner) {
  var length = nlTake(scanner, NL_PATTERNS.durationPrefixed, function (m) {
    return nlDurationMinutes(m[1])
  })
  if (length !== null) return length
  return nlTake(scanner, NL_PATTERNS.durationPlain, function (m) {
    return nlDurationMinutes(m[1] + m[2])
  })
}

// The whole line as a draft. The clicked day anchors everything the phrase
// does not name itself: a time with no day is that day, a day with no time is
// all day, and a length never moves either.
function parseEventPhrase(text, dayKey, nowMs, knownCalendars) {
  var raw = String(text === undefined || text === null ? "" : text).trim()
  if (!raw) return null
  var now = new Date(isFinite(nowMs) ? nowMs : Date.now())
  var base = dateFromKey(dayKey, null) || new Date(now.getFullYear(), now.getMonth(), now.getDate())

  var draft = emptyDraft("event", keyForDate(base))
  var known = []
  for (var k = 0; knownCalendars && k < knownCalendars.length; k++)
    known.push(String(knownCalendars[k] || ""))

  var scanner = nlScanner(raw)

  // ---- Flags first: /calendar, -aN[mhd], -rN[dwmy], -Nm, !, a pasted link.
  nlReadFlags(scanner, draft)

  // ---- A calendar named in words, only one the mirror has.
  nlReadCalendarName(scanner, draft, known)

  // ---- Repeats in words. "every monday" leaves the weekday to the day read.
  var weeklyOnDay = nlReadRepeat(scanner, draft)

  // ---- "in 20 min", "now": a start that names its own day.
  var relative = nlReadRelative(scanner, now, base)
  if (relative !== null) {
    if (relative.at) {
      var landed = keyForDate(relative.at)
      draft.dateKey = landed
      draft.startTime = nlTimeLabel(relative.at.getHours() * 60 + relative.at.getMinutes())
      draft.allDay = false
      // "in 20 min" typed at 23:50 lands tomorrow, so the phrase did name a
      // day and the form's date chip has to follow it.
      nlNote(scanner, landed === keyForDate(base) ? "time" : "date")
    } else {
      draft.dateKey = keyForDate(relative.date)
      nlNote(scanner, "date")
    }
  }

  // ---- A range before anything can read one of its sides as a lone time.
  var range = nlReadClockRange(scanner)
  if (range !== null) {
    draft.startTime = nlTimeLabel(range.start)
    draft.endTime = nlTimeLabel(range.end)
    draft.allDay = false
    nlNote(scanner, "time")
  }

  // ---- Till: an end day, an end time, or both.
  nlReadTill(scanner, base, draft)

  // ---- The day, or the weekend.
  var day = null
  var weekend = nlReadWeekend(scanner, base)
  if (weekend !== null) {
    day = weekend.start
    draft.endDateKey = keyForDate(weekend.end)
    nlNote(scanner, "date")
  } else {
    day = nlReadDay(scanner, base, "date")
  }
  if (day) {
    draft.dateKey = keyForDate(day)
    if (weeklyOnDay || scanner.weekdayRepeat) draft.recurrence = { freq: "weekly", interval: 1 }
  }
  var dateEnd = day ? scanner.lastTo : -1

  // ---- "friday evening": the day part, then the clock it may replace.
  var part = nlReadDayPart(scanner, dateEnd)
  if (part !== null) nlNote(scanner, "time")

  if (draft.startTime === null) {
    var start = nlReadTime(scanner)
    if (start !== null) {
      draft.startTime = nlTimeLabel(start)
      draft.allDay = false
      nlNote(scanner, "time")
    }
  }

  // A noon word reads as a sensible title word too ("mittag", "lunch"): take
  // its time, but let the word survive into the title.
  var noon = nlPeek(scanner, NL_PATTERNS.noon, function () { return true })
  if (noon !== null) {
    nlNote(scanner, "time")
    if (draft.startTime === null) {
      draft.startTime = "12:00"
      draft.allDay = false
    }
  }

  if (part !== null && draft.startTime === null) {
    draft.startTime = nlTimeLabel(NL_DAY_PART_START[part])
    draft.allDay = false
  }

  // ---- A length, with or without the word in front of it.
  var length = nlReadDuration(scanner)
  if (length !== null) {
    draft.durationMinutes = length
    nlNote(scanner, "duration")
  }

  // ---- Title and place. A place keyword ("at", "bei") owns everything after
  //      it; without one, a capitalized tail is the place ("12:30 Café
  //      Central"), protected where a name travels with its preposition
  //      ("lunch with Ana" is a title).
  var tokens = nlLeftover(scanner)
  var titleTokens = tokens
  var placeAt = -1
  for (var t = 0; t + 1 < tokens.length; t++) {
    if (nlIs(nlFold(tokens[t].text), "place")) {
      placeAt = t
      break
    }
  }
  if (placeAt !== -1) {
    draft.location = nlJoinTokens(tokens.slice(placeAt + 1), true)
    for (var l = placeAt; l < tokens.length; l++)
      nlNoteRange(scanner, tokens[l].from + 1, tokens[l].to + 1, "location")
    titleTokens = tokens.slice(0, placeAt)
  } else {
    var PROTECT = { with: 1, mit: 1, to: 1, zu: 1, nach: 1, von: 1, from: 1 }
    var guarded = []
    for (var g = 0; g < tokens.length; g++) {
      var word = nlFold(tokens[g].text)
      var before = g > 0 ? nlFold(tokens[g - 1].text) : ""
      guarded.push(PROTECT[word] ? 1
        : (PROTECT[before] && /^[A-ZÄÖÜ]/.test(tokens[g].text) ? 1 : 0))
    }
    var locStart = -1
    var cursor = tokens.length - 1
    while (cursor >= 0 && /^[A-ZÄÖÜ]/.test(tokens[cursor].text) && !guarded[cursor]) {
      locStart = cursor
      cursor -= 1
    }
    if (locStart !== -1 && (cursor + 1 < locStart || locStart > 0)) {
      draft.location = nlJoinTokens(tokens.slice(locStart), true)
      for (var p = locStart; p < tokens.length; p++)
        nlNoteRange(scanner, tokens[p].from + 1, tokens[p].to + 1, "location")
      titleTokens = tokens.slice(0, locStart)
    }
  }

  for (var rest = 0; rest < titleTokens.length; rest++)
    if (scanner.roles[titleTokens[rest].from + 1] === null)
      nlNoteRange(scanner, titleTokens[rest].from + 1, titleTokens[rest].to + 1, "title")

  draft.title = nlJoinTokens(titleTokens)
  if (!draft.title) {
    // Never dead-end: whatever could not be parsed becomes the title whole.
    draft.title = raw.replace(/\s*\/[^ ]+|\s*-a\d+[mhd]|\s*-r\d+[dwmy]/gi, "").trim()
  }
  if (!draft.title) draft.title = raw

  // "trip friday to sunday" read on a Sunday: the Sunday that ends it is the
  // one after the Friday, not the one the phrase was typed on.
  if (draft.endDateKey && draft.endDateKey < draft.dateKey) {
    var endDay = dateFromKey(draft.endDateKey, null)
    for (var week = 0; endDay && week < 53 && keyForDate(endDay) < draft.dateKey; week++)
      endDay = nlOffset(endDay, 7)
    if (endDay) draft.endDateKey = keyForDate(endDay)
  }

  // An end before its start wraps into the next morning, unless the phrase
  // named the day it ends on.
  if (draft.endDateKey) draft.endNextDay = false
  else if (draft.endTime && draft.startTime) draft.endNextDay = draft.endTime <= draft.startTime
  else if (draft.endTime) draft.endNextDay = true

  draft.segments = nlSegments(scanner)
  return draft
}

function nlJoinTokens(tokens, strip) {
  var words = []
  for (var i = 0; i < tokens.length; i++) words.push(tokens[i].text)
  var text = words.join(" ")
  return strip ? text.replace(/[,.;]$/, "") : text
}

// Adjacent tokens playing the same role read as one coloured run, gaps
// included, "next monday" is one date, not two words that happen to agree.
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
  draft.title = String(text === undefined || text === null ? "" : text).trim()
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
  // Teams and the invite list never made it onto an agenda row, so they
  // arrive here, and teams arrives already locked, because an event that
  // is a Teams meeting cannot be turned back.
  draft.teams = !!detail.teams
  draft.teamsLocked = !!detail.teams
  var attendees = Array.isArray(detail.attendees) ? detail.attendees : []
  for (var i = 0; i < attendees.length; i++) {
    var a = attendees[i]
    var addr = String((a && a.address) || "").trim()
    if (!addr) continue
    draft.invitees.push({ address: addr, name: String((a && a.name) || ""), answer: String((a && a.answer) || "none") })
    draft.originalInvitees.push(addr)
  }
  return draft
}

// What the entry pane shows first when a task chip is clicked open for
// editing: the fields the chip already carried. The notes and the link are
// not on it -- they come from `todo view` and draftFromTaskDetail patches
// them in on top.
function draftFromTask(task) {
  var draft = emptyDraft("task", (task && task.dueKey) || "")
  if (!task) return draft
  draft.title = task.title || ""
  draft.calendarName = task.calendarName || null
  // The chip carries the priority word ("high"/"medium"/"low");
  // buildQuickAddRequest turns it back into the number iCalendar wants.
  draft.priority = task.priority || null
  if (task.time) {
    draft.startTime = task.time
    draft.allDay = false
  } else {
    draft.allDay = true
  }
  draft.editingId = String(task.id || "")
  return draft
}

// The rest of draftFromTask's picture, once `todo view` answers.
function draftFromTaskDetail(detail) {
  var draft = emptyDraft("task", "")
  if (!detail) return draft
  draft.description = detail.description || null
  draft.link = safeLinkUrl(detail.url) || null
  return draft
}

// ---- Invitee suggestions ---------------------------------------------------
//
// The same two searches and the same order as the mail composer's
// recipient field: the address book first, then the addresses mail has
// actually been exchanged with. Addresses on the calendar owner's domain
// float up, on the work calendar those are the colleagues, each group
// keeping the order the searches returned them in. The domain comes from
// the roster's `owner`, never a constant.
function inviteeDomain(address) {
  var text = String(address || "")
  var at = text.lastIndexOf("@")
  return at === -1 ? "" : text.slice(at + 1).toLowerCase()
}

function rankInviteeSuggestions(contacts, correspondents, ownerAddress, alreadyPicked, limit) {
  var max = limit === undefined ? 8 : limit
  var picked = {}
  for (var p = 0; p < (alreadyPicked || []).length; p++)
    picked[String(alreadyPicked[p] || "").toLowerCase()] = true
  var seen = {}
  var flat = []
  function push(name, addresses) {
    for (var j = 0; addresses && j < addresses.length; j++) {
      var addr = String(addresses[j] || "").trim()
      if (!addr) continue
      var lc = addr.toLowerCase()
      if (seen[lc] || picked[lc]) continue
      seen[lc] = true
      flat.push({ address: addr, name: String(name || "") })
    }
  }
  for (var i = 0; i < (contacts || []).length; i++) {
    var c = contacts[i]
    if (c) push(c.name, c.emails)
  }
  for (var k = 0; k < (correspondents || []).length; k++) {
    var r = correspondents[k]
    if (r) push(r.name, [r.email])
  }

  var ownerDomain = inviteeDomain(ownerAddress)
  if (!ownerDomain) return flat.slice(0, max)
  var home = []
  var away = []
  for (var n = 0; n < flat.length; n++)
    (inviteeDomain(flat[n].address) === ownerDomain ? home : away).push(flat[n])
  return home.concat(away).slice(0, max)
}

// One glyph per answer, for the pill and wherever attendees are listed:
// ✓ accepted, ? tentative, ✗ declined, and a dot for silence.
function inviteeAnswerMark(answer) {
  return answer === "accepted" ? "✓"
    : answer === "tentative" ? "?"
    : answer === "declined" ? "✗" : "·"
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
  var text = String(title || "").trim()
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
  var text = String(title || "").trim()
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
// been waiting long enough that the write plainly did not land, better a
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

function buildQuickAddRequest(draft, nowMs, calendar) {
  var now = isFinite(nowMs) ? nowMs : Date.now()
  var kind = draft && draft.kind === "task" ? "task" : "event"
  if (!draft) return { ok: false, error: "nothing entered" }

  var title = String(draft.title || "").trim()
  if (!title) return { ok: false, error: "title is empty" }
  if (title.length > 300) return { ok: false, error: "title is too long" }

  var startDate = dateFromKey(draft.dateKey, null)
  if (!startDate) return { ok: false, error: "no valid date" }

  var recurrence = null
  if (draft.recurrence && draft.recurrence.interval >= 1 && draft.recurrence.interval <= 366)
    recurrence = { freq: draft.recurrence.freq, interval: Math.round(draft.recurrence.interval) }

  if (kind === "task") {
    // A task's deadline is the day the pane shows; the hour too when one was
    // typed. "by Friday" and "by 17:00 on Friday" are different promises, so
    // dueHasTime records which was meant. The loose, dateless todo is what
    // the week-bucket + makes (buildQuickTodoRequest), the entry pane always
    // shows a date, so a phrase like "buy milk tomorrow" writes that date.
    var dueHasTime = draft.startTime !== null && draft.startTime !== undefined && draft.startTime !== ""
    var dueMs = nlMsFor(draft.dateKey, dueHasTime ? draft.startTime : null)
    if (!nlValidMs(dueMs, now)) return { ok: false, error: "due date out of range" }
    var prio = draft.priority === "high" ? 1 : draft.priority === "medium" ? 5 : draft.priority === "low" ? 9 : null
    return {
      ok: true,
      request: {
        kind: "task",
        id: draft.editingId || null,
        title: title,
        dueMs: dueMs,
        dueHasTime: dueHasTime,
        description: String(draft.description || "").trim().slice(0, 8000) || null,
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

  var location = String(draft.location || "").trim()
  var description = String(draft.description || "").trim().slice(0, 8000)
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
  // Teams and invites ride only on a Microsoft 365 calendar, the daemon
  // refuses the args on anything else, and a request that carried them
  // anyway would turn a mis-picked calendar into a failed write. The
  // caller passes the picked calendar's roster row.
  if (calendar && calendar.teams) {
    request.teams = !!draft.teams
    request.teamsLocked = !!draft.teamsLocked
    request.invitees = draft.invitees || []
    request.originalInvitees = draft.originalInvitees || []
  }
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
  if (request.teams) bits.push("Teams")
  if (request.invitees && request.invitees.length) bits.push(request.invitees.length + " invited")
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

// One agenda/todo row's time fields as local wall clock, the way the daemon
// takes them back.
function stampLocal(ms, withTime) {
  var d = new Date(ms)
  var day = keyForDate(d)
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
      tasks: kind === "tasks",
      // A Microsoft 365 events calendar, and the address it belongs to,
      // the gate for the Teams row, and the domain invitee suggestions
      // rank by (colleagues first on the work calendar).
      teams: !!row.teams,
      owner: String(row.owner || "")
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
      // take, the uid above is for de-duplicating recurring instances, not
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
      alarms: Array.isArray(row.alarms) ? row.alarms.map(Number).filter(isFinite) : [],
      meetingUrl: safeLinkUrl(row.url) || meetingUrlIn(row.summary, row.location, row.notes)
    }
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

// The three daemon answers as one document, the shape the panel draws from.
// The panel owns the asking; Model owns the shaping.
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
  if (request.kind === "task" && request.action !== "delete") {
    if (request.id) {
      // An edit names every field the pane can hold: what the user left
      // blank is sent as the "none" the daemon reads as "take it off", so a
      // cleared priority or link actually clears. Notes are the exception,
      // an empty box leaves whatever is there, matching event edits.
      var editArgs = { positional: String(request.id), title: String(request.title || "") }
      if (request.dueMs) editArgs.due = stampLocal(request.dueMs, !!request.dueHasTime)
      editArgs.priority = request.priority ? priorityWord(request.priority) : "none"
      if (request.description) editArgs.notes = String(request.description)
      editArgs.url = request.link ? String(request.link) : "none"
      return { cmd: ["todo", "edit"], args: editArgs }
    }
    var taskArgs = { positional: String(request.title || "") }
    if (request.calendarName) taskArgs.list = String(request.calendarName)
    if (request.dueMs) taskArgs.due = stampLocal(request.dueMs, !!request.dueHasTime)
    if (request.priority) taskArgs.priority = priorityWord(request.priority)
    return { cmd: ["todo", "add"], args: taskArgs }
  }
  if (request.action === "delete") {
    if (!request.id) return null
    if (request.kind === "task")
      return { cmd: ["todo", "drop"], args: { positional: String(request.id) } }
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
  // Teams and invites ride only when the request carries them, which
  // buildQuickAddRequest arranges by calendar, on any other calendar the
  // fields are absent and nothing goes out that the daemon would refuse.
  // Teams cannot be taken off once set (a Microsoft 365 rule), so an edit
  // that leaves it on sends nothing; only the switch from off to on is
  // worth a flag.
  if (request.teams && !(editing && request.teamsLocked)) args.teams = true
  var addresses = inviteeAddresses(request.invitees)
  var original = inviteeAddresses(request.originalInvitees)
  var invite = editing ? diffAddresses(addresses, original) : addresses
  if (invite.length) args.invite = invite
  if (editing) {
    var uninvite = diffAddresses(original, addresses)
    if (uninvite.length) args.uninvite = uninvite
  }
  return { cmd: ["event", editing ? "edit" : "add"], args: args }
}

// The addresses off invitee pills, whatever shape arrived, the pane keeps
// {address, name, answer}, and nothing past the address survives the trip.
function inviteeAddresses(invitees) {
  var out = []
  for (var i = 0; invitees && i < invitees.length; i++) {
    var row = invitees[i]
    var addr = String((row && row.address) || row || "").trim()
    if (addr) out.push(addr)
  }
  return out
}

// What is in `wanted` but not `have`, matched case-insensitively the way
// the daemon matches attendee addresses.
function diffAddresses(wanted, have) {
  var known = {}
  for (var i = 0; i < have.length; i++) known[have[i].toLowerCase()] = true
  var out = []
  for (var j = 0; j < wanted.length; j++) {
    var lc = wanted[j].toLowerCase()
    if (!known[lc]) {
      known[lc] = true
      out.push(wanted[j])
    }
  }
  return out
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
// in HTML, so every space but the last in a run becomes a hard one, the
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
// Qt's own date formatting lives, JavaScript's toLocaleDateString options
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
// phrase parser, "tomorrow", "next friday", "15.3.", "2026-03-15", so the
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
  var day = nlDayFrom(words[0], base, next)
  return day ? keyForDate(day) : null
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
// postal address and nothing else, "Garbe Biergarten, Garbenstraße,
// 70599 Stuttgart", never coordinates.

// A query is worth sending once it is a word rather than a keystroke, and
// only the panel's own field ever asks, the phrase never goes to the net.
function shouldSuggestAddress(text) {
  return String(text || "").trim().length >= 3
}

// House number after the street, the way German addresses are written; a
// place name leads when OSM has one, since that is what was typed.
function addressLines(props) {
  var p = props || {}
  var name = String(p.name || "").trim()
  var street = String(p.street || "").trim()
  var number = String(p.housenumber || "").trim()
  var postcode = String(p.postcode || "").trim()
  var city = String(p.city || p.locality || p.district || p.county || "").trim()
  var state = String(p.state || "").trim()
  var country = String(p.country || "").trim()

  var line = street ? (number ? street + " " + number : street) : ""
  var town = postcode && city ? postcode + " " + city : (city || postcode)
  // A place named the same as its street would read twice ("Garbenstraße,
  // Garbenstraße 5"), the street line already says it.
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
  var country = String(props.country || "").trim()
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
// on screen, `applied` is what the previous parse put there, the difference
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

  // The parser hands back a day either way, the clicked one when nothing was
  // said, so only a labelled date segment counts as the phrase speaking.
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
    // An open-ended start still means an hour, or the duration the phrase
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
    nextEvent: nextEvent,
    eventStartMs: eventStartMs,
    eventEndMs: eventEndMs,
    isInProgress: isInProgress,
    formatCountdown: formatCountdown,
    formatStartsIn: formatStartsIn,
    isImminent: isImminent,
    joinButtonLabel: joinButtonLabel,
    truncateTitle: truncateTitle,
    millisUntil: millisUntil,
    shouldAnnounce: shouldAnnounce,
    inAlarmBlip: inAlarmBlip,
    shouldNudge: shouldNudge,
    occurrenceKey: occurrenceKey,
    isDismissed: isDismissed,
    eventDisplayTime: eventDisplayTime,
    safeUrl: safeUrl,
    safeLinkUrl: safeLinkUrl,
    linkProviderLabel: linkProviderLabel,
    safeColor: safeColor,
    eventColor: eventColor,
    dayColors: dayColors,
    isCalendarFileUrl: isCalendarFileUrl,
    meetingUrlFor: meetingUrlFor,
    isMeetingUrl: isMeetingUrl,
    parseEventPhrase: parseEventPhrase,
    fallbackDraft: fallbackDraft,
    draftFromAgendaEvent: draftFromAgendaEvent,
    draftFromEventDetail: draftFromEventDetail,
    draftFromTask: draftFromTask,
    draftFromTaskDetail: draftFromTaskDetail,
    inviteeAnswerMark: inviteeAnswerMark,
    inviteeDomain: inviteeDomain,
    rankInviteeSuggestions: rankInviteeSuggestions,
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
