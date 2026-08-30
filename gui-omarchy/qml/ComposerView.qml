import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Dialogs

// Full-screen compose, in the shape of the reading view: a fixed header, a fixed
// action bar, and the form between them. Handles both a fresh message and a
// reply — a reply arrives with its recipient, subject and thread id prefilled.
//
// The body is the Lexxy editor and produces HTML, sent as body_html; the daemon
// keeps a plain-text twin (ADR-0022). Sending is deferred five seconds by
// SendUndoToast, so this view closes the instant Send is pressed and the actual
// send / reply call happens from Main.
Item {
    id: root

    property string mode: "new"          // new | reply
    property string replyId: ""
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
    function _localPath(url) { return decodeURIComponent(String(url).replace(/^file:\/\/\//, "/")) }
    function _stripTags(html) { return String(html || "").replace(/<[^>]*>/g, " ") }

    function resetForm() {
        root.mode = "new"; root.replyId = ""; root.replyAll = false
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

    // ctx: { id, all, from, subject }
    function openReply(ctx) {
        resetForm()
        root.mode = "reply"
        root.replyId = String(ctx.id || "")
        root.replyAll = !!ctx.all
        root.replyFrom = ctx.from || ""
        root.baseSubject = ctx.subject || ""
        var m = String(ctx.from || "").match(/^"?(.*?)"?\s*<([^>]+)>$/)
        if (m) toPills.addRecipient(m[1], m[2])
        else if (ctx.from) toPills.addRecipient("", ctx.from)
        subjectField.text = /^re:/i.test(root.baseSubject) ? root.baseSubject : ("Re: " + root.baseSubject)
        Qt.callLater(function () { lexxy.focusEditor() })
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
        var re = /\b(attached|attachment|attachments|attaching|enclosed|anbei|angeh[aä]ngt|anliegend|beigef[uü]gt)\b/i
        return re.test(root._stripTags(html)) && root.attachments.length === 0
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
        }
        return a
    }
    function sendCmd() { return root.mode === "reply" ? ["reply"] : ["send"] }

    function summaryLabel() {
        var n = toPills.recipients.length
        var who = n === 0 ? "" : (toPills.recipients[0].name || toPills.recipients[0].email)
        if (n > 1) who += " +" + (n - 1)
        return (root.mode === "reply" ? "Reply to " : "Message to ") + who
    }

    function snapshot(html) {
        return {
            mode: root.mode, replyId: root.replyId, replyAll: root.replyAll,
            replyFrom: root.replyFrom, baseSubject: root.baseSubject, showCc: root.showCc,
            to: toPills.recipients.slice(), cc: ccPills.recipients.slice(), bcc: bccPills.recipients.slice(),
            subject: subjectField.text, bodyHtml: html || "", attachments: root.attachments.slice()
        }
    }
    function restore(s) {
        root.mode = s.mode; root.replyId = s.replyId; root.replyAll = s.replyAll
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
            Mailbox.call(["draft", "save"], root.collectArgs(true, html), function (r) {
                win.flash(r.ok ? "Draft saved" : (r.error && r.error.length ? r.error : "Could not save draft"))
            })
            root.requestClose()
        })
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
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: root.mode === "reply" ? "Reply" : "New message"
            font.family: Theme.fontFamily; font.pixelSize: 12; color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
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
            kind: "danger"; glyph: "\uf1f8"; text: ""
            onClicked: root.requestClose()
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
        }

        // Drop files onto the editor to attach them.
        DropArea {
            anchors.fill: parent
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
