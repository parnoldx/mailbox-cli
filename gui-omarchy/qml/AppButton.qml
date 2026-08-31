import QtQuick

// One button, four weights. Primary is an accent pill, ghost is a hairline
// outline, soft is a filled selection chip with an accent label (a coloured but
// secondary action), danger tints its label red. Matches the radii and animation
// the rest of the window uses.
Rectangle {
    id: root
    property string text: ""
    property string glyph: ""
    property string kind: "ghost"      // primary | ghost | soft | danger
    property bool active: true
    signal clicked()

    readonly property bool primary: kind === "primary"
    readonly property bool danger: kind === "danger"
    readonly property bool soft: kind === "soft"

    implicitWidth: row.implicitWidth + 34
    implicitHeight: 38
    radius: Theme.radius
    opacity: active ? 1 : 0.4

    color: primary ? (hover.hovered ? Qt.lighter(Theme.accent, 1.12) : Theme.accent)
                   : soft ? (hover.hovered ? Theme.cardHover : Theme.selection)
                   : hover.hovered ? Theme.cardHover : "transparent"
    border.width: (primary || soft) ? 0 : 1
    border.color: danger ? Theme.red : Theme.hairline
    Behavior on color { ColorAnimation { duration: Theme.anim } }
    Behavior on border.color { ColorAnimation { duration: Theme.anim } }
    Behavior on opacity { NumberAnimation { duration: Theme.anim } }

    Row {
        id: row
        anchors.centerIn: parent
        spacing: 8
        Text {
            visible: root.glyph.length > 0
            anchors.verticalCenter: parent.verticalCenter
            text: root.glyph
            font.family: Theme.fontFamily
            font.pixelSize: 13
            color: root.primary ? Theme.onAccent : root.danger ? Theme.red
                   : root.soft ? Theme.accent : Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Text {
            visible: root.text.length > 0
            anchors.verticalCenter: parent.verticalCenter
            text: root.text
            font.family: Theme.fontFamily
            font.pixelSize: 13
            font.weight: root.primary ? Font.DemiBold : Font.Normal
            color: root.primary ? Theme.onAccent : root.danger ? Theme.red
                   : root.soft ? Theme.accent : Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    HoverHandler { id: hover; enabled: root.active; cursorShape: Qt.PointingHandCursor }
    TapHandler { enabled: root.active; onTapped: root.clicked() }
}
