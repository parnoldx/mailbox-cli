import QtQuick

// One hand-tended pile as a card stack: a glyph that jumps to the pile's
// bucket in full, then up to three cards stacked newest-on-top, each older
// one peeking out just a few pixels behind. Used twice along the bottom of
// the Inbox — Reply Later and Set Aside — by BottomStacks.qml.
Row {
    id: cl
    property var model
    property string bucketKey: ""
    property string glyph: ""

    readonly property int shown: Math.min(3, model ? model.count : 0)
    // Newest is model row 0; children painted later sit on top, so hand the
    // Repeater the shown rows oldest-first (newest last, flush at the front).
    readonly property var cards: model ? model.rows.slice(0, shown).reverse() : []

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

    // The stacked cards: the newest sits in front at full size and is the
    // only one actually readable; each older one behind it peeks out by just
    // `peek` pixels, bottom-right, like a real stack of cards rather than a
    // row of them. You cannot tell which message a sliver belongs to, so
    // tapping one jumps to the pile's bucket instead of guessing — same as
    // tapping the glyph. Only the front card opens its own message.
    readonly property int peek: 9
    Item {
        anchors.verticalCenter: parent.verticalCenter
        width: 208 + (cl.shown - 1) * cl.peek
        height: 52 + (cl.shown - 1) * cl.peek
        Repeater {
            model: cl.cards
            Rectangle {
                // cl.cards is built oldest-of-the-shown first, newest last, so
                // the Repeater paints later indices on top — backness 0 is
                // the newest, drawn last, flush at the front.
                readonly property int backness: cl.cards.length - 1 - index
                readonly property bool front: backness === 0
                x: backness * cl.peek
                y: backness * cl.peek
                width: 208; height: 52
                radius: Theme.radiusSmall
                color: cHover.hovered && front ? Theme.cardHover : Theme.cardBg
                opacity: front ? 1 : Math.max(0.5, 1 - backness * 0.25)
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
                TapHandler { onTapped: front ? win.openMessage(modelData.id) : win.switchToKey(cl.bucketKey) }
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
