import QtQuick

// One hand-tended pile as a fanned stack: a glyph that jumps to the pile's
// bucket in full, then up to three cards overlapped left-under-right with the
// newest on top. Used twice along the bottom of the Inbox — Reply Later and
// Set Aside — by BottomStacks.qml.
Row {
    id: cl
    property var model
    property string bucketKey: ""
    property string glyph: ""

    readonly property int shown: Math.min(3, model ? model.count : 0)
    // Newest is model row 0; Row paints later children on top, so walk the shown
    // rows back-to-front to leave the newest card fully visible on the right.
    property var cards: (model ? model.count : 0, buildCards())
    function buildCards() {
        var a = []
        for (var i = shown - 1; i >= 0; i--) a.push(model.get(i))
        return a
    }

    visible: shown > 0
    spacing: 8

    Rectangle {
        anchors.verticalCenter: parent.verticalCenter
        width: 34; height: 34; radius: 17
        color: gHover.hovered ? Theme.cardHover : Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        Text {
            anchors.centerIn: parent
            text: cl.glyph
            font.family: Theme.fontFamily
            font.pixelSize: 13
            color: gHover.hovered ? Theme.accent : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        HoverHandler { id: gHover; cursorShape: Qt.PointingHandCursor }
        TapHandler { onTapped: win.switchToKey(cl.bucketKey) }
    }

    // The fanned cards. Negative spacing overlaps them into a stack.
    Row {
        anchors.verticalCenter: parent.verticalCenter
        spacing: -26
        Repeater {
            model: cl.cards
            Rectangle {
                width: 208; height: 52
                radius: Theme.radiusSmall
                color: cHover.hovered ? Theme.cardHover : Theme.cardBg
                border.width: 1
                border.color: Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }

                Avatar {
                    id: av
                    width: 28; height: 28; radius: 14
                    anchors { left: parent.left; leftMargin: 12; verticalCenter: parent.verticalCenter }
                    name: modelData.fromName || ""
                    seed: modelData.fromAddr || ""
                }
                Column {
                    anchors {
                        left: av.right; leftMargin: 10
                        right: parent.right; rightMargin: 12
                        verticalCenter: parent.verticalCenter
                    }
                    spacing: 2
                    Text {
                        width: parent.width
                        text: modelData.subject || "(no subject)"
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        font.weight: Font.DemiBold
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        width: parent.width
                        text: modelData.fromName || ""
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: cHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: win.openMessage(modelData.id) }
            }
        }
    }

    // How many more are in the pile than the three on show.
    Rectangle {
        anchors.verticalCenter: parent.verticalCenter
        visible: cl.model && cl.model.count > cl.shown
        width: moreText.implicitWidth + 16
        height: 20
        radius: 10
        color: Theme.railBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Text {
            id: moreText
            anchors.centerIn: parent
            text: "+" + ((cl.model ? cl.model.count : 0) - cl.shown)
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }
}
