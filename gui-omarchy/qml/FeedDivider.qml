import QtQuick

// The "you got to here last time" rule drawn across the feed column.
Item {
    id: root
    property string label: ""
    implicitHeight: 34

    Rectangle {
        anchors.verticalCenter: parent.verticalCenter
        anchors.left: parent.left
        anchors.right: tag.left
        anchors.rightMargin: 12
        height: 1
        color: Theme.accent
        opacity: 0.5
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
    Text {
        id: tag
        anchors.centerIn: parent
        text: root.label.toUpperCase()
        font.family: Theme.fontFamily
        font.pixelSize: 9
        font.weight: Font.DemiBold
        font.letterSpacing: 1.5
        color: Theme.accent
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
    Rectangle {
        anchors.verticalCenter: parent.verticalCenter
        anchors.right: parent.right
        anchors.left: tag.right
        anchors.leftMargin: 12
        height: 1
        color: Theme.accent
        opacity: 0.5
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
