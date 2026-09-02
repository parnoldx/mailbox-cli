import QtQuick

Rectangle {
    id: root
    property string text: ""
    implicitWidth: Math.max(20, label.implicitWidth + 12)
    implicitHeight: 20
    radius: 5
    color: Theme.selection
    border.width: 1
    border.color: Theme.hairline
    Behavior on color { ColorAnimation { duration: Theme.anim } }
    Behavior on border.color { ColorAnimation { duration: Theme.anim } }

    Text {
        id: label
        anchors.centerIn: parent
        text: root.text
        font.family: Theme.fontFamily
        font.pixelSize: 10
        font.weight: Font.DemiBold
        color: Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
