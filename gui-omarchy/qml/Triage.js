.pragma library

// The triage vocabulary, in one place. The reading-view toolbar, the row
// right-click menu (RowActions), the Screener "Move to…" submenu and the
// Command Launcher all offered the same handful of moves, each with its own
// copy of the label strings ("Moved to Paper Trail", "Let into Inbox", …) and
// its own win.routeId / win.pileId / win.trashId call. Now they share this.
//
// dispatch(win, id, targetId) runs the move; label(id) is its confirmation
// flash (for menus that want to show it themselves).

var LABEL = {
    "inbox":       "Let into Inbox",
    "block":       "Blocked",
    "feed":        "Moved to Feed",
    "paper":       "Moved to Paper Trail",
    "aside":       "Set aside",
    "reply-later": "Reply later",
    "aside-done":  "Moved to Inbox",
    "rl-done":     "Moved to Inbox",
    "trash":       "Moved to Trash"
}

function label(id) { return LABEL[id] || "" }

// id ∈ inbox | block | feed | paper | aside | reply-later | aside-done |
//      rl-done | trash | reply-now | open
function dispatch(win, id, targetId) {
    switch (id) {
    case "inbox": case "block": case "feed": case "paper":
        win.routeId(id, LABEL[id], targetId); break
    case "aside":
        win.pileId("aside", LABEL["aside"], targetId); break
    case "reply-later":
        win.pileId("reply-later", LABEL["reply-later"], targetId); break
    case "aside-done":
        win.pileId("aside", LABEL["aside-done"], targetId, true); break
    case "rl-done":
        win.pileId("reply-later", LABEL["rl-done"], targetId, true); break
    case "trash":
        win.trashId(targetId); break
    case "reply-now":
        win.openMsg ? win.startReply(false) : win.openThenReply(targetId); break
    case "open":
        win.openMessage(targetId); break
    }
}
