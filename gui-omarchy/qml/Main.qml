import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Window

// HEY-style single view: one full-screen bucket at a time, a full-screen reading
// view, and a numbered Command Launcher (press H or Ctrl+K) to jump between
// buckets. No panes, no persistent chrome.
ApplicationWindow {
    id: win
    width: 1080
    height: 820
    minimumWidth: 720
    minimumHeight: 520
    visible: true
    title: "Mailbox"
    color: Theme.windowBg

    Behavior on color { ColorAnimation { duration: Theme.anim } }

    readonly property var buckets: [
        { key: "INBOX",       label: "Inbox",       glyph: "\uf01c", blurb: "New for you, then everything you have seen" },
        { key: "Feed",        label: "The Feed",    glyph: "\uf09e", blurb: "Newsletters and things to read at leisure" },
        { key: "Paper Trail", label: "Paper Trail", glyph: "\uf0f6", blurb: "Receipts, confirmations, invoices" },
        { key: "Screener",    label: "The Screener",glyph: "\uf0c0", blurb: "New senders waiting on a decision" },
        { key: "Aside",       label: "Set Aside",   glyph: "\uf02e", blurb: "Pulled out to deal with later" }
    ]

    property int bucketIndex: 0
    property var counts: ({})
    property string openId: ""      // "" → bucket view, otherwise reading view
    property var openMsg: null
    property bool openLoading: false
    property var openAttachments: []

    function currentKey() { return buckets[bucketIndex].key }

    function loadCounts() {
        Mailbox.call(["box", "list"], {}, function (r) {
            if (!r.ok || !r.data) return
            var c = {}
            for (var i = 0; i < r.data.length; i++) {
                var b = r.data[i]
                var name = b.box === "INBOX" ? "INBOX" : b.box
                c[name] = { count: b.count || 0, unseen: b.unseen || 0 }
            }
            win.counts = c
        })
    }

    function loadBucket() {
        win.openId = ""
        win.openMsg = null
        Mailbox.call(["box", "view"], { positional: currentKey(), limit: 200 }, function (r) {
            listModel.setRows(r.ok && r.data ? r.data : [])
        })
    }

    function switchTo(i) {
        launcher.close()
        if (i < 0 || i >= buckets.length) return
        bucketIndex = i
        loadBucket()
    }

    function openMessage(id) {
        win.openId = id
        win.openMsg = null
        win.openLoading = true
        win.openAttachments = []
        PixelBlock.reset()
        Mailbox.call(["message", "view"], { positional: id }, function (r) {
            win.openLoading = false
            if (win.openId !== id) return
            win.openMsg = (r.ok && r.data) ? r.data : null
        })
        Mailbox.call(["attachment", "list"], { positional: id }, function (r) {
            if (win.openId !== id) return
            win.openAttachments = (r.ok && r.data) ? r.data : []
            if (win._demoQl && win.openAttachments.length > 0) {
                win._demoQl = false
                win.openQuickLook(win.openAttachments[0])
            }
        })
    }

    function back() { win.openId = ""; win.openMsg = null }

    function openQuickLook(att) { quickLook.openFor(att) }

    Component.onCompleted: {
        var a = Qt.application.arguments
        var bi = a.indexOf("--bucket")
        if (bi >= 0 && bi + 1 < a.length) {
            var want = a[bi + 1].toLowerCase()
            for (var k = 0; k < buckets.length; k++)
                if (buckets[k].key.toLowerCase() === want || buckets[k].label.toLowerCase().indexOf(want) >= 0)
                    bucketIndex = k
        }
        loadCounts(); loadBucket()
        var oi = a.indexOf("--open")
        if (oi >= 0 && oi + 1 < a.length) { demoOpenId = a[oi + 1]; openFirstTimer.start() }
        else if (a.indexOf("--open-first") >= 0) openFirstTimer.start()
    }
    property string demoOpenId: ""
    property bool _demoQl: Qt.application.arguments.indexOf("--ql") >= 0
    Timer {
        id: openFirstTimer; interval: 900
        onTriggered: win.demoOpenId ? win.openMessage(win.demoOpenId) : bucketView.openHighlighted()
    }

    Connections {
        target: Mailbox
        function onOnlineChanged() { win.loadCounts(); win.loadBucket() }
        function onPushReceived(e, a, b) { win.loadCounts(); win.loadBucket() }
    }

    // The Feed gets its own scroll-and-expand view; every other bucket is a list.
    readonly property bool feedActive: !win.openId && !launcher.opened && currentKey() === "Feed"
    function navView() { return feedActive ? feedView : bucketView }

    Shortcut { sequences: ["Ctrl+K", "Ctrl+P"]; onActivated: launcher.toggle() }
    Shortcut {
        sequence: "Escape"
        onActivated: {
            if (quickLook.opened) quickLook.close()
            else if (launcher.opened) launcher.close()
            else if (win.openId) win.back()
            else if (win.feedActive && feedView.anyOpen()) feedView.collapseAll()
        }
    }
    // Number keys switch buckets straight from anywhere.
    Shortcut { sequence: "1"; onActivated: win.switchTo(0) }
    Shortcut { sequence: "2"; onActivated: win.switchTo(1) }
    Shortcut { sequence: "3"; onActivated: win.switchTo(2) }
    Shortcut { sequence: "4"; onActivated: win.switchTo(3) }
    Shortcut { sequence: "5"; onActivated: win.switchTo(4) }
    Shortcut { sequences: ["j", "Down"]; enabled: !win.openId && !launcher.opened; onActivated: win.navView().move(1) }
    Shortcut { sequences: ["k", "Up"]; enabled: !win.openId && !launcher.opened; onActivated: win.navView().move(-1) }
    Shortcut {
        sequences: ["Return", "Enter"]; enabled: !win.openId && !launcher.opened
        onActivated: win.navView().openHighlighted()
    }
    Shortcut {
        sequences: ["o", "l"]; enabled: !win.openId && !launcher.opened
        onActivated: win.feedActive ? feedView.openFull() : bucketView.openHighlighted()
    }
    Shortcut { sequence: "Ctrl+Q"; onActivated: Qt.quit() }

    // Explicit opaque backdrop so the look does not depend on the window's
    // clear colour or any compositor opacity rule.
    Rectangle {
        anchors.fill: parent
        color: Theme.windowBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    BucketView {
        id: bucketView
        anchors.fill: parent
        opacity: (win.openId || win.currentKey() === "Feed") ? 0 : 1
        visible: opacity > 0.01
        Behavior on opacity { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
    }

    FeedView {
        id: feedView
        anchors.fill: parent
        opacity: (!win.openId && win.currentKey() === "Feed") ? 1 : 0
        visible: opacity > 0.01
        Behavior on opacity { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
    }

    ReadingView {
        id: readingView
        anchors.fill: parent
        opacity: win.openId ? 1 : 0
        visible: opacity > 0.01
        Behavior on opacity { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
    }

    CommandLauncher {
        id: launcher
        anchors.fill: parent
    }

    QuickLook {
        id: quickLook
        anchors.fill: parent
    }
}
