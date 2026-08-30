import QtQuick

Item {
    id: root
    property var row: ({})
    property bool fresh: false
    property bool highlighted: false
    height: 78

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
        Text {
            width: parent.width
            text: root.row.subject || ""
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 14
            font.weight: root.fresh ? Font.DemiBold : Font.Normal
            color: root.fresh ? Theme.textPrimary : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
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
