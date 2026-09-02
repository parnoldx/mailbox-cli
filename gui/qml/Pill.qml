import QtQuick

Rectangle {
    id: root
    property int value: 0
    property bool strong: false

    visible: value > 0
    implicitWidth: Math.max(20, label.implicitWidth + 12)
    implicitHeight: 18
    radius: 9
    color: strong ? Theme.accent : Theme.selection
    Behavior on color { ColorAnimation { duration: Theme.anim } }

    Text {
        id: label
        anchors.centerIn: parent
        text: root.value > 99 ? "99+" : root.value
        font.family: Theme.fontFamily
        font.pixelSize: 10
        font.weight: Font.DemiBold
        color: root.strong ? Theme.onAccent : Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
