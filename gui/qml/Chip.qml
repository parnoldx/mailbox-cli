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
    // kbd: a one-letter keyboard hint shown faint after the label. The reader
    // toolbar chips each have a matching Shortcut; this is where it's advertised.
    property string kbd: ""
    // on: true → the chip is a two-state toggle that is currently on (filled
    // accent, e.g. the composer's Reply all).
    property bool on: false

    signal clicked()

    height: 20
    radius: 10
    width: row.implicitWidth + 18
    color: !interactive ? Theme.selection
         : on ? Theme.accent
         : hh.hovered ? (danger ? Theme.red : Theme.cardHover)
         : Theme.selection
    // A hairline edge so the pill reads as a button against the window, not
    // just a faint tint. Drops away once the fill is accent or a hover red.
    border.width: 1
    border.color: (interactive && !on && !(danger && hh.hovered)) ? Theme.hairline : "transparent"
    Behavior on color { ColorAnimation { duration: Theme.anim } }
    Behavior on border.color { ColorAnimation { duration: Theme.anim } }

    Row {
        id: row
        anchors.centerIn: parent
        spacing: 5

        Text {
            visible: chip.glyph.length > 0
            text: chip.glyph
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: chip.on ? Theme.onAccent
                 : chip.danger && hh.hovered ? "#ffffff"
                 : chip.accentGlyph && hh.hovered ? Theme.accent
                 : chip.glyphColor
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Text {
            visible: chip.label.length > 0
            text: chip.label
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: chip.on ? Theme.onAccent
                 : chip.danger && hh.hovered ? "#ffffff" : Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Text {
            visible: chip.kbd.length > 0
            text: chip.kbd
            font.family: Theme.fontFamily
            font.pixelSize: 9
            font.weight: Font.DemiBold
            opacity: 0.7
            color: chip.on ? Theme.onAccent
                 : chip.danger && hh.hovered ? "#ffffff" : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    HoverHandler { id: hh; enabled: chip.interactive; cursorShape: Qt.PointingHandCursor }
    TapHandler { enabled: chip.interactive; onTapped: chip.clicked() }
}
