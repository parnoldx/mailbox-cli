import QtQuick

Rectangle {
    id: root
    property string name: ""
    property string seed: ""

    width: 34; height: 34; radius: 17
    color: Theme.avatarColor(seed && seed.length ? seed : name)
    Behavior on color { ColorAnimation { duration: Theme.anim } }

    function initials(s) {
        if (!s) return "?"
        var parts = s.trim().split(/\s+/)
        if (parts.length === 1) return parts[0].substring(0, 2).toUpperCase()
        return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase()
    }

    Text {
        anchors.centerIn: parent
        text: root.initials(root.name)
        font.family: Theme.fontFamily
        font.pixelSize: 12
        font.weight: Font.DemiBold
        color: Theme.windowBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
