const test = require("node:test")
const assert = require("node:assert/strict")
const Model = require("../Model.js")

const MINUTE = 60 * 1000
const now = Date.parse("2026-08-23T10:00:00+02:00")

function event(overrides) {
  return Object.assign({
    id: "standup",
    title: "Standup",
    start: "2026-08-23T10:05:00+02:00",
    end: "2026-08-23T10:20:00+02:00",
    allDay: false,
    meetingUrl: "https://meet.google.com/abc-defg-hij"
  }, overrides)
}

test("nextEvent skips all-day and past events", () => {
  const events = [
    event({ id: "all", allDay: true, start: "2026-08-23", end: "2026-08-24" }),
    event({ id: "past", start: "2026-08-23T09:00:00+02:00", end: "2026-08-23T09:30:00+02:00" }),
    event({ id: "later", start: "2026-08-23T11:00:00+02:00", end: "2026-08-23T11:30:00+02:00" }),
    event({ id: "soon", start: "2026-08-23T10:15:00+02:00", end: "2026-08-23T10:45:00+02:00" })
  ]
  assert.equal(Model.nextEvent(events, now).id, "soon")
})

test("nextEvent keeps the meeting that is already in progress", () => {
  const events = [
    event({ id: "current", start: "2026-08-23T09:55:00+02:00", end: "2026-08-23T10:25:00+02:00" }),
    event({ id: "next", start: "2026-08-23T10:30:00+02:00", end: "2026-08-23T11:00:00+02:00" })
  ]
  assert.equal(Model.nextEvent(events, now).id, "current")
  assert.equal(Model.isInProgress(events[0], now), true)
  assert.equal(Model.nextEvent(events, Date.parse("2026-08-23T10:25:00+02:00")).id, "next")
})

test("formatCountdown uses MeetingBar-style units and now", () => {
  assert.equal(Model.formatCountdown(30 * 1000), "now")
  assert.equal(Model.formatCountdown(4 * MINUTE), "in 4m")
  assert.equal(Model.formatCountdown(60 * MINUTE), "in 1h")
  assert.equal(Model.formatCountdown(90 * MINUTE), "in 1h 30m")
  assert.equal(Model.formatCountdown(-30 * 1000), "now")
  assert.equal(Model.formatCountdown(24 * 60 * MINUTE), null)
})

test("shouldAnnounce covers the lead window and only the first minutes after start", () => {
  const upcoming = event()
  assert.equal(Model.shouldAnnounce(upcoming, now, 5), true)
  assert.equal(Model.shouldAnnounce(upcoming, now, 4), false)
  assert.equal(Model.shouldAnnounce(event({ start: "2026-08-23T09:55:00+02:00", end: "2026-08-23T10:20:00+02:00" }), now, 15, 5), true)
  assert.equal(Model.shouldAnnounce(event({ start: "2026-08-23T09:54:00+02:00", end: "2026-08-23T10:20:00+02:00" }), now, 15, 5), false)
  assert.equal(Model.shouldAnnounce(upcoming, now, 0), false)
})

test("shouldNudge waits out the first minute and gives up after five", () => {
  const current = event({ id: "current", start: "2026-08-23T10:00:00+02:00", end: "2026-08-23T10:25:00+02:00" })
  assert.equal(Model.shouldNudge(current, Date.parse("2026-08-23T10:00:59+02:00")), false)
  assert.equal(Model.shouldNudge(current, Date.parse("2026-08-23T10:01:00+02:00")), true)
  assert.equal(Model.shouldNudge(current, Date.parse("2026-08-23T10:05:00+02:00")), true)
  assert.equal(Model.shouldNudge(current, Date.parse("2026-08-23T10:05:01+02:00")), false)
  assert.equal(Model.shouldNudge(current, now, 10 * MINUTE), false) // grace past the window
})

test("shouldNudge needs a join link and a meeting that is actually running", () => {
  assert.equal(Model.shouldNudge(event({ meetingUrl: "" }), now), false)
  assert.equal(Model.shouldNudge(event({ allDay: true, start: "2026-08-23", end: "2026-08-24" }), now), false)
  assert.equal(Model.shouldNudge(event(), now), false) // not started yet
  assert.equal(
    Model.shouldNudge(event({ start: "2026-08-23T09:00:00+02:00", end: "2026-08-23T09:30:00+02:00" }), now),
    false
  ) // already over
  assert.equal(Model.shouldNudge(null, now), false)
})

test("isDismissed matches one occurrence and then stays quiet", () => {
  const upcoming = event()
  const key = Model.occurrenceKey(upcoming)
  assert.equal(key, "standup|2026-08-23T10:05:00+02:00")
  assert.equal(Model.isDismissed(upcoming, key), true)
  assert.equal(Model.isDismissed(upcoming, ""), false)
  assert.equal(Model.isDismissed(event({ id: "other" }), key), false)
})

test("formatStartsIn spells the Basecamp reminder copy", () => {
  assert.equal(Model.formatStartsIn(30 * 1000), "starts now")
  assert.equal(Model.formatStartsIn(1 * MINUTE), "starts in 1 minute")
  assert.equal(Model.formatStartsIn(14 * MINUTE), "starts in 14 minutes")
  assert.equal(Model.formatStartsIn(60 * MINUTE), "starts in 1 hour")
})

test("isImminent is only the last minute", () => {
  assert.equal(Model.isImminent(5 * MINUTE), false)
  assert.equal(Model.isImminent(1 * MINUTE), false)
  assert.equal(Model.isImminent(30 * 1000), true)
  assert.equal(Model.isImminent(-10 * 1000), true)
})

test("joinButtonLabel names the meeting service", () => {
  assert.equal(Model.joinButtonLabel("https://us02web.zoom.us/j/1"), "Join Zoom")
  assert.equal(Model.joinButtonLabel("https://meet.google.com/abc-defg-hij"), "Join Meet")
  assert.equal(Model.joinButtonLabel("https://teams.microsoft.com/l/meetup-join/x"), "Join Teams")
  assert.equal(Model.joinButtonLabel("https://meet.jit.si/Standup"), "Join Jitsi")
  assert.equal(Model.joinButtonLabel("https://example.com/call"), "Join")
})

test("safeUrl launches plain http(s) meeting links and nothing else", () => {
  assert.equal(Model.meetingUrlFor(event()), "https://meet.google.com/abc-defg-hij")
  // http counts: a link typed into the entry pane may point at the intranet,
  // and refusing to open what we just stored would be the odder of the two.
  assert.equal(Model.safeUrl("http://meet.example.local/abc"), "http://meet.example.local/abc")
  assert.equal(Model.safeUrl("ftp://files.example.com/x"), "")
  assert.equal(Model.safeUrl("https://meet.google.com/abc; rm -rf /"), "")
  assert.equal(Model.safeUrl("javascript:alert(1)"), "")
  assert.equal(Model.meetingUrlFor(null), "")
  assert.equal(Model.meetingUrlFor({
    meetingUrl: "https://us02web.zoom.us/meeting/abc/ics?icsToken=tok"
  }), "")
  assert.equal(Model.meetingUrlFor({
    meetingUrl: "https://us02web.zoom.us/j/88971526434"
  }), "https://us02web.zoom.us/j/88971526434")
})

