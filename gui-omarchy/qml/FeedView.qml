import QtQuick
import QtQuick.Window
import QtWebEngine

// The Feed as a feed — one chronological column of cards (sender, subject, a few
// lines of the body) with a "Read more" that expands the whole article in place,
// and a rule marking where the reader got to last time. How far down you scroll
// and anything you expand is remembered in Mailbox.stateGet/Set("feed.mark"),
// keyed by message date.
//
// The whole column is ONE HTML document in ONE WebEngineView (qml/vendor/feed.html).
// The old design gave every card its own WebEngineView, and each one swallowed
// the wheel so the surrounding QML Flickable never scrolled over article content.
// Now there is a single scroll surface. This file keeps all the data work —
// listModel -> rows, body prefetch, watermark persistence — and drives the page
// through __set*()/__state(); the page sends discrete actions back as `feed:`
// URLs caught in onNavigationRequested.
Item {
    id: root

    // Only do work while The Feed is the visible bucket: the shared listModel
    // also carries Inbox, Screener and the rest.
    readonly property bool active: win.currentKey() === "Feed"

    property var rows: []            // newest-first, straight off listModel
    property int newCount: 0         // rows[0 .. newCount) arrived since last visit
    property string mark: ""         // the "yyyy-MM-dd HH:mm" watermark at load
    property string pendingMark: ""  // advanced as the reader scrolls / expands
    property int hi: -1              // keyboard highlight, kept in step with the page
    property bool _anyOpen: false    // any card expanded — refreshed by the poll
    property bool _ready: false      // feed.html has loaded and __set*() exist

    // id -> { html, text } once `message view` has answered.
    property var bodies: ({})

    // -- body prefetch, a few requests in flight at a time ------------------
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
                    var rec = { html: d.body_html || "", text: d.body || "" }
                    var nb = Object.assign({}, root.bodies)
                    nb[d.id] = rec
                    root.bodies = nb
                    root._pushBody(d.id, rec)
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
        _persist()
        newCount = 0
        _pushData()
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
        _anyOpen = false
        _pushData()
        for (i = 0; i < rs.length; i++) needBody(rs[i].id)
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
    Connections {
        target: Theme
        function onChanged() {
            if (root._ready)
                root._js("__setTheme(" + JSON.stringify(root._themeObj()) + ")")
        }
    }

    // -- QML -> page --------------------------------------------------------
    function _themeObj() {
        return {
            dark: Theme.dark,
            bg: "" + Theme.windowBg,
            fg: "" + Theme.textPrimary,
            dim: "" + Theme.textDim,
            accent: "" + Theme.accent,
            card: "" + Theme.cardBg,
            cardHover: "" + Theme.cardHover,
            hairline: "" + Theme.hairline,
            sel: "" + Theme.selection,
            green: "" + Theme.green,
            sheet: "" + (Theme.dark ? Theme.background : "#ffffff"),
            radius: Theme.radiusSmall
        }
    }
    function _initials(s) {
        if (!s) return "?"
        var parts = s.trim().split(/\s+/)
        if (parts.length === 1) return parts[0].substring(0, 2).toUpperCase()
        return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
    }
    function _dataObj() {
        var out = []
        for (var i = 0; i < rows.length; i++) {
            var r = rows[i]
            var seed = (r.fromAddr && r.fromAddr.length) ? r.fromAddr : (r.fromName || "")
            out.push({
                id: r.id,
                fromName: r.fromName || "",
                fromAddr: r.fromAddr || "",
                initials: _initials(r.fromName || r.fromAddr || ""),
                subject: r.subject || "",
                date: r.date || "",
                dateRaw: r.dateRaw || "",
                avatarColor: "" + Theme.avatarColor(seed)
            })
        }
        return {
            rows: out,
            newCount: newCount,
            header: {
                glyph: win.buckets[win.bucketIndex].glyph,
                title: "The Feed",
                status: newCount > 0
                    ? (newCount + (newCount === 1 ? " new item since your last visit"
                                                  : " new items since your last visit"))
                    : "Nothing new — you are caught up"
            }
        }
    }
    function _js(s, cb) { if (webLoader.item) webLoader.item.runJavaScript(s, cb || function () {}) }
    function _pushData() { if (_ready) _js("__setData(" + JSON.stringify(_dataObj()) + ")") }
    function _pushBody(id, rec) {
        if (_ready) _js("__setBody(" + JSON.stringify(id) + "," + JSON.stringify(rec) + ")")
    }

    // -- public API used by Main.qml -------------------------------------
    function move(d) { if (_ready) _js("__move(" + d + ")") }
    function openHighlighted() { if (_ready) _js("__toggleHi()") }
    function openFull() { if (_ready) _js("__openFullHi()") }
    function anyOpen() { return _anyOpen }
    function collapseAll() {
        _anyOpen = false
        if (_ready) _js("__collapseAll()")
    }

    // Opaque floor. Main cross-fades this view against BucketView; without a
    // solid background the outgoing bucket header shows through for a few frames.
    Rectangle {
        anchors.fill: parent
        color: Theme.windowBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    // The feed page. Wrapped in a Loader so a Wayland remap (a workspace switch
    // away and back) that leaves QtWebEngine painting a black surface can be
    // shaken off by tearing the whole view down and rebuilding it — the only
    // thing that reliably works. State is round-tripped through the page.
    Loader {
        id: webLoader
        width: parent.width
        height: parent.height
        active: !root._reviving
        sourceComponent: WebEngineView {
            backgroundColor: Theme.windowBg
            url: "qrc:/qml/vendor/feed.html"

            onLoadingChanged: function (req) {
                if (req.status !== WebEngineView.LoadSucceededStatus) return
                root._ready = true
                root._js("__setDrLib(" + JSON.stringify(
                    (typeof DarkReaderJs === "string") ? DarkReaderJs : "") + ")")
                root._js("__setTheme(" + JSON.stringify(root._themeObj()) + ")")
                root._pushData()
                for (var id in root.bodies) root._pushBody(id, root.bodies[id])
                if (root._pendingRestore) {
                    root._js("__restore(" + JSON.stringify(root._pendingRestore) + ")")
                    root._pendingRestore = null
                    uncoverTimer.restart()
                } else {
                    root._covered = false
                }
            }

            onNavigationRequested: function (req) {
                var u = "" + req.url
                if (u.indexOf("feed:") === 0) {
                    req.action = WebEngineNavigationRequest.IgnoreRequest
                    if (u.indexOf("feed:openfull/") === 0)
                        win.openMessage(decodeURIComponent(u.substring(14)))
                    else if (u === "feed:markall")
                        root.markAllRead()
                    else if (u.indexOf("feed:mark/") === 0)
                        root.markUpTo(decodeURIComponent(u.substring(10)))
                    return
                }
                if (req.navigationType === WebEngineNavigationRequest.LinkClickedNavigation) {
                    Qt.openUrlExternally(req.url)
                    req.action = WebEngineNavigationRequest.IgnoreRequest
                }
            }
            onNewWindowRequested: function (req) { Qt.openUrlExternally(req.requestedUrl) }
        }
    }

    // Read the page's expand/scroll state back for Main.qml's Esc handler
    // (anyOpen must answer synchronously) and to persist the scroll watermark.
    Timer {
        interval: 400; repeat: true
        running: root.active && root._ready && !root._reviving
        onTriggered: root._js("__state()", function (s) {
            if (!s) return
            root._anyOpen = !!s.anyOpen
            if (s.mark) root.markUpTo(s.mark)
            if (typeof s.hi === "number" && s.hi >= 0) root.hi = s.hi
        })
    }

    // --- QtWebEngine leaves a black GPU surface behind after its Wayland
    // surface is unmapped and remapped (a Hyprland workspace switch). Neither
    // Window.visibility nor expose events move on a workspace switch — the only
    // hint is the window losing focus for more than a moment. On the way back,
    // snapshot the page, rebuild the WebEngineView and put the page back.
    property bool _reviving: false
    property bool _covered: false
    property bool _dirty: false
    property var _pendingRestore: null

    readonly property var _win: Window.window

    function _repaintCycle() {
        if (!_ready || !webLoader.item) { _covered = false; return }
        _covered = true
        _js("__state()", function (s) {
            root._pendingRestore = s || null
            root._ready = false
            root._reviving = true
            reviveTimer.restart()
        })
    }
    Timer { id: reviveTimer; interval: 60; onTriggered: root._reviving = false }
    Timer { id: uncoverTimer; interval: 200; onTriggered: root._covered = false }

    Connections {
        target: root._win
        enabled: root._win !== null
        function onVisibilityChanged() {
            var hidden = root._win.visibility === Window.Hidden
                      || root._win.visibility === Window.Minimized
            if (hidden) { root._covered = true; root._dirty = true }
            else if (root._dirty) { root._dirty = false; root._repaintCycle() }
        }
    }
    // Only a real unmap (workspace switch -> platform expose event, or a
    // minimize) rebuilds the view. The mouse leaving the window drops focus but
    // fires none of these, so it never triggers the ~0.5s reload.
    Connections {
        target: SurfaceWatcher
        function onObscured() { root._covered = true; root._dirty = true }
        function onRevealed() {
            if (root._dirty) { root._dirty = false; root._repaintCycle() }
        }
    }
    Rectangle {
        anchors.fill: parent
        z: 100
        visible: root._covered
        color: Theme.windowBg
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
