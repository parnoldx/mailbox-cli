// The slice of ../mailbox.clock/Model.js that this widget's capture path uses:
// the phrase parser and the request builder behind Super+N. Locale- and Qt-free,
// so node can run it. Every function here is the calendar widget's own, kept
// byte for byte — tests/capture.test.js fails when one drifts.

var MS_PER_DAY = 86400000

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

function dateFromKey(dateKey, fallback) {
  var parts = String(dateKey || "").split("-")
  if (parts.length !== 3) return fallback
  var year = parseInt(parts[0], 10)
  var month = parseInt(parts[1], 10)
  var day = parseInt(parts[2], 10)
  if (isNaN(year) || isNaN(month) || isNaN(day)) return fallback
  return new Date(year, month - 1, day)
}

var MINUTE_MS = 60 * 1000

var HOUR_MS = 60 * MINUTE_MS

var DAY_MS = MS_PER_DAY

// A link is supplied by whoever sent the invitation, so treating it as
// trusted input would be a mistake. http as well as https — the entry pane
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

// The same guard under the name the storing paths call it by.
function safeLinkUrl(url) {
  return safeUrl(url)
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

// A single day word — "today", "morgen", a weekday name, "15.3." or an ISO
// date — as a Date, or null when the word is not one of those. `next` pushes
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
  var raw = String(text === undefined || text === null ? "" : text).trim()
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

    var said = nlDayFrom(lower, base, pendingNext)
    if (said) {
      draft.dateKey = keyForDate(said)
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
        endDate = nlDayFrom(rest[q], base, false)
        if (endDate) q += 1
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

  draft.title = kept.join(" ").trim()
  if (!draft.title) {
    // Never dead-end: whatever could not be parsed becomes the title whole.
    draft.title = raw.replace(/\s*\/[^ ]+|\s*-a\d+[mhd]|\s*-r\d+[dwmy]/gi, "").trim()
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

function buildQuickAddRequest(draft, nowMs) {
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
    // the week-bucket + makes (buildQuickTodoRequest) — the entry pane always
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
  return { ok: true, request: request }
}

// One agenda/todo row's time fields as local wall clock, the way the daemon
// takes them back.
function stampLocal(ms, withTime) {
  var d = new Date(ms)
  var day = keyForDate(d)
  if (!withTime) return day
  return day + " " + pad2(d.getHours()) + ":" + pad2(d.getMinutes())
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
      // cleared priority or link actually clears. Notes are the exception —
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

// ---- Merging a parse into what is already on screen ------------------------

function phraseHasRole(segments, role) {
  for (var i = 0; segments && i < segments.length; i++)
    if (segments[i] && segments[i].role === role) return true
  return false
}

if (typeof module !== "undefined") {
  module.exports = {
    keyForDate: keyForDate,
    parseEventPhrase: parseEventPhrase,
    buildQuickAddRequest: buildQuickAddRequest,
    phraseHasRole: phraseHasRole,
    requestToArgs: requestToArgs
  }
}
