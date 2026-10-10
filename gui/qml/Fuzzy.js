.pragma library

// The fuzzy subsequence match the Command Launcher coined and the archive
// picker shares: every character of the needle shows up in order in the
// haystack — enough to pull "Archive/2021/receipts" out of a long tree by
// typing "a21rec". Case-blind.
function fuzzy(needle, hay) {
    needle = needle.toLowerCase(); hay = hay.toLowerCase()
    var j = 0
    for (var i = 0; i < hay.length && j < needle.length; i++)
        if (hay.charAt(i) === needle.charAt(j)) j++
    return j === needle.length
}

// The boxes mail moves into one at a time, off `box list --archive` rows
// ({ box, account, count, ... }): the archive tree plus Feed and Paper Trail
// (whose rows decide the sender's routing as well), minus the routing
// Destinations the launcher picks contextually — these have rows of their own
// in menus. Matches the server's routingOrder. `acct` narrows to one account
// ("" is the Primary); undefined/null leaves them all in. The routing boxes
// compare on the name's leaf, so a Secondary's prefixed copies ("work/Sent",…)
// drop with them.
var ROUTING_KEYS = ["INBOX", "Screener", "Aside", "Reply Later", "Sent", "Drafts", "Junk"]

function leaf(box) {
    return box.split("/").pop()
}

function moveBoxes(list, acct) {
    var out = []
    for (var i = 0; i < list.length; i++) {
        var b = list[i]
        if (ROUTING_KEYS.indexOf(leaf(b.box)) >= 0) continue
        if (acct !== undefined && acct !== null && b.account !== acct) continue
        out.push(b)
    }
    return out
}

// The archive view's picker is narrower still: Feed and Paper Trail have
// their own buckets (keys 2 and 3) with their own views of that mail, so
// listing them here only makes the same mail reachable two ways. The move
// menu keeps them — moving into them also routes the sender.
function pickerBoxes(list, acct) {
    return moveBoxes(list, acct).filter(function (b) {
        return leaf(b.box) !== "Feed" && leaf(b.box) !== "Paper Trail"
    })
}
