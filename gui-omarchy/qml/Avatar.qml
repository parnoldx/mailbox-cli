import QtQuick
import "MailFormat.js" as Fmt

Rectangle {
    id: root
    property string name: ""
    property string seed: ""

    width: 34; height: 34; radius: 17
    color: Theme.avatarColor(seed && seed.length ? seed : name)
    Behavior on color { ColorAnimation { duration: Theme.anim } }

    Text {
        anchors.centerIn: parent
        text: Fmt.initials(root.name)
        font.family: Theme.fontFamily
        font.pixelSize: 12
        font.weight: Font.DemiBold
        color: Theme.windowBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
