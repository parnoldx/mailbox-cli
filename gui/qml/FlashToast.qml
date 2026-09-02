import QtQuick

// The small transient confirmation ("Draft saved", "Set aside", …) — or a
// longer daemon error — that drops in from the top edge and slides back out.
// It lives at the top on purpose: the bottom is the Reply Later / Set Aside
// stacks' turf on the Inbox. show(text) is the whole API.
Item {
    id: root
    property bool shown: false

    function show(text) {
        flashText.text = text
        root.shown = true
        // A daemon error is longer and worth a longer read than a one-word
        // confirmation ("Set aside", "Draft saved").
        hideTimer.interval = /failed/.test(text) ? 5000 : 2200
        hideTimer.restart()
    }

    Rectangle {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        y: root.shown ? 28 : -height - 8
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        // A short confirmation keeps the tight pill; a long daemon error wraps
        // into a card up to most of the window's width rather than running off
        // both edges.
        width: Math.min(root.width - 64, flashText.implicitWidth + 32)
        height: Math.max(34, flashText.implicitHeight + 16)
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        visible: y > -height
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        Text {
            id: flashText
            anchors.centerIn: parent
            width: card.width - 32
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            maximumLineCount: 4
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Timer { id: hideTimer; interval: 2200; onTriggered: root.shown = false }
}
