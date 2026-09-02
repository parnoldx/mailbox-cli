import QtQuick
import QtQuick.Controls.Basic

// The "Move" step of a Screener decision — the destinations that are not
// one-tap primaries. Block and "let into the Inbox" are the two things you do
// most, so they stay on the surface; Feed, Paper Trail and Set aside live
// behind this. Shared by the row menu (RowActions) and the reading-view
// toolbar so both offer exactly the same set.
Menu {
    id: menu

    // The Screener message the decision acts on. Feed/Paper Trail are routes
    // (the sender), Set aside is a pile move (this thread) — see win.routeId /
    // win.pileId.
    property string targetId: ""

    implicitWidth: 200
    topPadding: 6
    bottomPadding: 6

    background: Rectangle {
        implicitWidth: 200
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        radius: Theme.radiusSmall
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    component Dest: MenuItem {
        id: mi
        property string glyph: ""
        height: 34
        contentItem: Row {
            spacing: 10
            Text {
                anchors.verticalCenter: parent.verticalCenter
                leftPadding: 14
                text: mi.glyph
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
            }
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: mi.text
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textPrimary
            }
        }
        background: Rectangle {
            color: mi.highlighted ? Theme.selection : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Dest {
        text: "Feed"
        glyph: ""
        onTriggered: win.routeId("feed", "Moved to Feed", menu.targetId)
    }
    Dest {
        text: "Paper Trail"
        glyph: ""
        onTriggered: win.routeId("paper", "Moved to Paper Trail", menu.targetId)
    }
    Dest {
        text: "Set aside"
        glyph: ""
        onTriggered: win.pileId("aside", "Set aside", menu.targetId)
    }
}
