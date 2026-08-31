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
        { key: "Aside",       label: "Set Aside",   glyph: "\uf02e", blurb: "Pulled out to deal with later" },
        { key: "Reply Later", label: "Reply Later", glyph: "\uf112", blurb: "Mail you owe a reply" }
    ]

    property int bucketIndex: 0
    property var counts: ({})
    property string openId: ""      // "" → bucket view, otherwise reading view
    // openMsg is always the newest Message of openThread — the one thread-level
    // actions (Reply, Trash, the subject line) name. It never changes as the
    // accordion below it expands and collapses; that is what expandedIds is for.
    property var openMsg: null
    // Every Message of openMsg's Thread, oldest first — a Message on its own
    // comes back as a one-element Thread, so this is always what backs the
    // reader.
    property var openThread: []
    property bool openLoading: false
    // Which Messages of openThread are expanded, keyed by id, and each one's
    // attachments once fetched — both keyed rather than single-valued because
    // several can be open in the accordion at once.
    property var expandedIds: ({})
    property var attachmentsById: ({})
    function isExpanded(id) { return !!win.expandedIds[id] }
    function attachmentsFor(id) { return win.attachmentsById[id] || [] }

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
    // Trash the open message: fire the daemon call, leave the reader, and
    // refresh the list and counts behind it. The reading view's one
    // destructive action.
    function trashCurrent() {
        if (!win.openMsg) return
        var id = win.openMsg.id
        win.back()
        Mailbox.call(["trash"], { positional: id }, function (r) {
            win.flash(r && r.ok ? "Moved to Trash" : "Trash failed")
            win.loadCounts()
            win.refreshBucket()
        })
    }
    // Put the open message into one of the bottom-stack piles: leave the reader,
    // fire the move, then refresh the list, the counts and the stacks behind it.
    // `pile` is "aside" or "reply-later"; `label` is what the flash says.
    function pileCurrent(pile, label) {
        if (!win.openMsg) return
        var id = win.openMsg.id
        win.back()
        Mailbox.call([pile], { positional: id }, function (r) {
            win.flash(r && r.ok ? label : (label + " failed"))
            win.loadCounts()
            win.refreshBucket()
            win.refreshStacks()
        })
    }
    function setAsideCurrent() { pileCurrent("aside", "Set aside") }
    function replyLaterCurrent() { pileCurrent("reply-later", "Reply later") }
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

    // The two hand-tended piles shown as stacks along the bottom of the Inbox.
    // Kept fresh alongside the Inbox list and after anything that moves mail in
    // or out of them.
    function refreshStacks() {
        Mailbox.call(["box", "view"], { positional: "Reply Later", limit: 12 }, function (r) {
            replyLaterModel.setRows(r.ok && r.data ? r.data : [])
        })
        Mailbox.call(["box", "view"], { positional: "Aside", limit: 12 }, function (r) {
            asideModel.setRows(r.ok && r.data ? r.data : [])
        })
    }

    function loadBucket() {
        win.openId = ""
        win.openMsg = null
        refreshBucket()
        refreshStacks()
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

    // `thread view` reads the whole conversation the id belongs to, oldest
    // first — a Message on its own comes back as a one-element Thread, so this
    // is the one call the reader ever needs to open anything. Opens with the
    // newest Message expanded, plus any Message still unread — everything
    // already read starts collapsed.
    function openMessage(id) {
        win.openId = id
        win.openMsg = null
        win.openThread = []
        win.expandedIds = {}
        win.attachmentsById = {}
        win.openLoading = true
        PixelBlock.reset()
        Mailbox.call(["thread", "view"], { positional: id }, function (r) {
            win.openLoading = false
            if (win.openId !== id) return
            var thread = (r.ok && r.data) ? r.data : []
            // Bad id, or the message vanished (trashed elsewhere, a stale deep
            // link) between the click and the reply — don't leave the reader
            // sitting on a blank window. Bounce to the Inbox and say why, the
            // same way trashCurrent() reports its outcome.
            if (thread.length === 0) {
                win.back()
                win.switchToKey("INBOX")
                win.flash("Couldn't find that message")
                return
            }
            win.openThread = thread
            win.openMsg = thread.length ? thread[thread.length - 1] : null
            var open = {}
            for (var i = 0; i < thread.length; i++)
                if (i === thread.length - 1 || thread[i].seen === false) open[thread[i].id] = true
            win.expandedIds = open
            // The id that opened it is not always the newest — a search hit or
            // a deep link can name any Message of the conversation — that one
            // gets the QuickLook demo treatment (--ql) if it carries an image.
            for (var eid in open) win._loadExpanded(eid, eid === id)
        })
    }

    // Fetches attachments (once) and marks read a Message that just became
    // expanded — what opening used to do for the single message, now done per
    // accordion row instead of once for the whole reader.
    function _loadExpanded(id, allowQuickLookDemo) {
        var msg = null
        for (var i = 0; i < win.openThread.length; i++)
            if (win.openThread[i].id === id) { msg = win.openThread[i]; break }
        if (!msg) return
        if (win.attachmentsById[id] === undefined) {
            Mailbox.call(["attachment", "list"], { positional: id }, function (r) {
                var list = (r.ok && r.data) ? r.data : []
                var m = Object.assign({}, win.attachmentsById)
                m[id] = list
                win.attachmentsById = m
                if (allowQuickLookDemo && win._demoQl && list.length > 0) {
                    win._demoQl = false
                    win.openQuickLook(list[0])
                }
            })
        }
        if (msg.seen === false) {
            Mailbox.call(["seen"], { positional: id }, function () {
                win.loadCounts()
                win.refreshBucket()
            })
        }
    }

    // What a click on an accordion row does: expand it in place if collapsed,
    // collapse it if already open. Nothing else in the Thread is disturbed.
    function toggleExpanded(id) {
        var m = Object.assign({}, win.expandedIds)
        if (m[id]) {
            delete m[id]
            win.expandedIds = m
            return
        }
        m[id] = true
        win.expandedIds = m
        win._loadExpanded(id, false)
    }

    // The up/down icon: expand or collapse every Message of the open Thread
    // at once.
    function setAllExpanded(state) {
        if (!state) { win.expandedIds = {}; return }
        var m = {}
        for (var i = 0; i < win.openThread.length; i++) {
            m[win.openThread[i].id] = true
            win._loadExpanded(win.openThread[i].id, false)
        }
        win.expandedIds = m
    }

    function back() {
        win.openId = ""; win.openMsg = null; win.openThread = []
        win.expandedIds = {}; win.attachmentsById = {}
    }

    function openQuickLook(att) { quickLook.openFor(att) }

    // `--open <id>`: raise the reader shell immediately so the bucket never
    // flashes behind it, then load the message the moment the daemon socket is
    // up (onlineChanged) — or after a short grace, so an offline start still
    // lands on the canned demo instead of hanging on "opening…".
    property string pendingOpenId: ""
    function openWhenReady(id) {
        win.openId = id
        win.openMsg = null
        win.openLoading = true
        win.expandedIds = {}
        win.attachmentsById = {}
        if (Mailbox.online) { win.openMessage(id); return }
        win.pendingOpenId = id
        openWaitTimer.restart()
    }
    function flushPendingOpen() {
        if (!win.pendingOpenId) return
        var id = win.pendingOpenId
        win.pendingOpenId = ""
        openWaitTimer.stop()
        win.openMessage(id)
    }
    Timer { id: openWaitTimer; interval: 1500; onTriggered: win.flushPendingOpen() }

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
        if (oi >= 0 && oi + 1 < a.length) openWhenReady(a[oi + 1])
        else if (a.indexOf("--open-first") >= 0) openFirstTimer.start()
        if (a.indexOf("--compose") >= 0) composeTimer.start()
    }
    Timer { id: composeTimer; interval: 700; onTriggered: win.startCompose() }
    property bool _demoQl: Qt.application.arguments.indexOf("--ql") >= 0
    // --open-first (demo/screenshot): open whatever row lands highlighted once
    // the first bucket load has settled.
    Timer {
        id: openFirstTimer; interval: 900
        onTriggered: bucketView.openHighlighted()
    }

    Connections {
        target: Mailbox
        function onOnlineChanged() {
            win.loadCounts()
            // Don't reset the reader if one is open (reconnect mid-read, or the
            // socket coming up just after --open raised it).
            if (win.openId) win.refreshBucket()
            else win.loadBucket()
            win.refreshStacks()
            if (Mailbox.online) win.flushPendingOpen()
        }
        // A background change (new mail, a flag flipped) refreshes the list, the
        // counts and the bottom stacks but must not close the reader.
        function onPushReceived(e, a, b) { win.loadCounts(); win.refreshBucket(); win.refreshStacks() }
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
    // rest keep their order, so Set Aside is 4 and Reply Later 5.
    Shortcut { sequence: "1"; enabled: !win.composeOpen; onActivated: win.switchToKey("INBOX") }
    Shortcut { sequence: "2"; enabled: !win.composeOpen; onActivated: win.switchToKey("Feed") }
    Shortcut { sequence: "3"; enabled: !win.composeOpen; onActivated: win.switchToKey("Paper Trail") }
    Shortcut { sequence: "4"; enabled: !win.composeOpen; onActivated: win.switchToKey("Aside") }
    Shortcut { sequence: "5"; enabled: !win.composeOpen; onActivated: win.switchToKey("Reply Later") }
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
    // Trash the message you are reading. Matches the widget's T = trash.
    Shortcut {
        sequences: ["t", "Delete"]; enabled: !!win.openId && !launcher.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.trashCurrent()
    }
    // Triage the message you are reading into a bottom-stack pile: A = set aside,
    // R = reply later. Both drop you back to the list, like Trash.
    Shortcut {
        sequence: "a"; enabled: !!win.openId && !launcher.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.setAsideCurrent()
    }
    Shortcut {
        sequence: "r"; enabled: !!win.openId && !launcher.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.replyLaterCurrent()
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

    // Small transient confirmation ("Draft saved", …). Lives at the top —
    // the bottom is the Reply Later / Set Aside stacks' turf on the Inbox,
    // and this used to land right on top of them.
    Rectangle {
        id: flashLabel
        property alias text: flashText.text
        property bool shown: false
        anchors.horizontalCenter: parent.horizontalCenter
        y: shown ? 28 : -height - 8
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        width: flashText.implicitWidth + 32
        height: 34
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        visible: y > -height
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