test("safeColor takes hex calendar colours and nothing else", () => {
  assert.equal(Model.safeColor("#6600cc"), "#6600cc")
  assert.equal(Model.safeColor("  #CEE7FF "), "#CEE7FF")
  assert.equal(Model.safeColor("#abc"), "#abc")
  assert.equal(Model.safeColor("#aabbccdd"), "#aabbccdd")
  assert.equal(Model.safeColor("rebeccapurple"), "")
  assert.equal(Model.safeColor("#12345"), "")
  assert.equal(Model.safeColor(null), "")
})

test("dayColors lists each calendar once, in event order", () => {
  const events = [
    { color: "#6600cc" },
    { color: "#CEE7FF" },
    { color: "#6600cc" },
    { color: "not-a-colour" },
    {}
  ]
  assert.deepEqual(Model.dayColors(events), ["#6600cc", "#CEE7FF"])
  assert.deepEqual(Model.dayColors([]), [])
  assert.deepEqual(Model.dayColors(null), [])
})

test("monthGrid carries the calendar colours of each day", () => {
  const index = Model.indexEventsByDate([
    { dateKey: "2026-08-05", color: "#6600cc" },
    { dateKey: "2026-08-05", color: "#CEE7FF" },
    { dateKey: "2026-08-06", color: "#CEE7FF" }
  ])
  const days = Model.monthGrid(2026, 7, 1, "2026-08-23", index)
    .flatMap(week => week.days)
  const fifth = days.find(day => day.key === "2026-08-05")
  const sixth = days.find(day => day.key === "2026-08-06")
  const seventh = days.find(day => day.key === "2026-08-07")
  assert.deepEqual(fifth.colors, ["#6600cc", "#CEE7FF"])
  assert.deepEqual(sixth.colors, ["#CEE7FF"])
  assert.equal(seventh.hasEvent, false)
  assert.deepEqual(seventh.colors, [])
})

// ---- Natural-language quick-add ------------------------------------------
//
// The right-clicked day anchors everything: 2026-08-23 is a Sunday. nowMs is
// fixed so relative windows (±2 years) stay deterministic.

const nlNow = Date.parse("2026-08-23T10:00:00+02:00")
const nlDay = "2026-08-23"

function parse(text, calendars) {
  return Model.parseEventPhrase(text, nlDay, nlNow, calendars)
}

test("parseEventPhrase reads the C366 event examples in English", () => {
  const d = parse("Meeting with Alex tomorrow 10am /Work")
  assert.equal(d.title, "Meeting with Alex")
  assert.equal(d.dateKey, "2026-08-24")
  assert.equal(d.startTime, "10:00")
  assert.equal(d.allDay, false)
  assert.equal(d.calendarName, "Work")

  const lunch = parse("Lunch with Sarah Friday 12:30 Café Central", ["Personal"])
  assert.equal(lunch.title, "Lunch with Sarah")
  assert.equal(lunch.dateKey, "2026-08-28") // next Friday after Sunday
  assert.equal(lunch.startTime, "12:30")
  assert.equal(lunch.location, "Café Central")

  const dentist = parse("Dentist next Monday 3pm -a1d")
  assert.equal(dentist.title, "Dentist")
  assert.equal(dentist.dateKey, "2026-08-31") // next Monday, one week out
  assert.equal(dentist.startTime, "15:00")
  assert.equal(dentist.alertMinutes, 1440)

  const standup = parse("Team standup next Monday 9am -r1w /Work")
  assert.equal(standup.recurrence.freq, "weekly")
  assert.equal(standup.recurrence.interval, 1)

  const flight = parse("Flight to Berlin 15.3. 7:00-9:30")
  assert.equal(flight.title, "Flight to Berlin")
  assert.equal(flight.dateKey, "2027-03-15") // next occurrence of Mar 15
  assert.equal(flight.startTime, "07:00")
  assert.equal(flight.endTime, "09:30")
})

test("parseEventPhrase reads the C366 examples in German", () => {
  const d = parse("Meeting mit Alex morgen 10 Uhr /Arbeit")
  assert.equal(d.title, "Meeting mit Alex")
  assert.equal(d.dateKey, "2026-08-24")
  assert.equal(d.startTime, "10:00")

  const lunch = parse("Mittagessen mit Sarah Freitag 12:30 Café Central")
  assert.equal(lunch.title, "Mittagessen mit Sarah")
  assert.equal(lunch.dateKey, "2026-08-28")

  const zahn = parse("Zahnarzt nächsten Montag 15 Uhr -a1d")
  assert.equal(zahn.startTime, "15:00")
  assert.equal(zahn.alertMinutes, 1440)

  const standup = parse("Team Standup nächsten Montag 9 Uhr -r1w /Arbeit")
  assert.equal(standup.recurrence.freq, "weekly")

  const flug = parse("Flug nach Berlin 15.3. 7:00-9:30")
  assert.equal(flug.dateKey, "2027-03-15")
})

test("parseEventPhrase reads the C366 task examples", () => {
  const groceries = parse("Buy groceries tomorrow !")
  assert.equal(groceries.title, "Buy groceries")
  assert.equal(groceries.priority, "low")

  const report = parse("Finish report Friday /Work !!")
  assert.equal(report.priority, "medium")
  assert.equal(report.calendarName, "Work")

  const mom = parse("Call mom Sunday 18 Uhr")
  assert.equal(mom.startTime, "18:00")

  const bug = parse("Fix bug in login /Work !!!")
  assert.equal(bug.priority, "high")

  const plants = parse("Water plants nächsten Montag -r1w")
  assert.equal(plants.recurrence.freq, "weekly")
})

