import QtQuick
import QtQuick.Controls.Basic

// The HEY quick switcher: search focused immediately, numbered destinations,
// arrow keys or digits to pick. Below the destinations, when there is a
// message to act on (the open Thread, or the highlighted row of a list
// bucket), the same triage the reading view and the row right-click menu
// offer — Reply now, Reply later, Set aside, Trash.
Item {
    id: root
    property bool opened: false
    property int active: 0

    // "" is the switcher. "archive" is the second pane the Archive action opens
    // over it: every archive box, fuzzy-filtered, Enter moves the target Thread
    // into the highlighted one.
    property string pane: ""
    property string archiveTargetId: ""
    property var archiveBoxes: []
    // The boxes mail already moves through — dropped from the archive picker,
    // which is only for the tree behind them. Matches `routingOrder` server-side.
    readonly property var routingKeys: [
        "INBOX", "Feed", "Paper Trail", "Screener", "Aside", "Reply Later", "Sent", "Drafts", "Junk"
    ]

    function open() {
        query.text = ""
        pane = ""
        archiveTargetId = ""
        // active is a list position, not a bucket index — they differ now that
        // the Screener is skipped. Land on the current bucket if it is listed.
        active = 0
        for (var i = 0; i < rows.length; i++)
            if (rows[i].i === win.bucketIndex) { active = i; break }
        opened = true
        Qt.callLater(function () { query.forceActiveFocus() })
    }
    function close() {
        opened = false
        pane = ""
        // Hand keyboard focus back to the bucket view. Otherwise the search
        // field keeps active focus while hidden and claims the bucket-jump
        // number keys (1–7) as text input before the window Shortcuts can see
        // them — so after closing the launcher you could not switch buckets
        // until you had entered one some other way.
        query.focus = false
        win.navView().forceActiveFocus()
    }
    function toggle() { opened ? close() : open() }

    function results() {
        if (pane === "archive") return []
        var q = query.text.trim().toLowerCase()
        var out = []
        for (var i = 0; i < win.buckets.length; i++) {
            var b = win.buckets[i]
            // The Screener is not a destination here: it is reached only from
            // the button in the Inbox, when senders are actually waiting.
            if (b.key === "Screener") continue
            if (!q || b.label.toLowerCase().indexOf(q) >= 0)
                out.push({ i: i, b: b })
        }
        return out
    }
    property var rows: (query.text, opened, pane, results())

    // ---- archive picker -------------------------------------------------------
    // A subsequence match: every character of the needle shows up in order in
    // the haystack. Enough to pull "Archive/2021/receipts" out of a long tree
    // by typing "a21rec".
    function fuzzy(needle, hay) {
        needle = needle.toLowerCase(); hay = hay.toLowerCase()
        var j = 0
        for (var i = 0; i < hay.length && j < needle.length; i++)
            if (hay.charAt(i) === needle.charAt(j)) j++
        return j === needle.length
    }
    function archiveResults() {
        if (pane !== "archive") return []
        var q = query.text.trim()
        var out = []
        for (var i = 0; i < archiveBoxes.length; i++) {
            var b = archiveBoxes[i]
            if (root.routingKeys.indexOf(b.box) >= 0) continue
            if (!q || root.fuzzy(q, b.box)) out.push(b)
        }
        return out
    }
    property var archRows: (query.text, pane, archiveBoxes, archiveResults())

    function enterArchive() {
        root.archiveTargetId = win.actionTargetId()
        if (!root.archiveTargetId) { root.close(); return }
        root.pane = "archive"
        root.active = 0
        query.text = ""
        win.loadArchiveBoxes(function (list) { root.archiveBoxes = list; root.active = 0 })
        Qt.callLater(function () { query.forceActiveFocus() })
    }
    function exitArchive() {
        root.pane = ""
        root.active = 0
        query.text = ""
        Qt.callLater(function () { query.forceActiveFocus() })
    }

    // The action rows: Compose is always here (it needs no target), the triage
    // rows only when there is a message to act on. Context-aware — the pile you
    // are already in offers "Move to Inbox" instead of a move onto itself.
    // `kbd` is the matching global key, where there is one.
    function actionResults() {
        if (!opened || pane === "archive") return []
        var _q = query.text.trim().toLowerCase()
        // Compose needs no target, so it is always offered — and answers to C
        // here, the key that opens it everywhere else. `_f` appends it and then
        // narrows the whole list to the query, so typing "trash" / "set aside"
        // lands the highlight on that action instead of leaving it on the first
        // row (where Return would fire the wrong thing).
        var _compose = [ { id: "compose", label: "Compose new message", glyph: "\uf040", kbd: "C" } ]
        // Search needs no target either, and answers to / here — the key that
        // opens the search overlay everywhere else.
        var _search = [ { id: "search", label: "Search all mail", glyph: "\uf002", kbd: "/" } ]
        function _f(list) {
            var all = list.concat(_compose).concat(_search)
            return _q ? all.filter(function (a) { return a.label.toLowerCase().indexOf(_q) >= 0 }) : all
        }
        if (win.actionTargetId() === "") return _f([])
        // A Drafts row stands for a draft, not a thread to triage — only
        // Compose applies (opening the row is what edits or sends it).
        if (!win.openId && win.currentKey() === "Drafts") return _f([])
        // The Screener is a decision about the sender, not a read about a mail:
        // no reply until they are screened in. Two one-tap primaries, the two
        // move destinations, Set aside, Trash \u2014 the same set the row menu and
        // the reading-view toolbar carry.
        if (win.inScreener)
            return _f([
                { id: "route-inbox", label: "Let into Inbox",      glyph: "\uf01c", kbd: "I" },
                { id: "route-block", label: "Block",                glyph: "\uf05e", kbd: "B", danger: true },
                { id: "route-feed",  label: "Move to Feed",         glyph: "\uf09e", kbd: "" },
                { id: "route-paper", label: "Move to Paper Trail",  glyph: "\uf0f6", kbd: "" },
                { id: "aside",       label: "Set aside",            glyph: "\uf02e", kbd: "A" },
                { id: "trash",       label: "Trash",                glyph: "\uf1f8", kbd: "T", danger: true }
            ])
        var k = win.openId ? "" : win.currentKey()
        var out = [ { id: "reply-now", label: "Reply now", glyph: "\uf112", kbd: "" } ]
        if (k === "Aside")
            out.push({ id: "aside-done", label: "Move to Inbox", glyph: "\uf01c", kbd: "" })
        else
            out.push({ id: "aside", label: "Set aside", glyph: "\uf02e", kbd: "A" })
        if (k === "Reply Later")
            out.push({ id: "rl-done", label: "Move to Inbox", glyph: "\uf01c", kbd: "" })
        else
            out.push({ id: "reply-later", label: "Reply later", glyph: "\uf017", kbd: "R" })
        // Archive opens the second pane rather than acting straight away.
        out.push({ id: "archive", label: "Archive to\u2026", glyph: "\uf187", kbd: "" })
        out.push({ id: "trash", label: "Trash", glyph: "\uf1f8", kbd: "T", danger: true })
        return _f(out)
    }
    property var acts: (query.text, opened, win.openId, win.bucketIndex, actionResults())

    function choose(listPos) {
        if (pane === "archive") {
            var ab = archRows[listPos]
            if (!ab) return
            var id = root.archiveTargetId
            root.close()
            win.moveId(id, ab.box, "Archived to " + ab.box)
            return
        }
        if (listPos < rows.length) {
            var r = rows[listPos]
            if (r) win.switchTo(r.i)
            return
        }
        fire(acts[listPos - rows.length])
    }
    function fire(a) {
        if (!a) { root.close(); return }
        // Compose stands on its own — no message to target, so it runs before
        // the guard below.
        if (a.id === "compose") { root.close(); win.startCompose(); return }
        if (a.id === "search") { root.close(); win.openSearch(); return }
        // Archive swaps the launcher into its box picker instead of firing now.
        if (a.id === "archive") { root.enterArchive(); return }
        var id = win.actionTargetId()
        root.close()
        if (!id) return
        if (a.id === "reply-now") win.openMsg ? win.startReply(false) : win.openThenReply(id)
        else if (a.id === "route-inbox") win.routeId("inbox", "Let into Inbox", id)
        else if (a.id === "route-block") win.routeId("block", "Blocked", id)
        else if (a.id === "route-feed") win.routeId("feed", "Moved to Feed", id)
        else if (a.id === "route-paper") win.routeId("paper", "Moved to Paper Trail", id)
        else if (a.id === "aside") win.pileId("aside", "Set aside", id)
        else if (a.id === "aside-done") win.pileId("aside", "Moved to Inbox", id, true)
        else if (a.id === "reply-later") win.pileId("reply-later", "Reply later", id)
        else if (a.id === "rl-done") win.pileId("reply-later", "Moved to Inbox", id, true)
        else if (a.id === "trash") win.trashId(id)
    }
    function total() {
        if (pane === "archive") return archRows.length
        return rows.length + acts.length
    }

    visible: opened || scrim.opacity > 0.01

    // Scrim
    Rectangle {
        id: scrim
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.55)
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        TapHandler { onTapped: root.close() }
    }

    // Card
    Rectangle {
        id: card
        width: Math.min(560, root.width - 80)
        anchors.horizontalCenter: parent.horizontalCenter
        y: root.opened ? root.height * 0.16 : root.height * 0.16 - 12
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        height: content.implicitHeight + 24
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        Column {
            id: content
            width: parent.width
            padding: 12
            spacing: 8

            // Search field
            Rectangle {
                width: parent.width - 24
                height: 42
                radius: Theme.radiusSmall
                color: Theme.windowBg
                border.width: 1
                border.color: query.activeFocus ? Theme.accent : Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }

                Row {
                    anchors.fill: parent
                    anchors.leftMargin: 14
                    anchors.rightMargin: 14
                    spacing: 10
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: root.pane === "archive" ? "\uf187" : "\uf002"
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    TextField {
                        id: query
                        width: parent.width - 34
                        anchors.verticalCenter: parent.verticalCenter
                        placeholderText: root.pane === "archive" ? "Archive to…" : "Jump to…"
                        color: Theme.textPrimary
                        placeholderTextColor: Theme.textDim
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        background: null
                        leftPadding: 0
                        onTextChanged: root.active = 0
                        Keys.onDownPressed: root.active = Math.min(root.total() - 1, root.active + 1)
                        Keys.onUpPressed: root.active = Math.max(0, root.active - 1)
                        Keys.onReturnPressed: root.choose(root.active)
                        Keys.onEscapePressed: root.pane === "archive" ? root.exitArchive() : root.close()
                        Keys.onPressed: function (e) {
                            // In the archive picker every key is filter text —
                            // no compose/search/digit shortcuts to steal it.
                            if (root.pane === "archive") return
                            // C on an empty field is the compose shortcut, the
                            // same as `c` everywhere else; once a query is being
                            // typed it is just a letter to match on.
                            if (query.text === "" && e.key === Qt.Key_C
                                    && !(e.modifiers & Qt.ControlModifier)) {
                                root.fire({ id: "compose" }); e.accepted = true; return
                            }
                            // Likewise / on an empty field opens search, mirroring
                            // the global `/` shortcut.
                            if (query.text === "" && e.key === Qt.Key_Slash
                                    && !(e.modifiers & Qt.ControlModifier)) {
                                root.fire({ id: "search" }); e.accepted = true; return
                            }
                            if (e.text.length === 1 && e.text >= "1" && e.text <= "9") {
                                var n = parseInt(e.text) - 1
                                if (n < root.rows.length) { root.choose(n); e.accepted = true }
                            }
                        }
                    }
                }
            }

            // Destinations
            Repeater {
                model: root.rows
                Rectangle {
                    width: content.width - 24
                    height: 46
                    radius: Theme.radiusSmall
                    color: index === root.active ? Theme.selection
                         : itemHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }

                    Row {
                        anchors.fill: parent
                        anchors.leftMargin: 12
                        anchors.rightMargin: 12
                        spacing: 12

                        Kbd {
                            anchors.verticalCenter: parent.verticalCenter
                            text: (index + 1).toString()
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData.b.glyph
                            font.family: Theme.fontFamily
                            font.pixelSize: 14
                            color: index === root.active ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Column {
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 2
                            Text {
                                text: modelData.b.label
                                font.family: Theme.fontFamily
                                font.pixelSize: 13
                                font.weight: index === root.active ? Font.DemiBold : Font.Normal
                                color: Theme.textPrimary
                                Behavior on color { ColorAnimation { duration: Theme.anim } }
                            }
                        }
                    }

                    // Live count on the right.
                    Pill {
                        anchors { verticalCenter: parent.verticalCenter; right: parent.right; rightMargin: 14 }
                        value: {
                            var key = modelData.b.key
                            var c = win.counts[key]
                            if (!c) return 0
                            return (key === "INBOX" || key === "Screener") ? (c.unseen || c.count) : c.count
                        }
                        strong: modelData.b.key === "INBOX" || modelData.b.key === "Screener"
                    }

                    HoverHandler { id: itemHover }
                    TapHandler { onTapped: root.choose(index) }
                }
            }

            // ---- Actions on the current message ----------------------------
            Rectangle {
                width: content.width - 24
                height: 1
                color: Theme.hairline
                visible: root.acts.length > 0
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Text {
                visible: root.acts.length > 0
                leftPadding: 4
                text: "Actions"
                font.family: Theme.fontFamily
                font.pixelSize: 10
                font.weight: Font.DemiBold
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Repeater {
                model: root.acts
                Rectangle {
                    readonly property int pos: root.rows.length + index
                    width: content.width - 24
                    height: 40
                    radius: Theme.radiusSmall
                    color: pos === root.active ? Theme.selection
                         : actHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }

                    Row {
                        anchors.fill: parent
                        anchors.leftMargin: 12
                        anchors.rightMargin: 12
                        spacing: 12

                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            width: 16
                            horizontalAlignment: Text.AlignHCenter
                            text: modelData.glyph
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            color: modelData.danger ? Theme.red
                                 : pos === root.active ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData.label
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            font.weight: pos === root.active ? Font.DemiBold : Font.Normal
                            color: modelData.danger ? Theme.red : Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }

                    Kbd {
                        visible: modelData.kbd !== ""
                        anchors { verticalCenter: parent.verticalCenter; right: parent.right; rightMargin: 14 }
                        text: modelData.kbd
                    }

                    HoverHandler { id: actHover }
                    TapHandler { onTapped: root.fire(modelData) }
                }
            }

            // ---- Archive picker -------------------------------------------
            Text {
                visible: root.pane === "archive"
                leftPadding: 4
                text: "Move to archive box"
                font.family: Theme.fontFamily
                font.pixelSize: 10
                font.weight: Font.DemiBold
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Text {
                visible: root.pane === "archive" && root.archRows.length === 0
                leftPadding: 4
                topPadding: 4
                text: root.archiveBoxes.length === 0 ? "Loading boxes…" : "No archive box matches"
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Repeater {
                model: root.archRows
                Rectangle {
                    width: content.width - 24
                    height: 40
                    radius: Theme.radiusSmall
                    color: index === root.active ? Theme.selection
                         : archHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }

                    Row {
                        anchors.fill: parent
                        anchors.leftMargin: 12
                        anchors.rightMargin: 12
                        spacing: 12

                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            width: 16
                            horizontalAlignment: Text.AlignHCenter
                            text: ""
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            color: index === root.active ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData.box
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            font.weight: index === root.active ? Font.DemiBold : Font.Normal
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }

                    Pill {
                        visible: (modelData.count || 0) > 0
                        anchors { verticalCenter: parent.verticalCenter; right: parent.right; rightMargin: 14 }
                        value: modelData.count || 0
                    }

                    HoverHandler { id: archHover }
                    TapHandler { onTapped: root.choose(index) }
                }
            }
        }
    }
}
