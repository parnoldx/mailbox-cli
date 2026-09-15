import QtQuick
import "MailFormat.js" as Fmt

Rectangle {
    id: root
    property string name: ""
    property string seed: ""

    // avatarColor() is a Q_INVOKABLE reading C++ members directly, so a plain
    // `color: Theme.avatarColor(...)` binding never hears the theme's changed()
    // signal and stays on the old palette until the delegate is recreated.
    // Naming avatarPalette — a NOTIFY property holding exactly what the colour
    // derives from — is what makes the binding re-tint on a theme swap.
    readonly property string colorKey: seed && seed.length ? seed : name

    width: 34; height: 34; radius: 17
    color: Theme.avatarPalette && Theme.avatarColor(colorKey)
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