test("ends cross midnight and accept explicit days", () => {
  // 21:00 till 2:00 wraps into the next morning.
  let d = parse("party today 21:00 till 2:00")
  assert.equal(d.startTime, "21:00")
  assert.equal(d.endTime, "02:00")
  assert.equal(d.endNextDay, true)

  // …and an explicit next-day end needs no wrap heuristic.
  d = parse("party today till tomorrow 11:00")
  assert.equal(d.endTime, "11:00")
  assert.equal(d.endDateKey, "2026-08-24")
  assert.equal(d.endNextDay, false)

  d = parse("party bis morgen 11 Uhr")
  assert.equal(d.endTime, "11:00")
  assert.equal(d.endDateKey, "2026-08-24")

  // Relaxed bare hour only inside a till expression.
  d = parse("something till 11")
  assert.equal(d.endTime, "11:00")

  // Duration instead of an end.
  d = parse("workshop for 90m")
  assert.equal(d.durationMinutes, 90)
  d = parse("workshop für 2 h")
  assert.equal(d.durationMinutes, 120)

  // Multi-day all-day span.
  d = parse("retreat till 25.8.")
  assert.equal(d.allDay, true)
  assert.equal(d.endDateKey, "2026-08-25")
})

test("ranges take bare hours, to/bis connectors and um/von fillers", () => {
  let d = parse("lunch um 12-13")
  assert.equal(d.title, "lunch")
  assert.equal(d.startTime, "12:00")
  assert.equal(d.endTime, "13:00")
  assert.equal(d.allDay, false)
  assert.equal(d.endNextDay, false)

  d = parse("meeting von 12 bis 13 Uhr")
  assert.equal(d.title, "meeting")
  assert.equal(d.startTime, "12:00")
  assert.equal(d.endTime, "13:00")

  d = parse("standup 9am to 5pm")
  assert.equal(d.title, "standup")
  assert.equal(d.startTime, "09:00")
  assert.equal(d.endTime, "17:00")

  // A preposition anchors a bare hour the way "15 Uhr" anchors itself.
  d = parse("shift von 8")
  assert.equal(d.title, "shift")
  assert.equal(d.startTime, "08:00")
  assert.equal(d.endTime, null)

  // Wrap still applies to connector ranges.
  d = parse("party 21 to 2")
  assert.equal(d.startTime, "21:00")
  assert.equal(d.endTime, "02:00")
  assert.equal(d.endNextDay, true)
})

test("noon words stay in the title and durations set a real end", () => {
  const m = parse("mittag mit Ana morgen 12 bis 13 Uhr /Arbeit")
  assert.equal(m.title, "mittag mit Ana")
  assert.equal(m.startTime, "12:00")
  assert.equal(m.endTime, "13:00")

  // Duration survives as durationMinutes; buildQuickAddRequest turns it
  // into start + 2h, not start + the default hour.
  const w = parse("workshop um 17 für 2h")
  assert.equal(w.title, "workshop")
  assert.equal(w.startTime, "17:00")
  assert.equal(w.durationMinutes, 120)
  const built = Model.buildQuickAddRequest(
    Object.assign(Model.fallbackDraft("workshop", nlDay, "event"),
      { startTime: "17:00", allDay: false, durationMinutes: 120 }), nlNow)
  assert.equal(built.ok, true)
  assert.equal(built.request.endMs - built.request.startMs, 120 * 60 * 1000)
})

test("'at'/'bei' name a location and leave the title", () => {
  const d = parse("Essen um 18-21 at Garbe Biegarten")
  assert.equal(d.title, "Essen")
  assert.equal(d.location, "Garbe Biegarten")
  assert.equal(d.startTime, "18:00")
  assert.equal(d.endTime, "21:00")

  const de = parse("Essen bei anna")
  assert.equal(de.title, "Essen")
  assert.equal(de.location, "anna")

  // A date may still trail the place.
  const trailing = parse("Kaffee beim Italiener morgen 15 Uhr")
  assert.equal(trailing.title, "Kaffee")
  assert.equal(trailing.location, "Italiener")
  assert.equal(trailing.dateKey, "2026-08-24")
  assert.equal(trailing.startTime, "15:00")

  // The place wins over the capitalized-tail guess.
  const both = parse("Meeting Berlin at Anna")
  assert.equal(both.title, "Meeting Berlin")
  assert.equal(both.location, "Anna")

  // "at" before a time is still a time preposition.
  const timed = parse("standup at 9")
  assert.equal(timed.title, "standup")
  assert.equal(timed.location, null)
  assert.equal(timed.startTime, "09:00")

  // Nothing after it — the word stays in the title.
  const dangling = parse("dinner at")
  assert.equal(dangling.title, "dinner at")
  assert.equal(dangling.location, null)
})

// ---- Address suggestions -------------------------------------------------
//
// A Photon reply, trimmed to the fields we read. Coordinates come along in
// the real payload and are deliberately ignored: the field takes an address.

const photonReply = JSON.stringify({
  query: "garbe bierg",
  features: [
    {
      properties: {
        name: "Biergarten Wirtshaus Garbe", street: "Garbenstraße",
        postcode: "70599", city: "Stuttgart", state: "Baden-Württemberg",
        country: "Deutschland", countrycode: "DE"
      },
      geometry: { type: "Point", coordinates: [9.2038072, 48.7108348] }
    },
    {
      properties: {
        street: "Alsterufer", housenumber: "3", postcode: "20354",
        city: "Hamburg", country: "Deutschland"
      }
    }
  ]
})

test("a half-typed place becomes whole postal addresses", () => {
  const reply = Model.parseAddressSuggestions(photonReply)
  assert.equal(reply.query, "garbe bierg")
  assert.deepEqual(reply.items.map((i) => i.value), [
    "Biergarten Wirtshaus Garbe, Garbenstraße, 70599 Stuttgart",
    "Alsterufer 3, 20354 Hamburg"
  ])

  // The row reads as name over address; the country only joins the detail.
  assert.equal(reply.items[0].primary, "Biergarten Wirtshaus Garbe")
  assert.equal(reply.items[0].secondary, "Garbenstraße, 70599 Stuttgart, Deutschland")
  assert.equal(reply.items[1].primary, "Alsterufer 3")

  // Nothing coordinate-shaped reaches the field.
  for (const item of reply.items) assert.ok(!/\d+\.\d{4}/.test(item.value))
})

test("address rows drop the empty, the double and the duplicate", () => {
  // A place named after its street would say the street twice.
  assert.deepEqual(
    Model.addressLines({ name: "Garbenstraße", street: "Garbenstraße", postcode: "70599", city: "Stuttgart" }),
    ["Garbenstraße", "70599 Stuttgart"])

  // A district stands in for a missing city; a country alone still places it.
  assert.deepEqual(Model.addressLines({ street: "Hauptstraße", district: "Plieningen" }),
    ["Hauptstraße", "Plieningen"])
  assert.deepEqual(Model.addressLines({ name: "Zugspitze", country: "Deutschland" }),
    ["Zugspitze", "Deutschland"])
  assert.deepEqual(Model.addressLines({}), [])

  const twice = JSON.stringify({ query: "x", features: [
    { properties: { name: "Café Central", city: "Wien" } },
    { properties: { name: "Café Central", city: "Wien" } },
    { properties: {} }
  ]})
  assert.equal(Model.parseAddressSuggestions(twice).items.length, 1)
  assert.equal(Model.parseAddressSuggestions(photonReply, 1).items.length, 1)
})

