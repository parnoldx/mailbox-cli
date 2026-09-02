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
        { key: "INBOX",       label: "Inbox",       glyph: "\uf01c" },
        { key: "Feed",        label: "The Feed",    glyph: "\uf09e" },
        { key: "Paper Trail", label: "Paper Trail", glyph: "\uf0f6" },
        { key: "Screener",    label: "The Screener",glyph: "\uf0c0" },
        { key: "Aside",       label: "Set Aside",   glyph: "\uf02e" },
        { key: "Reply Later", label: "Reply Later", glyph: "\uf112" },
        { key: "Drafts",      label: "Drafts",      glyph: "\uf044" },
        { key: "Sent",        label: "Sent",        glyph: "\uf1d8" }
    ]

    // Drafts is not `box view` mail: its rows come from `draft list` and a row
    // opens the composer, not the reader.
    function isDraftsBucket() { return currentKey() === "Drafts" }

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
    // The Screener asks for a decision about a *sender*, not a read about a
    // mail, so it carries its own triage (route in / block / move) and hides
    // the reply actions — you screen someone in before you answer them.
    readonly property bool inScreener: currentKey() === "Screener"

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
            subject: win.openMsg.subject || "",
            // The parent's own words, for the quote block the composer keeps
            // folded below the editor and appends on send.
            date: win.openMsg.date || "",
            quote: win.openMsg.body || ""
        })
        win.composeOpen = true
    }
    // Send the open Message on to somebody else. The daemon quotes the
    // original itself, so the composer opens with just an Fwd: subject and an
    // empty note; a recipient is required.
    function startForward() {
        if (!win.openMsg) return
        composer.openForward({ id: win.openMsg.id, subject: win.openMsg.subject || "" })
        win.composeOpen = true
    }
    // ---- drafts ---------------------------------------------------------
    // Re-open a draft in the composer. `draft show` carries the recipients,
    // subject and both body twins the composer needs to fill itself.
    function openDraft(id) {
        if (!id) return
        Mailbox.call(["draft", "show"], { positional: id }, function (r) {
            if (!r.ok || !r.data) { win.flash(win._failMsg("Open draft", r)); return }
            composer.openDraft(r.data)
            win.composeOpen = true
        })
    }
    // Drop a draft straight from its row (the trash icon on a Drafts row),
    // without opening it first.
    function deleteDraft(id) {
        if (!id) return
        Mailbox.call(["draft", "delete"], { positional: id }, function (r) {
            win.flash(r && r.ok ? "Draft discarded" : win._failMsg("Discard draft", r))
            win.refreshBucket()
        })
    }
    // ---- triage actions, by id --------------------------------------------
    // The same four moves drive the reading-view toolbar, the overview's
    // right-click menu and the Command Launcher, so they all take a bare id
    // and clean up after themselves: leave the reader if it is the id being
    // moved, then refresh the list, the counts and the bottom stacks.

    // True when a reader is open on the Thread this id belongs to — the id
    // that opened it is not always its newest Message, so compare against the
    // whole Thread, not just win.openId.
    function _actsOnOpenThread(id) {
        if (!win.openId) return false
        if (id === win.openId) return true
        for (var i = 0; i < win.openThread.length; i++)
            if (win.openThread[i].id === id) return true
        return false
    }

    // What a failed daemon call says: the action, then the reason the daemon
    // gave (a dead server connection, an id that vanished) rather than a bare
    // "failed" that sends you to the CLI to find out what broke.
    function _failMsg(label, r) {
        return label + (r && r.error ? " failed: " + r.error : " failed")
    }

    // Trash a message (and its whole Thread, server-side). The one
    // destructive action.
    function trashId(id) {
        if (!id) return
        if (win._actsOnOpenThread(id)) win.back()
        Mailbox.call(["trash"], { positional: id }, function (r) {
            win.flash(r && r.ok ? "Moved to Trash" : win._failMsg("Trash", r))
            win.loadCounts()
            win.refreshBucket()
            win.refreshStacks()
        })
    }
    // Move a Thread into a hand-tended pile, or — with `done` — back to the
    // Inbox out of one. `pile` is "aside" or "reply-later"; `label` is what
    // the flash says.
    function pileId(pile, label, id, done) {
        if (!id) return
        if (win._actsOnOpenThread(id)) win.back()
        var cmd = done ? [pile, "done"] : [pile]
        Mailbox.call(cmd, { positional: id }, function (r) {
            win.flash(r && r.ok ? label : win._failMsg(label, r))
            win.loadCounts()
            win.refreshBucket()
            win.refreshStacks()
        })
    }
    // A Screener decision. Unlike trash/aside it acts on the *sender*: `route`
    // rewrites the sieve script so their next mail lands in `dest`, and sweeps
    // everything already waiting from that address out of the Screener with it.
    // `dest` is inbox | feed | paper | block; `id` is any Screener message of
    // theirs, which the daemon resolves to the address.
    function routeId(dest, label, id) {
        if (!id) return
        if (win._actsOnOpenThread(id)) win.back()
        Mailbox.call(["route"], { positional: id, to: dest }, function (r) {
            win.flash(r && r.ok ? label : win._failMsg(label, r))
            win.loadCounts()
            win.refreshBucket()
            win.refreshStacks()
        })
    }
    // Move a Thread into a named Box — the Command Launcher's archive picker,
    // where `box` is a short name off `box list --archive` (e.g. "Archive/2019").
    function moveId(id, box, label) {
        if (!id || !box) return
        if (win._actsOnOpenThread(id)) win.back()
        Mailbox.call(["move"], { positional: id, to: box }, function (r) {
            win.flash(r && r.ok ? label : win._failMsg(label, r))
            win.loadCounts()
            win.refreshBucket()
            win.refreshStacks()
        })
    }
    // Every Box the account holds, archive tree included. `cb` gets the raw
    // rows ({ box, count, unseen, ... }); the launcher filters them down.
    function loadArchiveBoxes(cb) {
        Mailbox.call(["box", "list"], { archive: true }, function (r) {
            cb(r.ok && r.data ? r.data : [])
        })
    }

    // Open a Thread and drop straight into a reply to its newest Message —
    // "Reply now" from a row that has not been opened yet.
    property bool _replyOnOpen: false
    function openThenReply(id) {
        if (!id) return
        win._replyOnOpen = true
        win.openMessage(id)
    }

    // Thin wrappers for the reading view, which always acts on the open Thread.
    function trashCurrent() { win.trashId(win.openMsg ? win.openMsg.id : "") }
    function setAsideCurrent() { win.pileId("aside", "Set aside", win.openMsg ? win.openMsg.id : "") }
    function replyLaterCurrent() { win.pileId("reply-later", "Reply later", win.openMsg ? win.openMsg.id : "") }
    function routeCurrent(dest, label) { win.routeId(dest, label, win.openMsg ? win.openMsg.id : "") }

    // The id the Command Launcher's action rows operate on: the open Thread
    // when reading, otherwise the highlighted row of the list bucket.
    function actionTargetId() {
        if (win.openMsg) return win.openMsg.id
        return bucketView.currentRowId()
    }
    // A row asks (right-click) for its triage menu — forwarded to the list
    // view, which owns the one shared RowActions instance.
    function showRowMenu(row) { bucketView.showRowMenu(row) }
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
        // A daemon error is longer and worth a longer read than a one-word
        // confirmation ("Set aside", "Draft saved").
        flashTimer.interval = /failed/.test(text) ? 5000 : 2200
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
        if (win.isDraftsBucket()) {
            Mailbox.call(["draft", "list"], { limit: 200 }, function (r) {
                listModel.setRows(r.ok && r.data ? r.data : [])
            })
            return
        }
        Mailbox.call(["box", "view"], { positional: currentKey(), limit: 200 }, function (r) {
            listModel.setRows(r.ok && r.data ? r.data : [])
        })
    }
    // Re-pull the Drafts list, but only when it is the bucket on screen — the
    // composer calls this after saving, editing or discarding a draft.
    function refreshDrafts() { if (win.isDraftsBucket()) win.refreshBucket() }

    // ---- search -------------------------------------------------------------
    // A full-screen overlay over every bucket. `search` is a ranked full-text
    // query answered from the Mirror across every Box outside Trash; a hit
    // opens in the reader like any other message.
    function openSearch() { searchView.open() }
    function runSearch(q) {
        q = String(q || "").trim()
        if (!q) { searchModel.setRows([]); return }
        Mailbox.call(["search"], { positional: q, limit: 50 }, function (r) {
            searchModel.setRows(r.ok && r.data ? r.data : [])
        })
    }
    function openSearchResult(id) {
        searchView.close()
        win.openMessage(id, true)   // force the reader even from the Drafts bucket
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
    function openMessage(id, forceReader) {
        // A Drafts row opens the composer, not the reader — unless the caller
        // (a search hit) explicitly wants the reader.
        if (win.isDraftsBucket() && !forceReader) { win.openDraft(id); return }
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
                win._replyOnOpen = false
                win.back()
                win.switchToKey("INBOX")
                win.flash("Couldn't find that message")
                return
            }
            win.openThread = thread
            win.openMsg = thread.length ? thread[thread.length - 1] : null
            // "Reply now" opened this only to hand it straight to the composer.
            if (win._replyOnOpen) { win._replyOnOpen = false; win.startReply(false) }
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
        // Let the web-hosting views know when our Wayland surface is unmapped
        // and remapped (a workspace switch) so they can repaint the stale black
        // GPU surface QtWebEngine leaves behind.
        SurfaceWatcher.attach(win)
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

    Shortcut { sequences: ["Ctrl+K", "Ctrl+P"]; enabled: !win.composeOpen && !searchView.opened; onActivated: launcher.toggle() }
    Shortcut {
        sequence: "Escape"
        onActivated: {
            if (quickLook.opened) quickLook.close()
            else if (win.composeOpen) win.composeOpen = false
            else if (searchView.opened) searchView.close()
            else if (launcher.opened) launcher.close()
            else if (win.openId) win.back()
            else if (win.feedActive && feedView.anyOpen()) feedView.collapseAll()
        }
    }
    // Compose a new message. Reply is driven from the reading view.
    Shortcut {
        sequences: ["c", "Ctrl+N"]
        enabled: !win.composeOpen && !launcher.opened && !searchView.opened && !quickLook.opened
        onActivated: win.startCompose()
    }
    // Open the search overlay from anywhere but a compose or a modal. Disabled
    // once it is open so a "/" typed into its own field is text, not a toggle.
    Shortcut {
        sequences: ["/", "Ctrl+F"]
        enabled: !win.composeOpen && !launcher.opened && !quickLook.opened && !searchView.opened
        onActivated: searchView.open()
    }
    // Number keys switch buckets straight from anywhere (but not mid-compose).
    // No key for the Screener: it is reached only from the Inbox button. The
    // rest keep their order: Set Aside 4, Reply Later 5, Drafts 6, Sent 7.
    Shortcut { sequence: "1"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("INBOX") }
    Shortcut { sequence: "2"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("Feed") }
    Shortcut { sequence: "3"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("Paper Trail") }
    Shortcut { sequence: "4"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("Aside") }
    Shortcut { sequence: "5"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("Reply Later") }
    Shortcut { sequence: "6"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("Drafts") }
    Shortcut { sequence: "7"; enabled: !win.composeOpen && !searchView.opened; onActivated: win.switchToKey("Sent") }
    Shortcut { sequences: ["j", "Down"]; enabled: !win.openId && !launcher.opened && !searchView.opened && !win.composeOpen; onActivated: win.navView().move(1) }
    Shortcut { sequences: ["k", "Up"]; enabled: !win.openId && !launcher.opened && !searchView.opened && !win.composeOpen; onActivated: win.navView().move(-1) }
    Shortcut {
        sequences: ["Return", "Enter"]; enabled: !win.openId && !launcher.opened && !searchView.opened && !win.composeOpen
        onActivated: win.navView().openHighlighted()
    }
    Shortcut {
        sequences: ["o", "l"]; enabled: !win.openId && !launcher.opened && !searchView.opened && !win.composeOpen
        onActivated: win.feedActive ? feedView.openFull() : bucketView.openHighlighted()
    }
    // Trash the message you are reading. Matches the widget's T = trash.
    Shortcut {
        sequences: ["t", "Delete"]; enabled: !!win.openId && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.trashCurrent()
    }
    // Triage the message you are reading into a bottom-stack pile: A = set aside,
    // R = reply later. Both drop you back to the list, like Trash. R is off in
    // the Screener — nothing is owed a reply until its sender is screened in.
    Shortcut {
        sequence: "a"; enabled: !!win.openId && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.setAsideCurrent()
    }
    Shortcut {
        sequence: "r"; enabled: !!win.openId && !win.inScreener && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.replyLaterCurrent()
    }
    // Screener decisions on the message you are reading: I = let the sender
    // into the Inbox, B = block them. Both act on the sender and drop you back.
    Shortcut {
        sequence: "i"; enabled: !!win.openId && win.inScreener && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.routeCurrent("inbox", "Let into Inbox")
    }
    Shortcut {
        sequence: "b"; enabled: !!win.openId && win.inScreener && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
        onActivated: win.routeCurrent("block", "Blocked")
    }
    // The same triage keys on the highlighted row of a list bucket — the Feed
    // is a web view and has no row to act on. T = trash, A = set aside,
    // R = reply later, all on the whole Thread the row stands for.
    readonly property bool _rowActionable:
        !win.openId && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
        && !win.feedActive && !win.isDraftsBucket() && bucketView.currentRowId() !== ""
    // T on a Drafts row deletes it outright — a draft is not routed, set aside
    // or trashed to Trash, so it does not go through _rowActionable.
    Shortcut {
        sequences: ["t", "Delete"]
        enabled: !win.openId && !launcher.opened && !searchView.opened && !win.composeOpen && !quickLook.opened
                 && win.isDraftsBucket() && bucketView.currentRowId() !== ""
        onActivated: win.deleteDraft(bucketView.currentRowId())
    }
    Shortcut {
        sequences: ["t", "Delete"]; enabled: win._rowActionable
        onActivated: win.trashId(bucketView.currentRowId())
    }
    Shortcut {
        sequence: "a"; enabled: win._rowActionable
        onActivated: win.pileId("aside", "Set aside", bucketView.currentRowId())
    }
    Shortcut {
        sequence: "r"; enabled: win._rowActionable && !win.inScreener
        onActivated: win.pileId("reply-later", "Reply later", bucketView.currentRowId())
    }
    // Screener decisions on the highlighted row: I = let the sender in,
    // B = block them — the same two one-tap primaries the row menu carries.
    Shortcut {
        sequence: "i"; enabled: win._rowActionable && win.inScreener
        onActivated: win.routeId("inbox", "Let into Inbox", bucketView.currentRowId())
    }
    Shortcut {
        sequence: "b"; enabled: win._rowActionable && win.inScreener
        onActivated: win.routeId("block", "Blocked", bucketView.currentRowId())
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

    SearchView {
        id: searchView
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
        // A short confirmation keeps the tight pill; a long daemon error wraps
        // into a card up to most of the window's width rather than running off
        // both edges.
        width: Math.min(win.width - 64, flashText.implicitWidth + 32)
        height: Math.max(34, flashText.implicitHeight + 16)
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
            width: flashLabel.width - 32
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            maximumLineCount: 4
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Timer { id: flashTimer; interval: 2200; onTriggered: flashLabel.shown = false }
    }
}
