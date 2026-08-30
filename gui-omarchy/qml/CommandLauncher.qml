import QtQuick
import QtQuick.Controls.Basic

// The HEY quick switcher: search focused immediately, numbered destinations,
// arrow keys or digits to pick.
Item {
    id: root
    property bool opened: false
    property int active: 0

    function open() {
        query.text = ""
        active = win.bucketIndex
        opened = true
        Qt.callLater(function () { query.forceActiveFocus() })
    }
    function close() { opened = false }
    function toggle() { opened ? close() : open() }

    function results() {
        var q = query.text.trim().toLowerCase()
        var out = []
        for (var i = 0; i < win.buckets.length; i++) {
            var b = win.buckets[i]
            if (!q || b.label.toLowerCase().indexOf(q) >= 0 || b.blurb.toLowerCase().indexOf(q) >= 0)
                out.push({ i: i, b: b })
        }
        return out
    }
    property var rows: (query.text, opened, results())

    function choose(listPos) {
        var r = rows[listPos]
        if (r) win.switchTo(r.i)
    }

    visible: opened || scrim.opacity > 0.01

    // Scrim
    Rectangle {
        id: scrim
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.55)
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        TapHandler { onTapped: root.close() }
    }

    // Card
    Rectangle {
        id: card
        width: Math.min(560, root.width - 80)
        anchors.horizontalCenter: parent.horizontalCenter
        y: root.opened ? root.height * 0.16 : root.height * 0.16 - 12
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        height: content.implicitHeight + 24
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        Column {
            id: content
            width: parent.width
            padding: 12
            spacing: 8

            // Search field
            Rectangle {
                width: parent.width - 24
                height: 42
                radius: Theme.radiusSmall
                color: Theme.windowBg
                border.width: 1
                border.color: query.activeFocus ? Theme.accent : Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }

                Row {
                    anchors.fill: parent
                    anchors.leftMargin: 14
                    anchors.rightMargin: 14
                    spacing: 10
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    TextField {
                        id: query
                        width: parent.width - 34
                        anchors.verticalCenter: parent.verticalCenter
                        placeholderText: "Jump to…"
                        color: Theme.textPrimary
                        placeholderTextColor: Theme.textDim
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        background: null
                        leftPadding: 0
                        onTextChanged: root.active = 0
                        Keys.onDownPressed: root.active = Math.min(root.rows.length - 1, root.active + 1)
                        Keys.onUpPressed: root.active = Math.max(0, root.active - 1)
                        Keys.onReturnPressed: root.choose(root.active)
                        Keys.onEscapePressed: root.close()
                        Keys.onPressed: function (e) {
                            if (e.text.length === 1 && e.text >= "1" && e.text <= "9") {
                                var n = parseInt(e.text) - 1
                                if (n < root.rows.length) { root.choose(n); e.accepted = true }
                            }
                        }
                    }
                }
            }

            // Destinations
            Repeater {
                model: root.rows
                Rectangle {
                    width: content.width - 24
                    height: 46
                    radius: Theme.radiusSmall
                    color: index === root.active ? Theme.selection
                         : itemHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }

                    Row {
                        anchors.fill: parent
                        anchors.leftMargin: 12
                        anchors.rightMargin: 12
                        spacing: 12

                        Kbd {
                            anchors.verticalCenter: parent.verticalCenter
                            text: (index + 1).toString()
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData.b.glyph
                            font.family: Theme.fontFamily
                            font.pixelSize: 14
                            color: index === root.active ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Column {
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 2
                            Text {
                                text: modelData.b.label
                                font.family: Theme.fontFamily
                                font.pixelSize: 13
                                font.weight: index === root.active ? Font.DemiBold : Font.Normal
                                color: Theme.textPrimary
                                Behavior on color { ColorAnimation { duration: Theme.anim } }
                            }
                        }
                    }

                    // Live count on the right.
                    Pill {
                        anchors { verticalCenter: parent.verticalCenter; right: parent.right; rightMargin: 14 }
                        value: {
                            var key = modelData.b.key
                            var c = win.counts[key]
                            if (!c) return 0
                            return (key === "INBOX" || key === "Screener") ? (c.unseen || c.count) : c.count
                        }
                        strong: modelData.b.key === "INBOX" || modelData.b.key === "Screener"
                    }

                    HoverHandler { id: itemHover }
                    TapHandler { onTapped: root.choose(index) }
                }
            }
        }
    }
}
