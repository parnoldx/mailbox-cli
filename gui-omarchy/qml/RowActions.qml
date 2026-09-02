import QtQuick
import QtQuick.Controls.Basic

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

    component Rule: MenuSeparator {
        contentItem: Rectangle {
            implicitHeight: 1
            color: Theme.hairline
        }
    }

    Action {
        text: "Open"
        glyph: ""
        onTriggered: win.openMessage(menu.row.id)
    }
    Action {
        text: "Reply now"
        glyph: ""
        visible: !menu.inScreener
        onTriggered: win.openThenReply(menu.row.id)
    }

    Rule {}

    // ---- Screener: a decision about the sender -----------------------------
    Action {
        text: "Let into Inbox"
        glyph: ""
        visible: menu.inScreener
        onTriggered: win.routeId("inbox", "Let into Inbox", menu.row.id)
    }
    Action {
        text: "Block"
        glyph: ""
        danger: true
        visible: menu.inScreener
        onTriggered: win.routeId("block", "Blocked", menu.row.id)
    }
    Action {
        text: "Move to Feed"
        glyph: ""
        visible: menu.inScreener
        onTriggered: win.routeId("feed", "Moved to Feed", menu.row.id)
    }
    Action {
        text: "Move to Paper Trail"
        glyph: ""
        visible: menu.inScreener
        onTriggered: win.routeId("paper", "Moved to Paper Trail", menu.row.id)
    }
    Action {
        text: "Move to Set aside"
        glyph: ""
        visible: menu.inScreener
        onTriggered: win.pileId("aside", "Set aside", menu.row.id)
    }

    // ---- Ordinary bucket: a move of this Thread --------------------------
    Action {
        text: "Move to Inbox"
        glyph: ""
        visible: menu.inAside || menu.inReplyLater
        onTriggered: win.pileId(menu.inAside ? "aside" : "reply-later",
                                "Moved to Inbox", menu.row.id, true)
    }
    Action {
        text: "Reply later"
        glyph: ""
        visible: !menu.inScreener && !menu.inReplyLater
        onTriggered: win.pileId("reply-later", "Reply later", menu.row.id)
    }
    Action {
        text: "Set aside"
        glyph: ""
        visible: !menu.inScreener && !menu.inAside
        onTriggered: win.pileId("aside", "Set aside", menu.row.id)
    }

    Rule {}

    Action {
        text: "Trash"
        glyph: ""
        danger: true
        onTriggered: win.trashId(menu.row.id)
    }
}