test("home comes first when the world answers", () => {
  // What Photon really returns for "Garbe": Ohio before Saxony-Anhalt.
  const mixed = JSON.stringify({
    query: "garbe",
    country: "DE",
    features: [
      { properties: { name: "Garber", city: "Clayton", countrycode: "US", country: "United States" } },
      { properties: { name: "Garbe", city: "Bergara", postcode: "20579", countrycode: "ES" } },
      { properties: { name: "Garbe", city: "Ackendorf", countrycode: "DE" } },
      { properties: { name: "Garbek", city: "Wensin", postcode: "23827", countrycode: "DE" } }
    ]
  })

  const reply = Model.parseAddressSuggestions(mixed)
  assert.deepEqual(reply.items.map((i) => i.countryCode), ["DE", "DE", "US", "ES"])
  // Neither group is reshuffled inside itself — Photon's own ranking stands.
  assert.deepEqual(reply.items.map((i) => i.primary), ["Garbe", "Garbek", "Garber", "Garbe"])

  // The cut to list length happens after the sort, so a home row three
  // pages down still shows.
  assert.deepEqual(Model.parseAddressSuggestions(mixed, 2).items.map((i) => i.countryCode), ["DE", "DE"])

  // The script names the home country; asking for another one, or none,
  // is a different order.
  assert.equal(Model.parseAddressSuggestions(mixed, 6, "ES").items[0].countryCode, "ES")
  assert.equal(Model.parseAddressSuggestions(mixed, 6, "").items[0].countryCode, "US")
})

test("suggestions wait for a word, and junk never throws", () => {
  assert.equal(Model.shouldSuggestAddress("Ga"), false)
  assert.equal(Model.shouldSuggestAddress("  "), false)
  assert.equal(Model.shouldSuggestAddress("Gar"), true)

  for (const junk of ["", "not json", "[]", "{}", null]) {
    const reply = Model.parseAddressSuggestions(junk)
    assert.equal(reply.query, "")
    assert.deepEqual(reply.items, [])
  }
})

test("bare calendar names match after 'in' but locations do not", () => {
  const d = parse("lunch with Ana in work", ["Work", "Private"])
  assert.equal(d.calendarName, "Work")
  assert.equal(d.location, null)

  const cafe = parse("Lunch Friday 12:30 in Café Central", ["Work", "Private"])
  assert.equal(cafe.calendarName, null)
})

test("parseEventPhrase never dead-ends on unparseable text", () => {
  const d = parse("???")
  assert.equal(d.title, "???")
  assert.equal(d.dateKey, nlDay)
  assert.equal(d.allDay, true)

  assert.equal(parse(""), null)
  assert.equal(parse("   "), null)
})

test("fallbackDraft keeps raw text as the title", () => {
  const d = Model.fallbackDraft("some odd thing", nlDay, "task")
  assert.equal(d.kind, "task")
  assert.equal(d.title, "some odd thing")
  assert.equal(d.dateKey, nlDay)
})

// ---- buildQuickAddRequest -------------------------------------------------

const baseDraft = () => Object.assign(Model.fallbackDraft("Lunch", "2026-08-23", "event"), {
  startTime: "12:30",
  allDay: false
})

test("buildQuickAddRequest builds timed, all-day and multi-day events", () => {
  const timed = Model.buildQuickAddRequest(baseDraft(), nlNow)
  assert.equal(timed.ok, true)
  assert.equal(timed.request.startMs, Date.parse("2026-08-23T12:30:00+02:00"))
  assert.equal(timed.request.endMs, Date.parse("2026-08-23T13:30:00+02:00"))
  assert.equal(timed.request.allDay, false)

  const midnightWrap = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { startTime: "21:00", endTime: "02:00", endNextDay: true }), nlNow)
  assert.equal(midnightWrap.request.endMs, Date.parse("2026-08-24T02:00:00+02:00"))

  const allDay = Model.buildQuickAddRequest(
    Object.assign(Model.fallbackDraft("Off", "2026-08-23", "event"), {}), nlNow)
  assert.equal(allDay.request.allDay, true)
  assert.equal(allDay.request.endMs, null)
  assert.equal(allDay.request.startMs, Date.parse("2026-08-23T00:00:00+02:00"))

  const multi = Model.buildQuickAddRequest(
    Object.assign(Model.fallbackDraft("Retreat", "2026-08-23", "event"), { endDateKey: "2026-08-26" }), nlNow)
  assert.equal(multi.request.endMs, Date.parse("2026-08-27T00:00:00+02:00")) // exclusive

  const summary = Model.formatEntrySummary(midnightWrap.request)
  assert.match(summary, /Lunch/)
  assert.match(summary, /\+1d 02:00/)

  const noted = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { description: "  bring cake  " }), nlNow)
  assert.equal(noted.request.description, "bring cake")
})

test("buildQuickAddRequest builds tasks with iCalendar priorities", () => {
  const mk = priority => Model.buildQuickAddRequest(
    Object.assign(Model.fallbackDraft("Buy groceries", "2026-08-24", "task"),
      { startTime: "17:00", priority }), nlNow)

  assert.deepEqual(
    [mk("high"), mk("medium"), mk("low"), mk(null)].map(r => r.request.priority),
    [1, 5, 9, null])
  const high = mk("high").request
  assert.equal(high.dueMs, Date.parse("2026-08-24T17:00:00+02:00"))
  assert.equal(Model.formatEntrySummary(high).indexOf("!!!") > 0, true)

  const undated = Model.buildQuickAddRequest(
    Model.fallbackDraft("Someday", "2026-08-24", "task"), nlNow)
  assert.equal(undated.request.dueMs, null)
  assert.match(Model.formatEntrySummary(undated.request), /no due date/)

  const noted = Model.buildQuickAddRequest(
    Object.assign(Model.fallbackDraft("Someday", "2026-08-24", "task"),
      { description: "check the shed" }), nlNow)
  assert.equal(noted.request.description, "check the shed")
})

test("buildQuickAddRequest rejects empty titles, bad dates, inverted ends", () => {
  assert.equal(Model.buildQuickAddRequest(Object.assign(baseDraft(), { title: "" }), nlNow).ok, false)
  assert.equal(Model.buildQuickAddRequest(Object.assign(baseDraft(), { dateKey: "" }), nlNow).ok, false)
  assert.equal(Model.buildQuickAddRequest(null, nlNow).ok, false)

  const far = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { dateKey: "2040-01-01" }), nlNow)
  assert.equal(far.ok, false)

  const inverted = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { endTime: "11:00" }), nlNow)
  assert.equal(inverted.ok, false)
  assert.match(inverted.error, /end/)

  const badTime = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { startTime: "25:99" }), nlNow)
  assert.equal(badTime.ok, false)
})

