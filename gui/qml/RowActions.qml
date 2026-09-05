import QtQuick
import QtQuick.Controls.Basic
import "Triage.js" as Triage

// The triage menu every row in a list bucket gets on right-click. In an
// ordinary bucket it is the same four moves the reading-view toolbar and the
// Command Launcher offer, acting on the whole Thread the row stands for, and
// context-aware: the pile the current bucket *is* turns into "Move to Inbox"
// rather than a move onto itself.
//
// In the Screener it is a different menu: a decision about the *sender*, not a
// read about a mail. Two one-tap primaries (let in / block), a "Move to…" that
// opens the rest (Feed, Paper Trail, Set aside), and Trash. No reply — you
// screen someone in before you answer them.
Menu {
    id: menu
    property var row: ({})
    property string bucketKey: "INBOX"

    readonly property bool inAside: bucketKey === "Aside"
    readonly property bool inReplyLater: bucketKey === "Reply Later"
    readonly property bool inScreener: bucketKey === "Screener"

    implicitWidth: 208
    topPadding: 6
    bottomPadding: 6

    background: Rectangle {
        implicitWidth: 208
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        radius: Theme.radiusSmall
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    component Action: MenuItem {
        id: mi
        property string glyph: ""
        property bool danger: false
        height: visible ? 34 : 0
        contentItem: Row {
            spacing: 10
            Text {
                anchors.verticalCenter: parent.verticalCenter
                leftPadding: 14
                text: mi.glyph
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: mi.danger ? Theme.red : Theme.textDim
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: mi.text
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: mi.danger ? Theme.red : Theme.textPrimary
            }
        }
        background: Rectangle {
            color: mi.highlighted ? Theme.selection : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    // The row that opens a submenu is created by Controls itself, from this
    // delegate — not by us — so without it the Bubble Up submenu below lands as
    // an unstyled, empty item with a stray arrow floating under "Bubble up".
    // Same look as Action, with the arrow that says it opens onto something.
    delegate: MenuItem {
        id: sub
        height: visible ? 34 : 0
        visible: !menu.inScreener
        contentItem: Row {
            spacing: 10
            Text {
                anchors.verticalCenter: parent.verticalCenter
                leftPadding: 14
                text: ""
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: sub.text
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textPrimary
            }
        }
        arrow: Text {
            anchors.verticalCenter: sub.verticalCenter
            anchors.right: sub.right
            anchors.rightMargin: 12
            visible: sub.subMenu
            text: ""
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textDim
        }
        background: Rectangle {
            color: sub.highlighted ? Theme.selection : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    component Rule: MenuSeparator {
        contentItem: Rectangle {
            implicitHeight: 1
            color: Theme.hairline
        }
    }

    Action {
        text: "Open"
        glyph: ""
        onTriggered: Triage.dispatch(win, "open", menu.row.id)
    }
    Action {
        text: "Reply now"
        glyph: ""
        visible: !menu.inScreener
        onTriggered: Triage.dispatch(win, "reply-now", menu.row.id)
    }

    Rule {}

    // ---- Screener: a decision about the sender -----------------------------
    Action {
        text: "Let into Inbox"
        glyph: ""
        visible: menu.inScreener
        onTriggered: Triage.dispatch(win, "inbox", menu.row.id)
    }
    Action {
        text: "Block"
        glyph: ""
        danger: true
        visible: menu.inScreener
        onTriggered: Triage.dispatch(win, "block", menu.row.id)
    }
    Action {
        text: "Move to Feed"
        glyph: ""
        visible: menu.inScreener
        onTriggered: Triage.dispatch(win, "feed", menu.row.id)
    }
    Action {
        text: "Move to Paper Trail"
        glyph: ""
        visible: menu.inScreener
        onTriggered: Triage.dispatch(win, "paper", menu.row.id)
    }
    Action {
        text: "Move to Set aside"
        glyph: ""
        visible: menu.inScreener
        onTriggered: Triage.dispatch(win, "aside", menu.row.id)
    }

    // ---- Ordinary bucket: a move of this Thread --------------------------
    Action {
        text: "Move to Inbox"
        glyph: ""
        visible: menu.inAside || menu.inReplyLater
        onTriggered: Triage.dispatch(win, menu.inAside ? "aside-done" : "rl-done", menu.row.id)
    }
    Action {
        text: "Reply later"
        glyph: ""
        visible: !menu.inScreener && !menu.inReplyLater
        onTriggered: Triage.dispatch(win, "reply-later", menu.row.id)
    }
    Action {
        text: "Set aside"
        glyph: ""
        visible: !menu.inScreener && !menu.inAside
        onTriggered: Triage.dispatch(win, "aside", menu.row.id)
    }
    // Bubble Up is the one move that asks a second question — when. It opens
    // sideways as a submenu (the delegate above styles the row that opens it)
    // rather than popping a second menu over this one.
    BubbleMenu {
        id: bubbleMenu
        title: "Bubble up"
        onChosen: function (timing) {
            menu.dismiss()
            win.bubbleId(menu.row.id, timing, "Bubbled up")
        }
    }

    Rule {}

    Action {
        text: "Trash"
        glyph: ""
        danger: true
        onTriggered: Triage.dispatch(win, "trash", menu.row.id)
    }
}
