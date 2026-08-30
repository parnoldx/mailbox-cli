import QtQuick
import QtQuick.Controls.Basic

// The Feed as a feed. Instead of a list that hands off to a full-screen reader,
// every item is a card in one chronological column: sender, subject and a few
// lines of the body, with a "Read more" that expands the whole text in place
// and collapses again. A divider marks where the reader got to last time —
// everything above it arrived since; below it has been seen. How far down the
// column you scroll (and anything you expand) is remembered between sessions in
// Mailbox.stateGet/Set("feed.mark"), keyed by the message date.
Item {
    id: root

    // Only do work while The Feed is the visible bucket: the shared listModel
    // also carries Inbox, Screener and the rest, and we must not prefetch their
    // bodies or partition their rows.
    readonly property bool active: win.currentKey() === "Feed"

    property var rows: []            // newest-first, straight off listModel
    property int newCount: 0         // rows[0 .. newCount) arrived since last visit
    property string mark: ""         // the "yyyy-MM-dd HH:mm" watermark at load
    property string pendingMark: ""  // advanced as the reader scrolls / expands
    property int hi: -1              // keyboard highlight into rows

    // id -> { snippet, body, fmt, html, hasAtt } once `message view` has answered,
    // or null while the request is in flight.
    property var bodies: ({})
    property var openIds: ({})       // id -> true for the cards currently expanded

    // -- body prefetch, a few requests in flight at a time ------------------
    // QML only notifies a `var` change on a *new* reference, so every mutation
    // rebuilds the object before assigning it back.
    property var _queue: []
    property var _requested: ({})
    property int _inflight: 0
    function needBody(id) {
        if (!active || _requested[id]) return
        _requested[id] = true
        _queue.push(id)
        _pump()
    }
    function _pump() {
        while (_inflight < 5 && _queue.length > 0) {
            var id = _queue.shift()
            _inflight++
            Mailbox.call(["message", "view"], { positional: id }, function (r) {
                root._inflight--
                if (r && r.ok && r.data) {
                    var d = r.data
                    var nb = Object.assign({}, root.bodies)
                    nb[d.id] = {
                        body: d.body || "",
                        fmt: d.body_format || "plain",
                        html: d.body_html || ""
                    }
                    root.bodies = nb
                }
                root._pump()
            })
        }
    }

    // -- the read watermark ------------------------------------------------
    Timer { id: persistTimer; interval: 500; onTriggered: root._persist() }
    function _persist() {
        if (root.pendingMark && root.pendingMark !== root.mark)
            Mailbox.stateSet("feed.mark", root.pendingMark)
    }
    function markUpTo(dateRaw) {
        if (dateRaw && dateRaw > pendingMark) {
            pendingMark = dateRaw
            persistTimer.restart()
        }
    }
    function markAllRead() {
        if (rows.length > 0) markUpTo(rows[0].dateRaw)
        root._persist()
        newCount = 0
    }

    function reload() {
        if (!active) return
        var rs = []
        for (var i = 0; i < listModel.count; i++)
            rs.push(listModel.get(i))
        mark = Mailbox.stateGet("feed.mark", "")
        pendingMark = mark
        var n = 0
        for (i = 0; i < rs.length; i++)
            if ((rs[i].dateRaw || "") > mark) n++
        newCount = n
        rows = rs
        hi = rs.length > 0 ? 0 : -1
        openIds = ({})
    }

    onActiveChanged: {
        if (active) reload()
        else _persist()
    }
    onVisibleChanged: if (!visible) _persist()
    Component.onCompleted: reload()

    Connections {
        target: listModel
        function onChanged() { root.reload() }
    }

    // -- keyboard --------------------------------------------------------
    function move(d) {
        if (rows.length === 0) return
        hi = Math.max(0, Math.min(rows.length - 1, (hi < 0 ? 0 : hi) + d))
        flick.ensureVisible(hi)
    }
    function toggle(id) {
        var o = Object.assign({}, openIds)
        if (o[id]) {
            delete o[id]
        } else {
            o[id] = true
            for (var i = 0; i < rows.length; i++)
                if (rows[i].id === id) markUpTo(rows[i].dateRaw)
        }
        openIds = o
    }
    function openHighlighted() {
        if (hi >= 0 && hi < rows.length) toggle(rows[hi].id)
    }
    function openFull() {
        if (hi >= 0 && hi < rows.length) win.openMessage(rows[hi].id)
    }
    function anyOpen() { return Object.keys(openIds).length > 0 }
    function collapseAll() { openIds = ({}) }

    // Opaque floor. Main cross-fades this view against BucketView; without a
    // solid background the outgoing bucket header shows through the fade for a
    // few frames. With it, the Feed covers the bucket the instant it is shown.
    Rectangle {
        anchors.fill: parent
        color: Theme.windowBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    Flickable {
        id: flick
        anchors.fill: parent
        contentWidth: width
        contentHeight: col.implicitHeight + 140
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        // Best-effort "scroll it into view": card tops are one row height apart
        // is not true once things expand, so just nudge if the highlight sits
        // outside the viewport.
        function ensureVisible(i) {
            var item = repeater.itemAt(i)
            if (!item) return
            var top = item.mapToItem(contentItem, 0, 0).y
            var bot = top + item.height
            if (top < contentY + 8) contentY = Math.max(0, top - 8)
            else if (bot > contentY + height - 8) contentY = bot - height + 8
        }

        Column {
            id: col
            // Same measure as BucketView, so the header lines up pixel-for-pixel
            // with every other bucket while the two views cross-fade.
            x: Math.max(40, (parent.width - 880) / 2)
            width: Math.min(880, parent.width - 80)
            topPadding: 56
            spacing: 6

            // Header — the same shape as every other bucket (see BucketView):
            // the bucket glyph, its name, and a status line under it. Keeping it
            // identical means switching in and out of the Feed never swaps one
            // header design for another mid-fade.
            Item {
                width: parent.width
                height: hdr.implicitHeight

                Row {
                    id: hdr
                    anchors.left: parent.left
                    anchors.right: markBtn.visible ? markBtn.left : parent.right
                    anchors.rightMargin: 14
                    spacing: 14

                    Text {
                        text: win.buckets[win.bucketIndex].glyph
                        font.family: Theme.fontFamily
                        font.pixelSize: 30
                        color: Theme.accent
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Column {
                        width: parent.width - 44
                        spacing: 4
                        Text {
                            text: "The Feed"
                            font.family: Theme.fontFamily
                            font.pixelSize: 30
                            font.weight: Font.Bold
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            text: root.newCount > 0
                                  ? root.newCount + (root.newCount === 1 ? " new item since your last visit"
                                                                         : " new items since your last visit")
                                  : "Nothing new — you are caught up"
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                }

                Rectangle {
                    id: markBtn
                    anchors.right: parent.right
                    anchors.verticalCenter: hdr.verticalCenter
                    visible: root.newCount > 0
                    width: markRow.implicitWidth + 22
                    height: 28
                    radius: 14
                    color: markHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        id: markRow
                        anchors.centerIn: parent
                        spacing: 6
                        Text {
                            text: ""
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: Theme.green
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            text: "Mark all read"
                            font.family: Theme.fontFamily
                            font.pixelSize: 11
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: markHover }
                    TapHandler { onTapped: root.markAllRead() }
                }
            }

            // The gap BucketView leaves between its header and its first row.
            Item { width: 1; height: 28 }

            // The cards keep a 680 reading measure, centred in the window like
            // before, while the header above still spans the wider bucket
            // column so it lines up with every other bucket.
            Column {
                id: list
                anchors.horizontalCenter: parent.horizontalCenter
                width: Math.min(680, col.width)
                spacing: 6

                Repeater {
                    id: repeater
                    model: root.active ? root.rows : []
                    FeedCard {
                        width: list.width
                        controller: root
                        scroller: flick
                        row: modelData
                        isNew: index < root.newCount
                        highlighted: root.hi === index
                        expanded: !!root.openIds[modelData.id]
                        showDividerBelow: root.newCount > 0 && root.newCount < root.rows.length
                                          && index === root.newCount - 1
                    }
                }

                // When everything is already read the divider has nothing above it;
                // still show it so the column has a "caught up" cap.
                FeedDivider {
                    width: parent.width
                    visible: root.active && root.newCount === 0 && root.rows.length > 0
                    label: "You are all caught up"
                }

                Column {
                    width: parent.width
                    spacing: 10
                    topPadding: 70
                    visible: root.active && root.rows.length === 0
                    Text {
                        anchors.horizontalCenter: parent.horizontalCenter
                        text: ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 32
                        color: Theme.hairline
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        anchors.horizontalCenter: parent.horizontalCenter
                        text: "The Feed is empty"
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
            }
        }
    }

    // Hints, mirroring BucketView's corner furniture.
    Row {
        anchors { right: parent.right; bottom: parent.bottom; margins: 20 }
        spacing: 10
        opacity: 0.75
        Kbd { text: "J" }
        Kbd { text: "K" }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "move"
            font.family: Theme.fontFamily
            font.pixelSize: 11
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Item { width: 8; height: 1 }
        Kbd { text: "Return" }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "expand"
            font.family: Theme.fontFamily
            font.pixelSize: 11
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Item { width: 8; height: 1 }
        Kbd { text: "O" }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "open"
            font.family: Theme.fontFamily
            font.pixelSize: 11
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Rectangle {
        anchors { left: parent.left; bottom: parent.bottom; margins: 22 }
        width: 8; height: 8; radius: 4
        opacity: 0.85
        color: Mailbox.online ? Theme.green : Theme.yellow
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