// ---- Entry-field rendering -------------------------------------------------

function segmentRoles(text, calendars) {
  const d = parse(text, calendars)
  return d.segments.map(s => [text.slice(s.start, s.end).trim(), s.role])
}

test("parseEventPhrase labels every part of the phrase it consumed", () => {
  assert.deepEqual(
    segmentRoles("Meeting with Alex tomorrow 10am -120 /Work -a40m"),
    [
      ["Meeting with Alex", "title"],
      ["tomorrow", "date"],
      ["10am", "time"],
      ["-120", "duration"],
      ["/Work", "calendar"],
      ["-a40m", "alert"]
    ])

  assert.deepEqual(
    segmentRoles("mittag mit Ana morgen 12 bis 13 Uhr /Arbeit"),
    [
      // "mittag" is why the start is 12:00, so it paints as a time even
      // though the word also survives into the title.
      ["mittag", "time"],
      ["mit Ana", "title"],
      ["morgen", "date"],
      ["12 bis 13 Uhr", "time"],
      ["/Arbeit", "calendar"]
    ])

  assert.deepEqual(
    segmentRoles("workshop next monday for 90m !!"),
    [
      ["workshop", "title"],
      ["next monday", "date"],
      ["for 90m", "duration"],
      ["!!", "priority"]
    ])

  assert.deepEqual(
    segmentRoles("lunch 12:30 Café Central"),
    [["lunch", "title"], ["12:30", "time"], ["Café Central", "location"]])

  assert.deepEqual(
    segmentRoles("lunch 12:30 at Café Central"),
    [["lunch", "title"], ["12:30", "time"], ["at Café Central", "location"]])

  assert.deepEqual(
    segmentRoles("standup in Work 9:00", ["Work"]),
    [["standup", "title"], ["in Work", "calendar"], ["9:00", "time"]])

  assert.deepEqual(
    segmentRoles("retreat till 25.8. -r1w"),
    [["retreat", "title"], ["till 25.8.", "date"], ["-r1w", "repeat"]])
})

test("segments cover the phrase and stay inside it", () => {
  for (const phrase of [
    "party today 21:00 till 2:00",
    "flight 15.3. 7:00-9:30 /Work",
    "einkaufen bis freitag !!!",
    "call mom sunday 6pm -a15m"
  ]) {
    const segments = parse(phrase).segments
    let cursor = 0
    for (const s of segments) {
      assert.ok(s.start >= cursor, phrase + ": segments overlap")
      assert.ok(s.end > s.start && s.end <= phrase.length, phrase + ": segment out of range")
      cursor = s.end
    }
    // Only whitespace may sit outside the segments.
    let covered = ""
    let at = 0
    for (const s of segments) { covered += phrase.slice(at, s.start); at = s.end }
    covered += phrase.slice(at)
    assert.equal(covered.trim(), "", phrase + ": non-space text left uncoloured")
  }
})

test("fallbackDraft still colours the whole line as a title", () => {
  const d = Model.fallbackDraft("just some words", "2026-08-23", "event")
  assert.deepEqual(d.segments, [{ start: 0, end: "just some words".length, role: "title" }])
  assert.deepEqual(Model.fallbackDraft("", "2026-08-23", "event").segments, [])
})

test("phraseHtml paints roles and escapes the rest", () => {
  const colors = { title: "#ffffff", time: "#cc88ff", calendar: "#ff5555" }
  const text = "tea 5pm /Work"
  const html = Model.phraseHtml(text, parse(text).segments, colors)
  assert.equal(html,
    '<font color="#ffffff">tea</font> ' +
    '<font color="#cc88ff">5pm</font> ' +
    '<font color="#ff5555">/Work</font>')

  assert.equal(Model.phraseHtml("a < b & c", [], {}), "a &lt; b &amp; c")
  // Runs of spaces keep their width — all but the last space go hard.
  assert.equal(Model.escapePhrase("a   b"), "a&#160;&#160; b")
  assert.equal(Model.phraseHtml("", null, {}), "")
})

test("relativeDayKind names today and its neighbours", () => {
  assert.equal(Model.relativeDayKind("2026-08-23", "2026-08-23"), "today")
  assert.equal(Model.relativeDayKind("2026-08-24", "2026-08-23"), "tomorrow")
  assert.equal(Model.relativeDayKind("2026-08-22", "2026-08-23"), "yesterday")
  assert.equal(Model.relativeDayKind("2026-08-27", "2026-08-23"), "weekday")
  assert.equal(Model.relativeDayKind("2026-09-30", "2026-08-23"), "date")
  assert.equal(Model.relativeDayKind("", "2026-08-23"), "")
})

test("chip inputs take the same vocabulary as the phrase", () => {
  assert.equal(Model.parseDateInput("tomorrow", nlDay, nlNow), "2026-08-24")
  assert.equal(Model.parseDateInput("next monday", nlDay, nlNow), "2026-08-31")
  assert.equal(Model.parseDateInput("15.3.", nlDay, nlNow), "2027-03-15")
  assert.equal(Model.parseDateInput("2026-09-01", nlDay, nlNow), "2026-09-01")
  assert.equal(Model.parseDateInput("nonsense", nlDay, nlNow), null)
  assert.equal(Model.parseDateInput("", nlDay, nlNow), null)

  assert.equal(Model.parseTimeInput("9"), "09:00")
  assert.equal(Model.parseTimeInput("9:30"), "09:30")
  assert.equal(Model.parseTimeInput("9.30"), "09:30")
  assert.equal(Model.parseTimeInput("6pm"), "18:00")
  assert.equal(Model.parseTimeInput("12am"), "00:00")
  assert.equal(Model.parseTimeInput("14 Uhr"), "14:00")
  assert.equal(Model.parseTimeInput("25:00"), null)
  assert.equal(Model.parseTimeInput("half past"), null)
})

test("calendarColors learns each calendar's colour from synced events", () => {
  const colors = Model.calendarColors([
    { calendarName: "Work", color: "#6600cc" },
    { calendarName: "Work", color: "#123456" },
    { calendarName: "Kalender", color: "not-a-color" },
    { calendarName: "", color: "#ffffff" }
  ])
  assert.deepEqual(colors, { Work: "#6600cc" })
})

