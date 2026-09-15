import QtQuick

// The labels a Thread carries, as small tinted pills. The colour is the label's
// name run through the same hash the avatars use, so a label keeps its colour
// everywhere without anybody choosing one and it follows the Omarchy theme.
Row {
    id: root
    property var labels: []
    spacing: 6
    visible: root.labels && root.labels.length > 0

    Repeater {
        model: root.labels
        Rectangle {
            height: 16
            width: pillText.implicitWidth + 14
            radius: 8
            // avatarColor() is a Q_INVOKABLE with no NOTIFY of its own — reading
            // the avatarPalette property is what ties these colours to a theme
            // change (same trick as Avatar.qml).
            readonly property color tint: Theme.avatarPalette && Theme.avatarColor(modelData)
            color: Qt.rgba(tint.r, tint.g, tint.b, 0.18)
            Behavior on color { ColorAnimation { duration: Theme.anim } }
            Text {
                id: pillText
                anchors.centerIn: parent
                text: modelData
                font.family: Theme.fontFamily
                font.pixelSize: 10
                font.weight: Font.DemiBold
                color: parent.tint
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
        }
    }
}
