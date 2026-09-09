import QtQuick
import QtQuick.Controls.Basic
import "Triage.js" as Triage

// The reader's overflow menu: Forward / Label / Move / Trash — the actions
// that no longer fit as chips beside Reply. Opened by the "More" chip or the
// M key (Main.qml). Same shell as ScreenerMoveMenu; each row also shows the
// shortcut letter that fires it straight from the reader.
Menu {
    id: menu

    // The open Message the actions act on.
    property string targetId: ""

    implicitWidth: 210
    topPadding: 6
    bottomPadding: 6

    background: Rectangle {
        implicitWidth: 210
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        radius: Theme.radiusSmall
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    component Act: MenuItem {
        id: mi
        property string glyph: ""
        property string kbd: ""
        property bool danger: false
        height: 34
        contentItem: Item {
            Row {
                anchors.verticalCenter: parent.verticalCenter
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
            Text {
                anchors.verticalCenter: parent.verticalCenter
                anchors.right: parent.right
                anchors.rightMargin: 14
                text: mi.kbd
                font.family: Theme.fontFamily
                font.pixelSize: 10
                font.weight: Font.DemiBold
                opacity: 0.75
                color: mi.danger ? Theme.red : Theme.textDim
            }
        }
        background: Rectangle {
            color: mi.highlighted ? Theme.selection : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Act {
        text: "Forward"; glyph: "\uf064"; kbd: "F"
        onTriggered: win.startForward()
    }
    Act {
        text: "Label"; glyph: "\uf02c"; kbd: "B"
        onTriggered: win.openLabelPicker(menu.targetId)
    }
    Act {
        text: "Move"; glyph: "\uf0b2"; kbd: "V"
        onTriggered: win.openMovePicker()
    }

    MenuSeparator {
        contentItem: Rectangle { implicitHeight: 1; color: Theme.hairline }
    }

    Act {
        text: "Trash"; glyph: "\uf1f8"; kbd: "T"; danger: true
        onTriggered: Triage.dispatch(win, "trash", menu.targetId)
    }
}