test("dropdown rows keep whatever the phrase set", () => {
  assert.deepEqual(Model.alertOptions(0).map(o => o.value), ["0", "5", "15", "30", "60", "1440"])
  assert.deepEqual(Model.alertOptions(40).map(o => o.value), ["0", "5", "15", "30", "40", "60", "1440"])
  assert.equal(Model.alertLabelFor(40), "40 minutes before")
  assert.equal(Model.alertLabelFor(60), "1 hour before")
  assert.equal(Model.alertLabelFor(2880), "2 days before")
  assert.equal(Model.alertLabelFor(0), "No alert")

  assert.deepEqual(Model.repeatOptions("").map(o => o.label),
    ["Does not repeat", "Daily", "Weekly", "Monthly", "Yearly"])
  assert.deepEqual(Model.repeatOptions("weekly:2").map(o => o.value).slice(-1), ["weekly:2"])
  assert.equal(Model.repeatLabelFor("weekly:2"), "Every 2 weeks")
})

test("a pasted meeting link is its own part, never the title", () => {
  const d = parse("standup 9:00 https://zoom.us/j/9421 /Work")
  assert.equal(d.title, "standup")
  assert.equal(d.link, "https://zoom.us/j/9421")
  assert.equal(d.calendarName, "Work")
  assert.deepEqual(
    d.segments.map(s => ["standup 9:00 https://zoom.us/j/9421 /Work".slice(s.start, s.end).trim(), s.role]),
    [
      ["standup", "title"],
      ["9:00", "time"],
      ["https://zoom.us/j/9421", "link"],
      ["/Work", "calendar"]
    ])

  // Trailing sentence punctuation is not part of the link.
  assert.equal(parse("call https://meet.google.com/abc-defg-hij.").link,
    "https://meet.google.com/abc-defg-hij")
  // Anything that is not a link stays a word.
  assert.equal(parse("read ftp://files.example.com").link, null)
})

test("safeLinkUrl keeps http and https and nothing else", () => {
  assert.equal(Model.safeLinkUrl("https://zoom.us/j/1"), "https://zoom.us/j/1")
  assert.equal(Model.safeLinkUrl("http://intra.local/room"), "http://intra.local/room")
  assert.equal(Model.safeLinkUrl("javascript:alert(1)"), "")
  assert.equal(Model.safeLinkUrl("https://a b.com"), "")
  assert.equal(Model.safeLinkUrl(""), "")
  assert.equal(Model.safeLinkUrl("https://x.com/" + "a".repeat(2000)), "")
})

test("linkProviderLabel names the service for the pill", () => {
  assert.equal(Model.linkProviderLabel("https://us02web.zoom.us/j/1"), "Zoom")
  assert.equal(Model.linkProviderLabel("https://meet.google.com/abc"), "Meet")
  assert.equal(Model.linkProviderLabel("https://teams.microsoft.com/l/x"), "Teams")
  assert.equal(Model.linkProviderLabel("https://example.com/room"), "Link")
  assert.equal(Model.linkProviderLabel(""), "Link")
})

test("buildQuickAddRequest carries the link on events and tasks", () => {
  const event = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { link: "https://zoom.us/j/9421" }), nlNow)
  assert.equal(event.request.link, "https://zoom.us/j/9421")

  const task = Model.buildQuickAddRequest(
    Object.assign(Model.fallbackDraft("Call", "2026-08-24", "task"),
      { link: "https://zoom.us/j/9421" }), nlNow)
  assert.equal(task.request.link, "https://zoom.us/j/9421")

  const junk = Model.buildQuickAddRequest(
    Object.assign(baseDraft(), { link: "javascript:alert(1)" }), nlNow)
  assert.equal(junk.request.link, null)
})

test("phraseHtml underlines a link so colour is not the only signal", () => {
  const text = "call https://zoom.us/j/94"
  const html = Model.phraseHtml(text, parse(text).segments, { title: "#ffffff", link: "#7aa8f7" })
  assert.equal(html,
    '<font color="#ffffff">call</font> ' +
    '<font color="#7aa8f7"><u>https://zoom.us/j/94</u></font>')
})

// ---- Merging a parse into what is already on screen -------------------------

const emptyForm = {
  date: nlDay, start: "", end: "", endDate: "",
  location: "", notes: "", link: "", alert: 0, repeat: "", priority: ""
}

// One keystroke: parse the phrase, merge it into what is on screen, and hand
// back the pane's new state — the same round the entry pane makes.
function type(text, form, applied, kind) {
  const parsed = Model.parseEventPhrase(text, nlDay, nlNow, ["Work", "Kalender"])
    || Model.fallbackDraft(text, nlDay, kind || "event")
  const merged = Model.mergeEntryDraft(parsed, form || emptyForm, applied || {}, kind || "event")
  return merged
}

test("an open-ended start gets an hour, and re-typing the hour moves it", () => {
  let round = type("lunch with otto at 1")
  assert.equal(round.values.start, "01:00")
  assert.equal(round.values.end, "02:00")

  // The 8 lands: 18:00 must bring 19:00 with it, not keep the 02:00 that the
  // pane derived a keystroke ago.
  round = type("lunch with otto at 18", round.values, round.applied)
  assert.equal(round.values.start, "18:00")
  assert.equal(round.values.end, "19:00")
})

test("a duration in the phrase sets the end, and a typed end survives", () => {
  let round = type("workshop 9:00 for 90m")
  assert.equal(round.values.end, "10:30")

  // Hand-edit the end, then type another word into the phrase.
  const edited = Object.assign({}, round.values, { end: "12:00" })
  round = type("workshop 9:00 for 90m with Ana", edited, round.applied)
  assert.equal(round.values.end, "12:00")
})

test("what the phrase gave, the phrase can take back", () => {
  let round = type("meet 9:00 Cafe Central")
  assert.equal(round.values.location, "Cafe Central")

  round = type("meet 9:00", round.values, round.applied)
  assert.equal(round.values.location, "")

  // Clearing the time clears the span with it.
  round = type("meet", round.values, round.applied)
  assert.equal(round.values.start, "")
  assert.equal(round.values.end, "")
})

test("parts typed by hand are not the phrase's to clear", () => {
  let round = type("review 14:00")
  const noted = Object.assign({}, round.values, { notes: "bring the deck", link: "https://zoom.us/j/1" })
  round = type("review 14:00 with Ana", noted, round.applied)
  assert.equal(round.values.notes, "bring the deck")
  assert.equal(round.values.link, "https://zoom.us/j/1")
})

test("merging keeps the task rules and the midnight wrap", () => {
  const task = type("einkaufen bis freitag", emptyForm, {}, "task")
  assert.equal(task.values.date, "2026-08-28")
  assert.equal(task.values.endDate, "")

  const party = type("party today 21:00 till 2:00")
  assert.equal(party.values.end, "02:00")
  assert.equal(party.values.endNextDay, true)
})

