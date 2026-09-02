.pragma library

// Shared, pure string helpers. These were copy-pasted across half a dozen QML
// files (Avatar/FeedView initials, ReadingView/MailRow subject stripping,
// ThreadMessage/ComposerView/RecipientPills address parsing,
// QuickLook/AttachmentChip/ComposerView file-url juggling, and the
// r.error-or-fallback dance in every daemon callback). One home, one behaviour.

// "Ada Lovelace" -> "AL", "cheddar" -> "CH", "" -> "?".
function initials(s) {
    if (!s) return "?"
    var parts = String(s).trim().split(/\s+/)
    if (parts.length === 1) return parts[0].substring(0, 2).toUpperCase()
    return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
}

// Drop a leading run of reply/forward prefixes (Re:, AW:, Fwd:, WG:, Antw:,
// Re[2]: …) for a heading or row. The raw subject a reply quotes is untouched.
function stripSubjectPrefixes(s) {
    return String(s == null ? "" : s).replace(
        /^\s*(?:(?:re|aw|fwd|fw|wg|antw)(?:\[\d+\])?\s*:\s*)+/i, "").trim()
}

// '"Ada" <ada@x>' / 'Ada <ada@x>' / 'ada@x' -> { name, addr }. A bare address
// comes back as { name: "", addr: "ada@x" }.
function parseAddress(raw) {
    var s = String(raw == null ? "" : raw).trim()
    var m = s.match(/^"?(.*?)"?\s*<([^>]+)>\s*$/)
    if (m) return { name: m[1].trim(), addr: m[2].trim() }
    return { name: "", addr: s }
}

// The display name for a from-string, falling back to the address, then "".
function displayName(raw) {
    if (!raw) return ""
    var p = parseAddress(raw)
    return p.name || p.addr
}

// The address for a from-string, falling back to the raw text.
function address(raw) {
    if (!raw) return ""
    return parseAddress(raw).addr
}

// An absolute path -> a percent-encoded file:// URL, segment by segment so the
// slashes survive.
function fileUrl(path) {
    var parts = String(path || "").split("/")
    for (var i = 0; i < parts.length; i++) parts[i] = encodeURIComponent(parts[i])
    return "file://" + parts.join("/")
}

// A file:// URL (any number of leading slashes past the scheme) -> a decoded
// absolute path.
function localPath(url) {
    return decodeURIComponent(String(url).replace(/^file:\/+/, "/"))
}

// The message a failed daemon call should show: the daemon's own reason when it
// gave one, otherwise the caller's fallback.
function errText(r, fallback) {
    return (r && r.error && String(r.error).length) ? r.error : fallback
}
