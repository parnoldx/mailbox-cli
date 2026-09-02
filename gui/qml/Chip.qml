import QtQuick

// The small pill used all over the reading-view toolbar and the message
// accordion: an optional leading glyph, a label, hover feedback. It replaces
// ~a dozen hand-rolled copies of the same Rectangle + Row + two Texts +
// HoverHandler + TapHandler.
//
//   interactive: false  → a static badge (no hover, no tap) — the tracker count
//   danger: true        → hover turns the pill red with white text (Trash, Block)
//   accentGlyph: true   → the glyph (only) goes accent on hover (Reply, Forward…)
//   glyphColor          → a fixed glyph colour for a static badge (the green tick)
Rectangle {
    id: chip

    property string glyph: ""
    property string label: ""
    property bool interactive: true
    property bool danger: false
    property bool accentGlyph: false
    property color glyphColor: Theme.textDim

    signal clicked()

    height: 20
    radius: 10
    width: row.implicitWidth + 18
    color: !interactive ? Theme.selection
         : hh.hovered ? (danger ? Theme.red : Theme.cardHover)
         : Theme.selection
    Behavior on color { ColorAnimation { duration: Theme.anim } }

    Row {
        id: row
        anchors.centerIn: parent
        spacing: 5

        Text {
            visible: chip.glyph.length > 0
            text: chip.glyph
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: chip.danger && hh.hovered ? "#ffffff"
                 : chip.accentGlyph && hh.hovered ? Theme.accent
                 : chip.glyphColor
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Text {
            visible: chip.label.length > 0
            text: chip.label
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: chip.danger && hh.hovered ? "#ffffff" : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    HoverHandler { id: hh; enabled: chip.interactive; cursorShape: Qt.PointingHandCursor }
    TapHandler { enabled: chip.interactive; onTapped: chip.clicked() }
}