test("defaultEndTime is an hour on, wrapping past midnight", () => {
  assert.equal(Model.defaultEndTime("09:00", null), "10:00")
  assert.equal(Model.defaultEndTime("09:00", 90), "10:30")
  assert.equal(Model.defaultEndTime("23:30", null), "00:30")
  assert.equal(Model.defaultEndTime("", null), "")
})

// ---- Tasks: placement, the week bucket, and the display-only rollover.

function task(overrides) {
  return Object.assign({
    id: "t1",
    calendarId: "aufgaben",
    calendarName: "Aufgaben",
    color: "#FF9500",
    dueKey: "",
    due: "",
    time: "",
    allDay: true,
    title: "Steuer",
    priority: "",
    done: false,
    completedAt: ""
  }, overrides)
}

const TODAY = "2026-08-28"

test("taskPlacement sorts a task onto a day, into the week, or into done", () => {
  assert.equal(Model.taskPlacement(task({ dueKey: "2026-08-30" }), TODAY), "day")
  assert.equal(Model.taskPlacement(task({ dueKey: TODAY }), TODAY), "day")
  assert.equal(Model.taskPlacement(task({ dueKey: "" }), TODAY), "week")
  assert.equal(Model.taskPlacement(task({ dueKey: "2026-08-20" }), TODAY), "week")
  assert.equal(Model.taskPlacement(task({ dueKey: "2026-08-20", done: true }), TODAY), "done")
})

test("an overdue task rolls forward for display without being rewritten", () => {
  const overdue = task({ id: "late", dueKey: "2026-08-10" })
  const bucket = Model.weekTasks([overdue], TODAY)
  assert.deepEqual(bucket.map(t => t.id), ["late"])
  // The rollover is a read: the task itself still carries its original due
  // date, so nothing has to be written back to Thunderbird.
  assert.equal(overdue.dueKey, "2026-08-10")
  assert.equal(Model.taskOverdueDays(overdue, TODAY), 18)
  // And it stays out of its old day, rather than showing in both places.
  assert.deepEqual(Model.indexTasksByDate([overdue], TODAY), {})
})

test("the week bucket leads with the most overdue, then by priority", () => {
  const tasks = [
    task({ id: "fresh", dueKey: "" }),
    task({ id: "urgent", dueKey: "", priority: "high" }),
    task({ id: "old", dueKey: "2026-08-01" }),
    task({ id: "older", dueKey: "2026-07-20" }),
    task({ id: "future", dueKey: "2026-09-09" }),
    task({ id: "finished", dueKey: "2026-08-02", done: true })
  ]
  assert.deepEqual(
    Model.weekTasks(tasks, TODAY).map(t => t.id),
    ["older", "old", "urgent", "fresh"]
  )
})

test("dated todos land on their day, by time; ticked ones are gone", () => {
  const tasks = [
    task({ id: "done", dueKey: "2026-08-30", done: true }),
    task({ id: "noon", dueKey: "2026-08-30", time: "12:00" }),
    task({ id: "dawn", dueKey: "2026-08-30", time: "06:00" }),
    task({ id: "elsewhere", dueKey: "2026-08-31" })
  ]
  const index = Model.indexTasksByDate(tasks, TODAY)
  assert.deepEqual(
    Model.tasksForDateKey(index, "2026-08-30").map(t => t.id),
    ["dawn", "noon"]
  )
})

test("ticking a todo takes it out of the view entirely", () => {
  // The optimistic flip in the panel is exactly this: set done, and the todo
  // is placed nowhere — not in its day, not in the bucket.
  const dated = task({ id: "d", dueKey: "2026-08-30" })
  const loose = task({ id: "l", dueKey: "" })
  assert.equal(Object.keys(Model.indexTasksByDate([dated], TODAY)).length, 1)
  assert.equal(Model.weekTasks([loose], TODAY).length, 1)

  dated.done = true
  loose.done = true
  assert.deepEqual(Model.indexTasksByDate([dated], TODAY), {})
  assert.deepEqual(Model.weekTasks([loose], TODAY), [])
  assert.deepEqual(Model.weekTasksFor([loose], Model.weekStartKeyOf(TODAY, 1), TODAY, 1), [])
})

test("a day cell rings only for work still owed", () => {
  const index = Model.indexTasksByDate([
    task({ id: "a", dueKey: "2026-08-30", done: true, color: "#FF9500" })
  ], TODAY)
  assert.equal(index["2026-08-30"], undefined)
  assert.equal(Model.hasOpenTask(index["2026-08-30"]), false)
  assert.deepEqual(Model.taskColors(index["2026-08-30"]), [])

  const open = Model.indexTasksByDate([
    task({ id: "a", dueKey: "2026-08-30", color: "#FF9500" }),
    task({ id: "b", dueKey: "2026-08-30", color: "#FF9500" }),
    task({ id: "c", dueKey: "2026-08-30", color: "#6600cc" })
  ], TODAY)
  assert.equal(Model.hasOpenTask(open["2026-08-30"]), true)
  assert.deepEqual(Model.taskColors(open["2026-08-30"]), ["#FF9500", "#6600cc"])
})

test("monthGrid carries the task ring alongside the event dots", () => {
  const events = Model.indexEventsByDate([
    { dateKey: "2026-08-30", color: "#6600cc", title: "Standup" }
  ])
  const tasks = Model.indexTasksByDate([task({ dueKey: "2026-08-30" })], TODAY)
  const weeks = Model.monthGrid(2026, 7, 1, TODAY, events, tasks)
  const cell = weeks.flatMap(w => w.days).find(d => d.key === "2026-08-30")
  assert.equal(cell.hasEvent, true)
  assert.deepEqual(cell.colors, ["#6600cc"])
  assert.equal(cell.hasTask, true)
  assert.deepEqual(cell.taskColors, ["#FF9500"])
})

test("monthGrid without a task index is the old grid, unchanged", () => {
  const weeks = Model.monthGrid(2026, 7, 1, TODAY, null)
  const cell = weeks.flatMap(w => w.days).find(d => d.key === "2026-08-30")
  assert.equal(cell.hasTask, false)
  assert.deepEqual(cell.taskColors, [])
})

test("buildCompleteRequest names the list as well as the task", () => {
  const req = Model.buildCompleteRequest(task({ id: "uid-1" }), Date.parse("2026-08-28T09:00:00Z"))
  assert.equal(req.kind, "complete")
  assert.equal(req.uid, "uid-1")
  assert.equal(req.calendarName, "Aufgaben")
  assert.equal(req.done, true)
  // Ticking a finished task again is the way back out of "done".
  assert.equal(Model.buildCompleteRequest(task({ done: true }), Date.now()).done, false)
  assert.equal(Model.buildCompleteRequest(null, Date.now()), null)
})

