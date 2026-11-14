import QtQuick
import QtQuick.Controls.Basic

// One type-to-filter field: the rounded shell, the glyph in front of it and the input
// itself, with the border following focus. Everything the window does with a
// field is the same, so the shell is written once and the caller keeps only what
// is its own: what the keys mean, and what happens when the text changes.
//
// The callers name their instance `query`, so `query.text`,
// `query.forceActiveFocus()` and `query.focus = false` read the way they did
// when the TextField itself carried that id.

Item {
    id: root

    // icon is the glyph before the input. The search glass unless the field is
    // picking something out (a Box, a label) rather than searching.
    property string icon: "\uf002"
    property string placeholder: ""
    // background is the shell's fill: the reading pane's window colour by
    // default, the rail's when the field sits in a rail.
    property color background: Theme.windowBg
    // accented says the border wears the accent. Focus by default, because
    // typing is what accents; a field whose focus is mirrored somewhere else
    // (BucketView keeps it in win.archiveBoxOpen) overrides it.
    property bool accented: textInput.activeFocus
    // editFocus follows the input itself, which Item.activeFocus does not: that
    // one is false as soon as a child holds the focus.
    readonly property bool editFocus: textInput.activeFocus

    property alias text: textInput.text
    // input is the TextField itself, for the two things a field cannot say in
    // a property: selectAll() and a forced focus.
    property alias input: textInput

    implicitHeight: 42

    // A focus proxy: an Item cannot be typed into, so a caller that focuses the
    // field focuses the input inside it, and `query.forceActiveFocus()` reads
    // the way it did when the TextField carried the id.
    onActiveFocusChanged: if (activeFocus) textInput.forceActiveFocus()

    // edited fires on every change, programmatic ones included, exactly as the
    // TextField's own textChanged did. The text is in `text` by then, so the
    // handler reads the field rather than carrying a payload.
    signal edited()
    signal downPressed()
    signal upPressed()
    signal returnPressed()
    signal escapePressed()
    // A key none of the above took. The event is the same object the input saw,
    // so a handler that sets accepted stops it there.
    signal keyPressed(var event)

    Rectangle {
        anchors.fill: parent
        radius: Theme.radiusSmall
        color: root.background
        border.width: 1
        border.color: root.accented ? Theme.accent : Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        Row {
            anchors.fill: parent
            anchors.leftMargin: 14
            anchors.rightMargin: 14
            spacing: 10
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: root.icon
                font.family: Theme.fontFamily
                font.pixelSize: 13
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            TextField {
                id: textInput
                width: parent.width - 34
                anchors.verticalCenter: parent.verticalCenter
                placeholderText: root.placeholder
                color: Theme.textPrimary
                placeholderTextColor: Theme.textDim
                font.family: Theme.fontFamily
                font.pixelSize: 13
                background: null
                leftPadding: 0
                onTextChanged: root.edited()
                Keys.onDownPressed: root.downPressed()
                Keys.onUpPressed: root.upPressed()
                Keys.onReturnPressed: root.returnPressed()
                Keys.onEscapePressed: root.escapePressed()
                Keys.onPressed: function (e) { root.keyPressed(e) }
            }
        }
    }
}
