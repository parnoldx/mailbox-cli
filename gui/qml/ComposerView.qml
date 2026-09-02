import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs
import "MailFormat.js" as Fmt

// Full-screen compose, in the shape of the reading view: a fixed header, a fixed
// action bar, and the form between them. Handles a fresh message, a reply, a
// forward and re-opening an existing draft — a reply arrives with its recipient,
// subject and thread id prefilled; a forward with just an `Fwd:` subject and the
// id of the message being sent on; a draft with everything it last held.
//
// The body is the Lexxy editor and produces HTML, sent as body_html; the daemon
// keeps a plain-text twin (ADR-0022). A forward is the exception: the daemon
// quotes the original itself and ignores body_html, so only the plain note is
// sent. Sending is deferred five seconds by SendUndoToast, so this view closes
// the instant Send is pressed and the actual send / reply / forward call happens
// from Main.
Item {
    id: root

    property string mode: "new"          // new | reply | forward | draft
    property string replyId: ""
    property string forwardId: ""
    property string draftId: ""
    property bool replyAll: false
    property string replyFrom: ""
    property string baseSubject: ""
    property bool showCc: false
    property var attachments: []         // [{ name, path }]

    signal requestClose()

    function _addr(r) {
        return (r.name && r.name.length > 0) ? ('"' + r.name + '" <' + r.email + '>') : r.email
    }
    function _base(p) { var s = String(p).split("/"); return s[s.length - 1] || p }
    function _localPath(url) { return Fmt.localPath(url) }
    function _stripTags(html) { return String(html || "").replace(/<[^>]*>/g, " ") }
    function _esc(s) {
        return String(s === undefined || s === null ? "" : s)
            .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    }

    // "On 3 Sep 2026 at 14:03, Jamie Roe <…> wrote:" — the line above the quote.
    function _attribution(from, iso) {
        var who = String(from || "").trim() || "the sender"
        var d = new Date(iso)
        var when = isNaN(d.getTime()) ? "" : Qt.formatDateTime(d, "d MMM yyyy 'at' HH:mm")
        return when ? ("On " + when + ", " + who + " wrote:") : (who + " wrote:")
    }
    // The reply's starting document: an empty line for the answer, then the
    // attribution and the parent's text in a real <blockquote>. Lexxy is
    // Lexical underneath and imports <blockquote> as a first-class node, so
    // this is just editor content the user can trim — nothing is stitched on
    // at send time.
    function _replyDoc(quote, attribution) {
        var paras = String(quote || "").replace(/\r\n?/g, "\n").split(/\n{2,}/)
            .map(function (p) { return "<p>" + p.split("\n").map(root._esc).join("<br>") + "</p>" })
            .join("")
        if (paras.length === 0) paras = "<p></p>"
        return "<p><br></p><p>" + root._esc(attribution) + "</p><blockquote>" + paras + "</blockquote>"
    }

    function resetForm() {
        root.mode = "new"; root.replyId = ""; root.forwardId = ""; root.draftId = ""
        root.replyAll = false
        root.replyFrom = ""; root.baseSubject = ""; root.showCc = false
        root.attachments = []
        toPills.recipients = []; ccPills.recipients = []; bccPills.recipients = []
        subjectField.text = ""
        lexxy.clear()
    }

    function openNew() {
        resetForm()
        Qt.callLater(function () { toPills.focusInput() })
    }

    // ctx: { id, all, from, subject, date, quote }
    function openReply(ctx) {
        resetForm()
        root.mode = "reply"
        root.replyId = String(ctx.id || "")
        root.replyAll = !!ctx.all
        root.replyFrom = ctx.from || ""
        root.baseSubject = ctx.subject || ""
        if (ctx.from) {
            var a = Fmt.parseAddress(ctx.from)
            toPills.addRecipient(a.name, a.addr)
        }
        subjectField.text = /^re:/i.test(root.baseSubject) ? root.baseSubject : ("Re: " + root.baseSubject)
        // Seed the editor with an empty first line and the quoted parent, and
        // land the caret above it (atStart) so the reply is written on top.
        var quote = String(ctx.quote || "").trim()
        if (quote.length > 0)
            lexxy.setHtml(root._replyDoc(quote, root._attribution(ctx.from || "", ctx.date || "")), true)
        else
            Qt.callLater(function () { lexxy.focusStart() })
    }

    // ctx: { id, subject } — a forward carries no recipient (forward needs
    // --to) and no quote: the daemon appends the original whole, from its
    // text/plain, so the editor starts empty for the note.
    function openForward(ctx) {
        resetForm()
        root.mode = "forward"
        root.forwardId = String(ctx.id || "")
        root.baseSubject = ctx.subject || ""
        subjectField.text = /^\s*(fwd?|aw):/i.test(root.baseSubject)
            ? root.baseSubject : ("Fwd: " + root.baseSubject)
        Qt.callLater(function () { toPills.focusInput() })
    }

    // ctx: { to, cc, bcc, subject, body } decoded from a mailto: URI by
    // Main.startMailto. All plain text — the body becomes one paragraph with
    // newlines as <br>. Caret lands in the To row when the link named no
    // recipient, otherwise in the editor.
    function openMailto(ctx) {
        resetForm()
        root.mode = "new"
        root._fillRecipients(toPills, ctx.to || "")
        if (String(ctx.cc || "").length > 0 || String(ctx.bcc || "").length > 0) {
            root.showCc = true
            root._fillRecipients(ccPills, ctx.cc || "")
            root._fillRecipients(bccPills, ctx.bcc || "")
        }
        subjectField.text = ctx.subject || ""
        var body = String(ctx.body || "")
        if (body.length > 0)
            lexxy.setHtml("<p>" + root._esc(body).replace(/\n/g, "<br>") + "</p>", true)
        var haveTo = String(ctx.to || "").length > 0
        Qt.callLater(function () { haveTo ? lexxy.focusStart() : toPills.focusInput() })
    }

    // msg: a `message` object from `draft show` — id, to, subject, body_html,
    // body. Re-opens the draft with everything it last held; Save writes it
    // back in place (draft edit), Send takes it out of the pile (draft send).
    function openDraft(msg) {
        resetForm()
        root.mode = "draft"
        root.draftId = String(msg.id || "")
        root.baseSubject = msg.subject || ""
        subjectField.text = msg.subject || ""
        root._fillRecipients(toPills, msg.to || "")
        if (msg.cc && String(msg.cc).length > 0) {
            root.showCc = true
            root._fillRecipients(ccPills, msg.cc)
        }
        var html = msg.body_html && String(msg.body_html).length > 0
            ? msg.body_html
            : ("<p>" + root._esc(msg.body || "").replace(/\n/g, "<br>") + "</p>")
        lexxy.setHtml(html, true)
        Qt.callLater(function () { toPills.focusInput() })
    }

    // Split a header address string ("Ada <a@x>, b@y") onto a pill row.
    function _fillRecipients(pills, str) {
        var parts = String(str || "").split(/,(?![^<]*>)/)
        for (var i = 0; i < parts.length; i++) {
            var s = parts[i].trim()
            if (!s) continue
            var a = Fmt.parseAddress(s)
            pills.addRecipient(a.name, a.addr)
        }
    }

    function addAttachment(path) {
        path = String(path || "")
        if (!path) return
        for (var i = 0; i < root.attachments.length; i++)
            if (root.attachments[i].path === path) return
        var next = root.attachments.slice()
        next.push({ name: root._base(path), path: path })
        root.attachments = next
    }
    function removeAttachment(i) {
        var next = root.attachments.slice()
        next.splice(i, 1)
        root.attachments = next
    }

    readonly property bool canSend: toPills.recipients.length > 0

    function _mentionsAttachment(html) {
        // Only what the user actually wrote — the quoted parent may well say
        // "see attached" about a file that was never on this reply.
        var own = String(html || "").replace(/<blockquote[\s\S]*?<\/blockquote>/gi, " ")
        var re = /\b(attached|attachment|attachments|attaching|enclosed|anbei|angeh[aä]ngt|anliegend|beigef[uü]gt)\b/i
        return re.test(root._stripTags(own)) && root.attachments.length === 0
    }

    function collectArgs(forDraft, html) {
        var a = {
            to: toPills.recipients.map(root._addr),
            cc: (root.showCc ? ccPills.recipients : []).map(root._addr),
            bcc: (root.showCc ? bccPills.recipients : []).map(root._addr),
            subject: subjectField.text.trim(),
            body_html: html || "",
            attach: root.attachments.map(function (x) { return x.path })
        }
        if (root.mode === "reply" && !forDraft) {
            a.positional = root.replyId
            a.all = root.replyAll
        } else if (root.mode === "forward" && !forDraft) {
            // forward quotes the original server-side from its text/plain and
            // ignores body_html — send only the plain note.
            a.positional = root.forwardId
            a.body = root._stripTags(html).replace(/\s+/g, " ").trim()
            delete a.body_html
        } else if (root.mode === "draft") {
            // edit / send an existing draft in place, not a fresh append.
            a.positional = root.draftId
            // draft edit/send keeps the old plain twin unless `body` is given,
            // so hand it a stripped copy alongside the real HTML.
            a.body = root._stripTags(html).replace(/[ \t]+\n/g, "\n").trim()
        }
        return a
    }
    function sendCmd() {
        if (root.mode === "reply") return ["reply"]
        if (root.mode === "forward") return ["forward"]
        if (root.mode === "draft") return ["draft", "send"]
        return ["send"]
    }

    function summaryLabel() {
        var n = toPills.recipients.length
        var who = n === 0 ? "" : (toPills.recipients[0].name || toPills.recipients[0].email)
        if (n > 1) who += " +" + (n - 1)
        var verb = root.mode === "reply" ? "Reply to "
                 : root.mode === "forward" ? "Forward to "
                 : "Message to "
        return verb + who
    }

    function snapshot(html) {
        return {
            mode: root.mode, replyId: root.replyId, forwardId: root.forwardId,
            draftId: root.draftId, replyAll: root.replyAll,
            replyFrom: root.replyFrom, baseSubject: root.baseSubject, showCc: root.showCc,
            to: toPills.recipients.slice(), cc: ccPills.recipients.slice(), bcc: bccPills.recipients.slice(),
            subject: subjectField.text, bodyHtml: html || "", attachments: root.attachments.slice()
        }
    }
    function restore(s) {
        root.mode = s.mode; root.replyId = s.replyId
        root.forwardId = s.forwardId || ""; root.draftId = s.draftId || ""
        root.replyAll = s.replyAll
        root.replyFrom = s.replyFrom; root.baseSubject = s.baseSubject; root.showCc = s.showCc
        toPills.recipients = s.to; ccPills.recipients = s.cc; bccPills.recipients = s.bcc
        subjectField.text = s.subject; root.attachments = s.attachments
        lexxy.setHtml(s.bodyHtml)
    }

    function doSend() {
        if (!root.canSend) return
        lexxy.getHtml(function (html) {
            win.beginSend({
                cmd: root.sendCmd(),
                args: root.collectArgs(false, html),
                label: root.summaryLabel(),
                warnAttachment: root._mentionsAttachment(html),
                form: root.snapshot(html)
            })
        })
    }
    function doSaveDraft() {
        lexxy.getHtml(function (html) {
            // A re-opened draft is written back in place (edit); a fresh one is
            // a first append (save).
            var cmd = root.mode === "draft" ? ["draft", "edit"] : ["draft", "save"]
            Mailbox.call(cmd, root.collectArgs(true, html), function (r) {
                if (r.ok && r.data && r.data.id) root.draftId = r.data.id
                win.flash(r.ok ? "Draft saved" : Fmt.errText(r, "Could not save draft"))
                if (r.ok) win.refreshDrafts()
            })
            root.requestClose()
        })
    }

    // Drop a re-opened draft from the pile. The action bar's trash button does
    // this in draft mode instead of just closing the view.
    function doDiscardDraft() {
        if (root.mode === "draft" && root.draftId) {
            Mailbox.call(["draft", "delete"], { positional: root.draftId }, function (r) {
                win.flash(r.ok ? "Draft discarded" : Fmt.errText(r, "Could not discard draft"))
                if (r.ok) win.refreshDrafts()
            })
        }
        root.requestClose()
    }

    readonly property real sideMargin: Math.max(28, (width - 820) / 2)

    // ---- Header -----------------------------------------------------------
    Row {
        id: header
        anchors { top: parent.top; left: parent.left; right: parent.right }
        anchors.leftMargin: root.sideMargin
        anchors.rightMargin: root.sideMargin
        anchors.topMargin: 22
        spacing: 10

        Rectangle {
            width: 28; height: 28; radius: 14
            color: backHover.hovered ? Theme.cardHover : Theme.selection
            Behavior on color { ColorAnimation { duration: Theme.anim } }
            Text {
                anchors.centerIn: parent; text: "\uf060"
                font.family: Theme.fontFamily; font.pixelSize: 12
                color: backHover.hovered ? Theme.accent : Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            HoverHandler { id: backHover; cursorShape: Qt.PointingHandCursor }
            TapHandler { onTapped: root.requestClose() }
        }
        Kbd { anchors.verticalCenter: parent.verticalCenter; text: "Esc" }
    }

    // ---- Action bar (fixed, bottom) -------------------------------------
    Rectangle {
        id: actionBar
        anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
        height: 68
        color: Theme.railBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Rectangle {
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: 1; color: Theme.hairline
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Row {
            anchors.verticalCenter: parent.verticalCenter
            anchors.left: parent.left
            anchors.leftMargin: root.sideMargin
            spacing: 10
            AppButton {
                kind: "primary"; glyph: "\uf1d8"; text: "Send message"
                active: root.canSend
                onClicked: root.doSend()
            }
            AppButton { kind: "ghost"; text: "Save draft"; onClicked: root.doSaveDraft() }
            // Attach — a paperclip, right of Save draft.
            AppButton {
                kind: "ghost"; glyph: "\uf0c6"; text: ""
                onClicked: attachDialog.open()
            }
        }
        // Discard — a trash can, far right.
        AppButton {
            anchors.verticalCenter: parent.verticalCenter
            anchors.right: parent.right
            anchors.rightMargin: root.sideMargin
            kind: "danger"; glyph: "\uf1f8"
            text: root.mode === "draft" ? "Discard draft" : ""
            onClicked: root.mode === "draft" ? root.doDiscardDraft() : root.requestClose()
        }
    }

    // ---- Attachment tray (fixed, above the action bar) -----------------
    Flow {
        id: tray
        anchors { bottom: actionBar.top; left: parent.left; right: parent.right }
        anchors.leftMargin: root.sideMargin
        anchors.rightMargin: root.sideMargin
        anchors.bottomMargin: 12
        spacing: 8
        visible: root.attachments.length > 0

        Repeater {
            model: root.attachments
            Rectangle {
                height: 40
                width: Math.min(280, aRow.implicitWidth + 46)
                radius: Theme.radiusSmall
                color: Theme.cardBg
                border.width: 1
                border.color: Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: aRow
                    anchors.verticalCenter: parent.verticalCenter
                    anchors.left: parent.left
                    anchors.leftMargin: 11
                    spacing: 9
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "\uf0c6"
                        font.family: Theme.fontFamily; font.pixelSize: 13; color: Theme.accent
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: modelData.name
                        elide: Text.ElideMiddle
                        width: Math.min(170, implicitWidth)
                        font.family: Theme.fontFamily; font.pixelSize: 11; color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                Rectangle {
                    width: 20; height: 20; radius: 10
                    anchors { right: parent.right; rightMargin: 8; verticalCenter: parent.verticalCenter }
                    color: xHover.hovered ? Theme.red : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Text {
                        anchors.centerIn: parent; text: "\uf00d"
                        font.family: Theme.fontFamily; font.pixelSize: 9
                        color: xHover.hovered ? Theme.onAccent : Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    HoverHandler { id: xHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: root.removeAttachment(index) }
                }
            }
        }
    }

    // ---- Fields (recipients + subject) --------------------------------
    Column {
        id: fields
        anchors { top: header.bottom; left: parent.left; right: parent.right }
        anchors.leftMargin: root.sideMargin
        anchors.rightMargin: root.sideMargin
        anchors.topMargin: 18
        spacing: 12

        // To, with the Cc/Bcc reveal sitting at its right edge.
        Item {
            width: parent.width
            height: Math.max(34, toPills.implicitHeight)
            RecipientPills {
                id: toPills
                label: "To"
                width: parent.width - (root.showCc ? 0 : ccToggle.width + 12)
            }
            Rectangle {
                id: ccToggle
                visible: !root.showCc
                anchors { right: parent.right; top: parent.top }
                width: ccRow.implicitWidth + 18
                height: 26
                radius: 13
                color: ccHover.hovered ? Theme.cardHover : Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: ccRow
                    anchors.centerIn: parent
                    spacing: 6
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "\uf067"
                        font.family: Theme.fontFamily; font.pixelSize: 9
                        color: ccHover.hovered ? Theme.accent : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "Cc / Bcc"
                        font.family: Theme.fontFamily; font.pixelSize: 11
                        color: ccHover.hovered ? Theme.accent : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: ccHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: { root.showCc = true; Qt.callLater(function () { ccPills.focusInput() }) } }
            }
        }

        RecipientPills { id: ccPills; label: "Cc"; width: parent.width; visible: root.showCc }
        RecipientPills { id: bccPills; label: "Bcc"; width: parent.width; visible: root.showCc }

        // Subject — just the field, its placeholder is label enough.
        TextField {
            id: subjectField
            width: parent.width
            placeholderText: "Subject"
            color: Theme.textPrimary
            placeholderTextColor: Theme.textDim
            font.family: Theme.fontFamily
            font.pixelSize: 15
            font.weight: Font.DemiBold
            background: null
            leftPadding: 0
        }

        Rectangle {
            width: parent.width; height: 1; color: Theme.hairline
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    // ---- Body editor (Lexxy, fills the gap) --------------------------
    Rectangle {
        id: editorFrame
        anchors { top: fields.bottom; left: parent.left; right: parent.right
                  bottom: tray.visible ? tray.top : actionBar.top }
        anchors.leftMargin: root.sideMargin
        anchors.rightMargin: root.sideMargin
        anchors.topMargin: 12
        anchors.bottomMargin: 12
        radius: Theme.radiusSmall
        color: Theme.cardBg
        border.width: 1
        border.color: Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        clip: true

        LexxyEditor {
            id: lexxy
            anchors.fill: parent
            anchors.margins: 1
            // A file dropped straight onto the web view (past the DropArea).
            onFileDropped: function (url) { root.addAttachment(root._localPath(url)) }
        }

        // Drop files onto the editor to attach them.
        DropArea {
            anchors.fill: parent
            onEntered: function (drag) { if (drag.hasUrls) drag.accept() }
            onDropped: function (drop) {
                if (!drop.hasUrls) return
                for (var i = 0; i < drop.urls.length; i++)
                    root.addAttachment(root._localPath(drop.urls[i]))
                drop.accept()
            }
            Rectangle {
                anchors.fill: parent
                visible: parent.containsDrag
                color: Qt.rgba(Theme.accent.r, Theme.accent.g, Theme.accent.b, 0.12)
                border.width: 1
                border.color: Theme.accent
                radius: Theme.radiusSmall
            }
        }
    }

    FileDialog {
        id: attachDialog
        fileMode: FileDialog.OpenFiles
        onAccepted: {
            for (var i = 0; i < selectedFiles.length; i++)
                root.addAttachment(root._localPath(selectedFiles[i]))
        }
    }
}
