// Model.js — Pure data transformations, string parsing, and formatting
// for the mailbox email and screening plugin.

function cleanSenderName(fromStr) {
  if (!fromStr || typeof fromStr !== "string") return ""
  var text = fromStr.trim()
  // "Alice Smith" <alice@example.com> -> Alice Smith
  var angleIdx = text.indexOf("<")
  if (angleIdx > 0) {
    var name = text.substring(0, angleIdx).trim()
    name = name.replace(/^["']|["']$/g, "").trim()
    if (name.length > 0) return name
  }
  if (angleIdx === 0 && text.indexOf(">") > 0) {
    text = text.substring(1, text.indexOf(">")).trim()
  }
  // If only email, return the local part or whole
  var atIdx = text.indexOf("@")
  if (atIdx > 0) {
    return text.substring(0, atIdx)
  }
  return text.replace(/^["']|["']$/g, "").trim()
}

function cleanAddress(fromStr) {
  if (!fromStr || typeof fromStr !== "string") return ""
  var text = fromStr.trim()
  var match = text.match(/<([^>]+)>/)
  if (match && match[1]) {
    return match[1].trim()
  }
  return text.replace(/^["']|["']$/g, "").trim()
}

function extractInitials(fromStr) {
  var name = cleanSenderName(fromStr)
  if (!name) {
    var addr = cleanAddress(fromStr)
    if (!addr) return "?"
    name = addr.split("@")[0]
  }

  // Remove special characters, split into words
  var words = name.replace(/[^a-zA-Z0-9\s._-]/g, " ").trim().split(/[\s._-]+/).filter(Boolean)
  if (words.length === 0) return "?"
  if (words.length === 1) {
    var single = words[0]
    if (single.length >= 2) {
      return (single[0] + single[1]).toUpperCase()
    }
    return single[0].toUpperCase()
  }
  // First letter of first word + first letter of second word
  return (words[0][0] + words[1][0]).toUpperCase()
}

function avatarColorIndex(identifier, paletteLength) {
  if (!paletteLength || paletteLength <= 0) return 0
  var str = String(identifier || "").toLowerCase().trim()
  if (!str) return 0
  var hash = 0
  for (var i = 0; i < str.length; i++) {
    hash = ((hash << 5) - hash) + str.charCodeAt(i)
    hash |= 0
  }
  return Math.abs(hash) % paletteLength
}

function parseTimestamp(dateInput) {
  if (!dateInput) return null
  if (dateInput instanceof Date) return isNaN(dateInput.getTime()) ? null : dateInput.getTime()
  if (typeof dateInput === "number") return isFinite(dateInput) ? dateInput : null
  var text = String(dateInput).trim()
  if (!text) return null

  // Check ISO / RFC3339
  var parsed = Date.parse(text)
  if (!isNaN(parsed)) return parsed

  // "2026-08-30 14:30" format
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}(:\d{2})?$/.test(text)) {
    var iso = text.replace(" ", "T")
    parsed = Date.parse(iso)
    if (!isNaN(parsed)) return parsed
  }

  return null
}

function formatRelativeTime(dateInput, nowMs) {
  var timeMs = parseTimestamp(dateInput)
  if (timeMs === null) return String(dateInput || "")
  var now = typeof nowMs === "number" && isFinite(nowMs) ? nowMs : Date.now()
  var diffMs = now - timeMs
  if (diffMs < 0 && diffMs > -60000) diffMs = 0 // slight clock skew tolerance

  var diffSec = Math.floor(diffMs / 1000)
  var diffMin = Math.floor(diffSec / 60)
  var diffHours = Math.floor(diffMin / 60)
  var diffDays = Math.floor(diffHours / 24)

  if (diffSec < 60) {
    return "just now"
  }
  if (diffMin < 60) {
    return diffMin + "m ago"
  }

  var d = new Date(timeMs)
  var nowDate = new Date(now)

  var isSameDay = d.getFullYear() === nowDate.getFullYear()
    && d.getMonth() === nowDate.getMonth()
    && d.getDate() === nowDate.getDate()

  var pad2 = function(n) { return n < 10 ? "0" + n : String(n) }
  var timePart = pad2(d.getHours()) + ":" + pad2(d.getMinutes())

  if (isSameDay) {
    return timePart
  }

  var yesterday = new Date(now)
  yesterday.setDate(yesterday.getDate() - 1)
  var isYesterday = d.getFullYear() === yesterday.getFullYear()
    && d.getMonth() === yesterday.getMonth()
    && d.getDate() === yesterday.getDate()

  if (isYesterday) {
    return "Yesterday"
  }

  var monthNames = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
  if (d.getFullYear() === nowDate.getFullYear()) {
    return monthNames[d.getMonth()] + " " + d.getDate()
  }

  return d.getDate() + " " + monthNames[d.getMonth()] + " " + d.getFullYear()
}

function filterMessages(messages, accountFilter, stateFilter) {
  var list = Array.isArray(messages) ? messages : []
  var acc = String(accountFilter || "").trim()
  var state = String(stateFilter || "unread").trim()

  var out = []
  for (var i = 0; i < list.length; i++) {
    var m = list[i]
    if (!m) continue
    if (acc !== "" && String(m.account || m.accountId || "") !== acc) {
      continue
    }
    if (state === "unread") {
      if (m.seen !== true) out.push(m)
    } else if (state === "previous") {
      if (m.seen === true) out.push(m)
    } else {
      out.push(m)
    }
  }
  return out
}

function screenerCards(screenerData, paletteLength) {
  var list = Array.isArray(screenerData) ? screenerData : []
  var paletteLen = paletteLength || 8
  var out = []
  for (var i = 0; i < list.length; i++) {
    var item = list[i]
    if (!item) continue
    var addr = cleanAddress(item.address || "")
    var name = item.name || cleanSenderName(item.address || "") || addr
    out.push({
      address: addr,
      name: name,
      count: item.count || 1,
      unread: item.unread || 0,
      subject: item.subject || "(No Subject)",
      time: item.newest ? formatRelativeTime(item.newest) : "",
      rawTime: item.newest || "",
      id: item.id || "",
      initials: extractInitials(name || addr),
      colorIndex: avatarColorIndex(addr || name, paletteLen)
    })
  }
  return out
}

function accountFilterOptions(accounts) {
  var list = Array.isArray(accounts) ? accounts : []
  var out = [{ value: "", label: "All accounts" }]
  for (var i = 0; i < list.length; i++) {
    var a = list[i]
    if (!a) continue
    var name = typeof a === "string" ? a : (a.name || a.id || String(a))
    out.push({
      value: name,
      label: name
    })
  }
  return out
}

function requestToArgs(action) {
  if (!action || typeof action !== "object") return null
  var kind = String(action.kind || "")

  switch (kind) {
    case "seen":
      return {
        cmd: [action.seen === false ? "unseen" : "seen"],
        args: { positional: String(action.id || "") }
      }
    case "aside":
      return {
        cmd: ["aside"],
        args: { positional: String(action.id || "") }
      }
    case "aside_done":
      return {
        cmd: ["aside", "done"],
        args: { positional: String(action.id || "") }
      }
    case "route":
      return {
        cmd: ["route"],
        args: {
          positional: String(action.target || action.address || action.id || ""),
          to: String(action.to || "inbox")
        }
      }
    case "trash":
      return {
        cmd: ["trash"],
        args: { positional: String(action.id || "") }
      }
    case "spam":
      return {
        cmd: ["spam"],
        args: { positional: String(action.id || "") }
      }
  }
  return null
}

function shellQuote(value) {
  return "'" + String(value || "").replace(/'/g, "'\\''") + "'"
}

// Clicking a mail in the notification widget always opens it in the mailbox
// desktop client (gui-omarchy, binary `mailbox-omarchy`). There is no override:
// this is the only client we ship, so it is the only target.
function buildOpenCommand(messageId) {
  return "mailbox-omarchy --open " + shellQuote(String(messageId || "").trim())
}

if (typeof module !== "undefined" && module.exports) {
  module.exports = {
    cleanSenderName: cleanSenderName,
    cleanAddress: cleanAddress,
    extractInitials: extractInitials,
    avatarColorIndex: avatarColorIndex,
    parseTimestamp: parseTimestamp,
    formatRelativeTime: formatRelativeTime,
    filterMessages: filterMessages,
    screenerCards: screenerCards,
    accountFilterOptions: accountFilterOptions,
    requestToArgs: requestToArgs,
    shellQuote: shellQuote,
    buildOpenCommand: buildOpenCommand
  }
}
