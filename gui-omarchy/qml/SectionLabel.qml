import QtQuick

Row {
    id: root
    property string text: ""
    property int count: 0
    height: 30
    spacing: 10

    Text {
        anchors.verticalCenter: parent.verticalCenter
        text: root.text.toUpperCase()
        font.family: Theme.fontFamily
        font.pixelSize: 10
        font.weight: Font.DemiBold
        font.letterSpacing: 1.5
        color: Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
    Rectangle {
        anchors.verticalCenter: parent.verticalCenter
        width: 16; height: 16; radius: 8
        color: Theme.selection
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Text {
            anchors.centerIn: parent
            text: root.count
            font.family: Theme.fontFamily
            font.pixelSize: 9
            font.weight: Font.DemiBold
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }
}