test("tasksFromDocument tolerates a sync that predates tasks", () => {
  assert.deepEqual(Model.tasksFromDocument({ version: 1, events: [] }), [])
  assert.deepEqual(Model.tasksFromDocument(null), [])
  assert.equal(Model.tasksFromDocument({ tasks: [task({})] }).length, 1)
})

test("weekStartKeyOf names a week by its first day, per week-start setting", () => {
  // Friday 28 August 2026.
  assert.equal(Model.weekStartKeyOf("2026-08-28", 1), "2026-08-24")
  assert.equal(Model.weekStartKeyOf("2026-08-28", 0), "2026-08-23")
  // Every day of one week answers with the same key.
  for (const day of ["2026-08-24", "2026-08-27", "2026-08-30"]) {
    assert.equal(Model.weekStartKeyOf(day, 1), "2026-08-24")
  }
  assert.equal(Model.weekStartKeyOf("2026-08-31", 1), "2026-08-31")
  assert.equal(Model.weekStartKeyOf("", 1), "")
})

test("the bucket belongs to this week and to no other week", () => {
  const tasks = [task({ id: "loose", dueKey: "" }), task({ id: "late", dueKey: "2026-08-01" })]
  const thisWeek = Model.weekStartKeyOf(TODAY, 1)

  assert.deepEqual(
    Model.weekTasksFor(tasks, thisWeek, TODAY, 1).map(t => t.id),
    ["late", "loose"]
  )
  // Next week is empty: nothing has been carried into it yet. This is the
  // whole point of scoping — an unfinished task showed up under every week
  // of the month before.
  assert.deepEqual(Model.weekTasksFor(tasks, "2026-08-31", TODAY, 1), [])
  assert.deepEqual(Model.weekTasksFor(tasks, "2026-08-17", TODAY, 1), [])
  assert.deepEqual(Model.weekTasksFor(tasks, "", TODAY, 1), [])
})

test("the bucket follows the week-start setting, not a fixed Monday", () => {
  const tasks = [task({ id: "loose" })]
  // Sunday-start weeks put 28 August in the week beginning the 23rd.
  assert.equal(Model.weekTasksFor(tasks, "2026-08-23", TODAY, 0).length, 1)
  assert.equal(Model.weekTasksFor(tasks, "2026-08-24", TODAY, 0).length, 0)
})

test("come next week, the same unfinished task is in that week's bucket", () => {
  // Nothing about the task changes — only what day it is.
  const tasks = [task({ id: "loose", dueKey: "" })]
  const nextFriday = "2026-09-04"
  assert.equal(Model.weekTasksFor(tasks, "2026-08-24", nextFriday, 1).length, 0)
  assert.equal(Model.weekTasksFor(tasks, "2026-08-31", nextFriday, 1).length, 1)
  assert.equal(tasks[0].dueKey, "")
})

test("buildQuickTodoRequest files a bare title and nothing else", () => {
  const built = Model.buildQuickTodoRequest("  read book  ", "Aufgaben")
  assert.equal(built.ok, true)
  assert.equal(built.request.kind, "task")
  assert.equal(built.request.title, "read book")
  assert.equal(built.request.calendarName, "Aufgaben")
  // No due date is the point: it lands in the bucket it was typed into.
  assert.equal(built.request.dueMs, null)
  assert.equal(built.request.priority, null)
  assert.equal(built.request.recurrence, null)

  assert.equal(Model.buildQuickTodoRequest("   ", "Aufgaben").ok, false)
  assert.equal(Model.buildQuickTodoRequest("x".repeat(301), "Aufgaben").ok, false)
  // No list in the roster yet: the add-on picks one rather than being told a
  // name that does not exist.
  assert.equal(Model.buildQuickTodoRequest("read book", "").request.calendarName, null)
})

test("a bare todo goes to the first list that can hold one", () => {
  const roster = [
    { name: "Kalender", events: true, tasks: false },
    { name: "Aufgaben", events: false, tasks: true },
    { name: "Work", events: true, tasks: true }
  ]
  assert.equal(Model.firstTaskCalendarName(roster), "Aufgaben")
  assert.equal(Model.firstTaskCalendarName([{ name: "Kalender", events: true, tasks: false }]), "")
  assert.equal(Model.firstTaskCalendarName([]), "")
})

test("a typed todo stands in for itself until the sync catches up", () => {
  const existing = [task({ id: "gym", title: "go to gym", calendarName: "Aufgaben" })]
  const p = Model.pendingTodo("read book", "Aufgaben", 1000, existing)
  assert.equal(p.pending, true)
  assert.equal(p.done, false)
  assert.equal(p.dueKey, "")
  // It takes the list's colour off a task already in it, so the chip is not
  // a different colour from its neighbours for three seconds.
  assert.equal(p.color, "#FF9500")
  // And it is a bucket task, so it lands in the section it was typed into.
  assert.equal(Model.taskPlacement(p, TODAY), "week")

  assert.equal(Model.prunePendingTodos([p], existing, 2000).length, 1)
  const synced = existing.concat([task({ id: "real", title: "read book", calendarName: "Aufgaben" })])
  assert.equal(Model.prunePendingTodos([p], synced, 2000).length, 0)
  assert.equal(Model.pendingTodo("  ", "Aufgaben", 1000, existing), null)
})

test("a second todo of the same title still shows instantly", () => {
  // The count at creation, not a yes/no: a repeat chore is the normal case.
  const existing = [task({ id: "w1", title: "Wäsche", calendarName: "Aufgaben" })]
  const p = Model.pendingTodo("Wäsche", "Aufgaben", 1000, existing)
  assert.equal(p.seen, 1)
  assert.equal(Model.prunePendingTodos([p], existing, 2000).length, 1)
  const twoNow = existing.concat([task({ id: "w2", title: "Wäsche", calendarName: "Aufgaben" })])
  assert.equal(Model.prunePendingTodos([p], twoNow, 2000).length, 0)
})

test("a placeholder gives up rather than lying about being filed", () => {
  const p = Model.pendingTodo("read book", "Aufgaben", 1000, [])
  assert.equal(Model.prunePendingTodos([p], [], 1000 + 89000).length, 1)
  assert.equal(Model.prunePendingTodos([p], [], 1000 + 91000).length, 0)
})

test("countOpenTodos ignores finished ones and other lists", () => {
  const tasks = [
    task({ title: "Wäsche", calendarName: "Aufgaben" }),
    task({ title: "Wäsche", calendarName: "Aufgaben", done: true }),
    task({ title: "Wäsche", calendarName: "Work" })
  ]
  assert.equal(Model.countOpenTodos(tasks, "Wäsche", "Aufgaben"), 1)
  assert.equal(Model.countOpenTodos(tasks, "Wäsche", ""), 2)
  assert.equal(Model.countOpenTodos(tasks, "nothing", "Aufgaben"), 0)
})
