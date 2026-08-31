import QtQuick

Item {
    id: root
    property var row: ({})
    property bool fresh: false
    property bool highlighted: false
    height: 78

    // A conversation has one subject; the Re:/Fwd:/AW: a client stacks onto
    // every reply is per-message noise. Strip a leading run of them for the
    // row — the raw subject is still what a reply quotes and sends.
    function threadSubject(s) {
        return String(s || "").replace(
            /^\s*(?:(?:re|aw|fwd|fw|wg|antw)(?:\[\d+\])?\s*:\s*)+/i, "").trim()
    }

    Rectangle {
        anchors.fill: parent
        anchors.topMargin: 2
        anchors.bottomMargin: 2
        radius: Theme.radiusSmall
        color: root.highlighted ? Theme.selection
             : hover.hovered ? Theme.cardHover : "transparent"
        border.width: root.highlighted ? 1 : 0
        border.color: Theme.accent
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        Rectangle {
            width: 3; radius: 2
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom
                      topMargin: 16; bottomMargin: 16 }
            color: Theme.accent
            opacity: root.fresh ? 1 : 0
            Behavior on opacity { NumberAnimation { duration: Theme.anim } }
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Avatar {
        id: av
        width: 40; height: 40; radius: 20
        anchors { left: parent.left; leftMargin: 18; verticalCenter: parent.verticalCenter }
        name: root.row.fromName || ""
        seed: root.row.fromAddr || ""
    }

    Column {
        anchors {
            left: av.right; leftMargin: 16
            right: date.left; rightMargin: 16
            verticalCenter: parent.verticalCenter
        }
        spacing: 4
        Text {
            width: parent.width
            text: root.row.fromName || ""
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 13
            font.weight: root.fresh ? Font.Bold : Font.Normal
            color: root.fresh ? Theme.textPrimary : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Row {
            width: parent.width
            spacing: 6
            Text {
                width: parent.width - (countBadge.visible ? countBadge.width + parent.spacing : 0)
                text: root.threadSubject(root.row.subject) || root.row.subject || ""
                elide: Text.ElideRight
                font.family: Theme.fontFamily
                font.pixelSize: 14
                font.weight: root.fresh ? Font.DemiBold : Font.Normal
                color: root.fresh ? Theme.textPrimary : Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            // How many Messages of this Thread are in the box being shown —
            // the daemon already collapsed the listing to this one row.
            Rectangle {
                id: countBadge
                visible: (root.row.count || 0) > 1
                width: countText.implicitWidth + 10; height: 16; radius: 8
                anchors.verticalCenter: parent.verticalCenter
                color: Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Text {
                    id: countText
                    anchors.centerIn: parent
                    text: root.row.count || ""
                    font.family: Theme.fontFamily
                    font.pixelSize: 9
                    font.weight: Font.DemiBold
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
        }
    }

    Text {
        id: date
        anchors { right: parent.right; rightMargin: 6; verticalCenter: parent.verticalCenter }
        text: root.row.date || ""
        font.family: Theme.fontFamily
        font.pixelSize: 11
        color: Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    Rectangle {
        anchors { bottom: parent.bottom; left: av.right; right: parent.right; leftMargin: 16 }
        height: 1
        color: Theme.hairline
        opacity: 0.4
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    HoverHandler { id: hover }
    TapHandler { onTapped: win.openMessage(root.row.id) }
}
