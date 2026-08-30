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

    property bool composeOpen: false   // the compose view sits above everything else

    function currentKey() { return buckets[bucketIndex].key }

    // ---- compose -------------------------------------------------------
    function startCompose() {
        composer.openNew()
        win.composeOpen = true
    }
    function startReply(all) {
        if (!win.openMsg) return
        composer.openReply({
            id: win.openMsg.id,
            all: !!all,
            from: win.openMsg.from || "",
            subject: win.openMsg.subject || ""
        })
        win.composeOpen = true
    }
    // Called by ComposerView when Send is pressed: close the view now, hand the
    // payload to the toast, which fires the real call after the grace period.
    function beginSend(payload) {
        win.composeOpen = false
        sendToast.start(payload)
    }
    function reopenComposer(form) {
        composer.restore(form)
        win.composeOpen = true
    }
    function flash(text) {
        flashLabel.text = text
        flashLabel.shown = true
        flashTimer.restart()
    }

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

    // Re-pull the open bucket's rows in place, without disturbing the reader.
    function refreshBucket() {
        Mailbox.call(["box", "view"], { positional: currentKey(), limit: 200 }, function (r) {
            listModel.setRows(r.ok && r.data ? r.data : [])
        })
    }

    function loadBucket() {
        win.openId = ""
        win.openMsg = null
        refreshBucket()
    }

    function switchTo(i) {
        launcher.close()
        if (i < 0 || i >= buckets.length) return
        bucketIndex = i
        loadBucket()
    }
    function switchToKey(k) {
        for (var i = 0; i < buckets.length; i++)
            if (buckets[i].key === k) { switchTo(i); return }
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
            // Opening a message is what marks it read. `message view` does not
            // touch the flag, so do it here — once, and only if it was unread —
            // then refresh the counts and the row behind the reader.
            if (win.openMsg && win.openMsg.seen === false) {
                Mailbox.call(["seen"], { positional: id }, function () {
                    win.loadCounts()
                    win.refreshBucket()
                })
            }
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
        if (a.indexOf("--compose") >= 0) composeTimer.start()
    }
    Timer { id: composeTimer; interval: 700; onTriggered: win.startCompose() }
    property string demoOpenId: ""
    property bool _demoQl: Qt.application.arguments.indexOf("--ql") >= 0
    Timer {
        id: openFirstTimer; interval: 900
        onTriggered: win.demoOpenId ? win.openMessage(win.demoOpenId) : bucketView.openHighlighted()
    }

    Connections {
        target: Mailbox
        function onOnlineChanged() { win.loadCounts(); win.loadBucket() }
        // A background change (new mail, a flag flipped) refreshes the list and
        // counts but must not close the reader out from under you.
        function onPushReceived(e, a, b) { win.loadCounts(); win.refreshBucket() }
    }

    // The Feed gets its own scroll-and-expand view; every other bucket is a list.
    readonly property bool feedActive: !win.openId && !launcher.opened && currentKey() === "Feed"
    function navView() { return feedActive ? feedView : bucketView }

    Shortcut { sequences: ["Ctrl+K", "Ctrl+P"]; enabled: !win.composeOpen; onActivated: launcher.toggle() }
    Shortcut {
        sequence: "Escape"
        onActivated: {
            if (quickLook.opened) quickLook.close()
            else if (win.composeOpen) win.composeOpen = false
            else if (launcher.opened) launcher.close()
            else if (win.openId) win.back()
            else if (win.feedActive && feedView.anyOpen()) feedView.collapseAll()
        }
    }
    // Compose a new message. Reply is driven from the reading view.
    Shortcut {
        sequences: ["c", "Ctrl+N"]
        enabled: !win.composeOpen && !launcher.opened && !quickLook.opened
        onActivated: win.startCompose()
    }
    // Number keys switch buckets straight from anywhere (but not mid-compose).
    // No key for the Screener: it is reached only from the Inbox button. The
    // rest keep their order, so Set Aside moves up to 4.
    Shortcut { sequence: "1"; enabled: !win.composeOpen; onActivated: win.switchToKey("INBOX") }
    Shortcut { sequence: "2"; enabled: !win.composeOpen; onActivated: win.switchToKey("Feed") }
    Shortcut { sequence: "3"; enabled: !win.composeOpen; onActivated: win.switchToKey("Paper Trail") }
    Shortcut { sequence: "4"; enabled: !win.composeOpen; onActivated: win.switchToKey("Aside") }
    Shortcut { sequences: ["j", "Down"]; enabled: !win.openId && !launcher.opened && !win.composeOpen; onActivated: win.navView().move(1) }
    Shortcut { sequences: ["k", "Up"]; enabled: !win.openId && !launcher.opened && !win.composeOpen; onActivated: win.navView().move(-1) }
    Shortcut {
        sequences: ["Return", "Enter"]; enabled: !win.openId && !launcher.opened && !win.composeOpen
        onActivated: win.navView().openHighlighted()
    }
    Shortcut {
        sequences: ["o", "l"]; enabled: !win.openId && !launcher.opened && !win.composeOpen
        onActivated: win.feedActive ? feedView.openFull() : bucketView.openHighlighted()
    }
    Shortcut {
        sequences: ["Ctrl+Return", "Ctrl+Enter"]
        enabled: win.composeOpen
        onActivated: composer.doSend()
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
        // No fade: the Feed is opaque and sits above BucketView, so it snaps in
        // to cover the outgoing bucket header while that one fades out beneath.
        opacity: (!win.openId && win.currentKey() === "Feed") ? 1 : 0
        visible: opacity > 0.01
    }

    ReadingView {
        id: readingView
        anchors.fill: parent
        opacity: win.openId ? 1 : 0
        visible: opacity > 0.01
        Behavior on opacity { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
    }

    ComposerView {
        id: composer
        anchors.fill: parent
        opacity: win.composeOpen ? 1 : 0
        visible: opacity > 0.01
        enabled: win.composeOpen
        onRequestClose: win.composeOpen = false
        Behavior on opacity { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }

        // Opaque backdrop so the view underneath never shows through.
        Rectangle {
            anchors.fill: parent
            z: -1
            color: Theme.windowBg
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    CommandLauncher {
        id: launcher
        anchors.fill: parent
    }

    QuickLook {
        id: quickLook
        anchors.fill: parent
    }

    SendUndoToast {
        id: sendToast
        anchors.fill: parent
    }

    // Small transient confirmation ("Draft saved", …).
    Rectangle {
        id: flashLabel
        property alias text: flashText.text
        property bool shown: false
        anchors.horizontalCenter: parent.horizontalCenter
        y: shown ? parent.height - height - 28 : parent.height + 8
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        width: flashText.implicitWidth + 32
        height: 34
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        visible: y < parent.height
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        Text {
            id: flashText
            anchors.centerIn: parent
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Timer { id: flashTimer; interval: 2200; onTriggered: flashLabel.shown = false }
    }
}
