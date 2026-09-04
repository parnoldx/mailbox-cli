import QtQuick
import QtQuick.Controls.Basic
import "MailFormat.js" as Fmt
import "Triage.js" as Triage

// The reader: a fixed toolbar plus subject, then every Message of the open
// Thread stacked in one scrolling accordion (ThreadMessage.qml) — collapsed
// to a line by default, expanding in place. A Message on its own is a
// one-element Thread, so this is the only shape the reader ever renders.
Item {
    id: root

    function allExpanded() {
        if (win.openThread.length === 0) return false
        for (var i = 0; i < win.openThread.length; i++)
            if (!win.isExpanded(win.openThread[i].id)) return false
        return true
    }

    // The toolbar chips, in order. Reply/Forward/pile chips show only outside
    // the Screener; the Screener swaps in its own decision chips; Trash is
    // always last. Re-evaluated whenever anything it reads changes.
    readonly property var toolbarChips: {
        void win.openMsg; void win.inScreener; void win.openThread
        void win.expandedIds; void PixelBlock.blocked
        var m = !!win.openMsg, scr = win.inScreener, out = []
        var named = (win.openMsg && win.openMsg.trackers) ? win.openMsg.trackers : []
        if (named && named.length)
            out.push({ act: "", interactive: false, glyph: "", glyphColor: Theme.green,
                       label: "Tracked by " + named.join(", ") })
        else if (PixelBlock.blocked > 0)
            out.push({ act: "", interactive: false, glyph: "", glyphColor: Theme.green,
                       label: PixelBlock.blocked + (PixelBlock.blocked === 1 ? " tracker blocked" : " trackers blocked") })
        if (win.openThread.length > 1)
            out.push({ act: "toggle-all",
                       glyph: root.allExpanded() ? "" : "",
                       label: root.allExpanded() ? "Collapse all" : "Expand all" })
        if (m && !scr) {
            out.push({ act: "reply",       glyph: "", label: "Reply",       accentGlyph: true })
            out.push({ act: "reply-all",   glyph: "", label: "Reply all",   accentGlyph: true })
            out.push({ act: "forward",     glyph: "", label: "Forward",     accentGlyph: true })
            out.push({ act: "reply-later", glyph: "", label: "Reply later", accentGlyph: true })
            out.push({ act: "set-aside",   glyph: "", label: "Set aside",   accentGlyph: true })
            out.push({ act: "bubble",      glyph: "", label: "Bubble up",   accentGlyph: true })
        }
        if (m && win.openMsg.invite) {
            out.push({ act: "rsvp-accept",    glyph: "", label: "Accept",    accentGlyph: true })
            out.push({ act: "rsvp-tentative", glyph: "", label: "Maybe",     accentGlyph: true })
            out.push({ act: "rsvp-decline",   glyph: "", label: "Decline",   danger: true })
        }
        if (m && scr) {
            out.push({ act: "route-inbox",   glyph: "", label: "Inbox", accentGlyph: true })
            out.push({ act: "route-block",   glyph: "", label: "Block", danger: true })
            out.push({ act: "screener-move", glyph: "", label: "Move",  accentGlyph: true })
        }
        if (m) out.push({ act: "trash", glyph: "", label: "Trash", danger: true })
        return out
    }

    function fireChip(act, chipItem) {
        var id = win.openMsg ? win.openMsg.id : ""
        if (act === "toggle-all") win.setAllExpanded(!root.allExpanded())
        else if (act === "reply") win.startReply(false)
        else if (act === "reply-all") win.startReply(true)
        else if (act === "forward") win.startForward()
        else if (act === "screener-move") scMoveMenu.popup(chipItem, 0, chipItem.height + 4)
        else if (act === "bubble") bubbleMenu.popup(chipItem, 0, chipItem.height + 4)
        else if (act === "route-inbox") Triage.dispatch(win, "inbox", id)
        else if (act === "route-block") Triage.dispatch(win, "block", id)
        else if (act === "set-aside") Triage.dispatch(win, "aside", id)
        else if (act === "rsvp-accept") win.rsvpId(id, "accept", "Accepted")
        else if (act === "rsvp-tentative") win.rsvpId(id, "tentative", "Tentative")
        else if (act === "rsvp-decline") win.rsvpId(id, "decline", "Declined")
        else Triage.dispatch(win, act, id)   // reply-later | trash
    }

    // ---- Toolbar (does not scroll) ----------------------------------------
    Column {
        id: header
        anchors { top: parent.top; left: parent.left; right: parent.right }
        anchors.leftMargin: Math.max(28, (parent.width - 820) / 2)
        anchors.rightMargin: anchors.leftMargin
        anchors.topMargin: 22
        spacing: 16
        visible: win.openMsg && !win.openLoading

        Row {
            spacing: 10
            Rectangle {
                width: 28; height: 28; radius: 14
                color: backHover.hovered ? Theme.cardHover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Text {
                    anchors.centerIn: parent
                    text: "\uf060"
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                HoverHandler { id: backHover }
                TapHandler { onTapped: win.back() }
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: win.buckets[win.bucketIndex].label
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Kbd { anchors.verticalCenter: parent.verticalCenter; text: "Esc" }

            // Every triage / reply / expand affordance is a Chip; the set and
            // the dispatch live in toolbarChips / fireChip above.
            Repeater {
                model: root.toolbarChips
                Chip {
                    anchors.verticalCenter: parent.verticalCenter
                    glyph: modelData.glyph
                    label: modelData.label
                    interactive: modelData.interactive === undefined ? true : modelData.interactive
                    danger: !!modelData.danger
                    accentGlyph: !!modelData.accentGlyph
                    glyphColor: modelData.glyphColor === undefined ? Theme.textDim : modelData.glyphColor
                    onClicked: root.fireChip(modelData.act, this)
                }
            }
            ScreenerMoveMenu {
                id: scMoveMenu
                targetId: win.openMsg ? win.openMsg.id : ""
            }
            BubbleMenu {
                id: bubbleMenu
                onChosen: function (timing) {
                    win.bubbleId(win.openMsg ? win.openMsg.id : "", timing, "Bubbled up")
                }
            }
        }

        // The Thread's subject, shown once — individual Messages below carry
        // their own sender and date, not another copy of this. The pill after
        // it is the message count, the way the Inbox row that opened this
        // badges the conversation (HEY's trick).
        Row {
            width: parent.width
            spacing: 12
            Text {
                id: subjectText
                width: parent.width - (threadCount.visible ? threadCount.width + parent.spacing : 0)
                text: win.openMsg ? (Fmt.stripSubjectPrefixes(win.openMsg.subject) || win.openMsg.subject || "(no subject)") : ""
                wrapMode: Text.Wrap
                maximumLineCount: 3
                elide: Text.ElideRight
                font.family: Theme.fontFamily
                font.pixelSize: 25
                font.weight: Font.Bold
                lineHeight: 1.18
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Rectangle {
                id: threadCount
                visible: win.openThread.length > 1
                anchors.verticalCenter: parent.verticalCenter
                width: threadCountText.implicitWidth + 16; height: 22; radius: 11
                color: Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Text {
                    id: threadCountText
                    anchors.centerIn: parent
                    text: win.openThread.length
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    font.weight: Font.DemiBold
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
        }

        Rectangle {
            width: parent.width; height: 1
            color: Theme.hairline
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Text {
        anchors.centerIn: parent
        visible: win.openLoading
        text: "opening…"
        font.family: Theme.fontFamily
        font.pixelSize: 12
        color: Theme.textDim
    }

    // ---- The accordion: every Message, oldest first, scrolling as one. ----
    Flickable {
        id: reader
        anchors { top: header.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }
        anchors.topMargin: 14
        anchors.leftMargin: Math.max(28, (root.width - 820) / 2)
        anchors.rightMargin: anchors.leftMargin
        anchors.bottomMargin: 22
        visible: win.openMsg && !win.openLoading && !win.composeOpen
        contentWidth: width
        contentHeight: col.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        Column {
            id: col
            width: parent.width
            spacing: 4
            Repeater {
                model: win.openThread
                ThreadMessage {
                    width: parent.width
                    msg: modelData
                    attachments: win.attachmentsFor(modelData.id)
                    expanded: win.isExpanded(modelData.id)
                    // A one-element Thread has no accordion to share space
                    // with, so its HTML body gets the reader's full height
                    // instead of the fixed sheet size.
                    sole: win.openThread.length === 1
                    viewportHeight: reader.height
                    scroller: reader
                }
            }
        }
    }
}
