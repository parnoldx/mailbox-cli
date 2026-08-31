import QtQuick
import QtQuick.Controls.Basic

// The reader: a fixed toolbar plus subject, then every Message of the open
// Thread stacked in one scrolling accordion (ThreadMessage.qml) — collapsed
// to a line by default, expanding in place. A Message on its own is a
// one-element Thread, so this is the only shape the reader ever renders.
Item {
    id: root

    // A thread has one subject; the Re:/Fwd:/AW: a client stacks onto every
    // reply is per-message noise, so strip a leading run of them for the
    // heading. The raw subject on each Message is untouched.
    function threadSubject(s) {
        return String(s || "").replace(
            /^\s*(?:(?:re|aw|fwd|fw|wg|antw)(?:\[\d+\])?\s*:\s*)+/i, "").trim()
    }

    function allExpanded() {
        if (win.openThread.length === 0) return false
        for (var i = 0; i < win.openThread.length; i++)
            if (!win.isExpanded(win.openThread[i].id)) return false
        return true
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
                    text: ""
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

            // Trackers blocked across every Message rendered since this Thread
            // opened — a per-message count would just be noise at this scale.
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                visible: PixelBlock.blocked > 0
                width: badge.implicitWidth + 18
                height: 20
                radius: 10
                color: Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: badge
                    anchors.centerIn: parent
                    spacing: 5
                    Text {
                        text: ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.green
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: PixelBlock.blocked + (PixelBlock.blocked === 1 ? " tracker blocked" : " trackers blocked")
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
            }

            // Expand or collapse every Message of the Thread at once.
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                visible: win.openThread.length > 1
                width: exAllRow.implicitWidth + 18
                height: 20
                radius: 10
                color: exAllHover.hovered ? Theme.cardHover : Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: exAllRow
                    anchors.centerIn: parent
                    spacing: 5
                    Text {
                        text: root.allExpanded() ? "" : ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: root.allExpanded() ? "Collapse all" : "Expand all"
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: exAllHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: win.setAllExpanded(!root.allExpanded()) }
            }

            // Reply / Reply all — always to the newest Message, the way a
            // reply within a thread view names the latest word in it.
            Repeater {
                model: [ { label: "Reply", all: false }, { label: "Reply all", all: true } ]
                Rectangle {
                    anchors.verticalCenter: parent.verticalCenter
                    visible: !!win.openMsg
                    width: rpRow.implicitWidth + 18
                    height: 20
                    radius: 10
                    color: rpHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        id: rpRow
                        anchors.centerIn: parent
                        spacing: 5
                        Text {
                            text: modelData.all ? "" : ""
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: rpHover.hovered ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            text: modelData.label
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: rpHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: win.startReply(modelData.all) }
                }
            }

            // Triage into a bottom-stack pile: Reply later (key R) or Set aside
            // (key A). Both drop back to the list, then fire the move.
            Repeater {
                model: [
                    { glyph: "", label: "Reply later", fn: "replyLaterCurrent" },
                    { glyph: "", label: "Set aside",   fn: "setAsideCurrent" }
                ]
                Rectangle {
                    anchors.verticalCenter: parent.verticalCenter
                    visible: !!win.openMsg
                    width: plRow.implicitWidth + 18
                    height: 20
                    radius: 10
                    color: plHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        id: plRow
                        anchors.centerIn: parent
                        spacing: 5
                        Text {
                            text: modelData.glyph
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: plHover.hovered ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            text: modelData.label
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: plHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: win[modelData.fn]() }
                }
            }

            // Trash — acts on the whole Thread server-side (any id in it
            // does), not just the newest Message. Drops back to the list.
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                visible: !!win.openMsg
                width: trRow.implicitWidth + 18
                height: 20
                radius: 10
                color: trHover.hovered ? Theme.red : Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: trRow
                    anchors.centerIn: parent
                    spacing: 5
                    Text {
                        text: ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: trHover.hovered ? "#ffffff" : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: "Trash"
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: trHover.hovered ? "#ffffff" : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: trHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: win.trashCurrent() }
            }
        }

        // The Thread's subject, shown once — individual Messages below carry
        // their own sender and date, not another copy of this.
        Text {
            width: parent.width
            text: win.openMsg ? (root.threadSubject(win.openMsg.subject) || win.openMsg.subject || "(no subject)") : ""
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
                }
            }
        }
    }
}
