import QtQuick
import QtQuick.Controls.Basic

// A month grid for "Bubble up… Pick a date". No native QtQuick.Controls
// calendar exists in this build, so it's a small custom one in the same
// hand-styled idiom as the rest of gui/qml (compare ScreenerMoveMenu, Chip).
// Emits picked("YYYY-MM-DD") — the exact shape `--on DATE` takes — and closes.
Popup {
    id: root
    modal: true
    dim: true
    focus: true
    parent: Overlay.overlay
    anchors.centerIn: parent
    padding: 16
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    signal picked(string isoDate)

    // The first of the month currently shown. Reset to this month whenever
    // the popup opens, so it never opens showing wherever it was last left.
    property var shown: new Date()
    onOpened: shown = new Date()

    function _pad(n) { return n < 10 ? "0" + n : "" + n }
    function _iso(y, m, d) { return y + "-" + _pad(m + 1) + "-" + _pad(d) }
    function _sameDay(a, y, m, d) {
        return a.getFullYear() === y && a.getMonth() === m && a.getDate() === d
    }

    background: Rectangle {
        implicitWidth: 260
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        radius: Theme.radiusSmall
    }

    contentItem: Column {
        spacing: 10

        Row {
            width: 228
            height: 26
            Rectangle {
                width: 26; height: 26; radius: 13
                color: prevHover.hovered ? Theme.cardHover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Text {
                    anchors.centerIn: parent; text: ""
                    font.family: Theme.fontFamily; font.pixelSize: 11; color: Theme.textDim
                }
                HoverHandler { id: prevHover; cursorShape: Qt.PointingHandCursor }
                TapHandler {
                    onTapped: root.shown = new Date(root.shown.getFullYear(), root.shown.getMonth() - 1, 1)
                }
            }
            Text {
                width: parent.width - 52
                horizontalAlignment: Text.AlignHCenter
                anchors.verticalCenter: parent.verticalCenter
                text: Qt.formatDate(root.shown, "MMMM yyyy")
                font.family: Theme.fontFamily
                font.pixelSize: 12
                font.weight: Font.DemiBold
                color: Theme.textPrimary
            }
            Rectangle {
                width: 26; height: 26; radius: 13
                color: nextHover.hovered ? Theme.cardHover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Text {
                    anchors.centerIn: parent; text: ""
                    font.family: Theme.fontFamily; font.pixelSize: 11; color: Theme.textDim
                }
                HoverHandler { id: nextHover; cursorShape: Qt.PointingHandCursor }
                TapHandler {
                    onTapped: root.shown = new Date(root.shown.getFullYear(), root.shown.getMonth() + 1, 1)
                }
            }
        }

        Grid {
            width: 228
            columns: 7
            Repeater {
                model: ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"]
                Text {
                    width: 32; height: 22
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    text: modelData
                    font.family: Theme.fontFamily
                    font.pixelSize: 10
                    color: Theme.textDim
                }
            }
        }

        Grid {
            id: days
            width: 228
            columns: 7

            // Monday-first offset for the 1st of the shown month, and the
            // month's day count — both recomputed whenever `shown` changes.
            readonly property int firstWeekday: (new Date(shown.getFullYear(), shown.getMonth(), 1).getDay() + 6) % 7
            readonly property int dayCount: new Date(shown.getFullYear(), shown.getMonth() + 1, 0).getDate()

            Repeater {
                model: days.firstWeekday
                Item { width: 32; height: 32 }
            }
            Repeater {
                model: days.dayCount
                Rectangle {
                    id: cell
                    property int dayNum: index + 1
                    property bool isToday: root._sameDay(new Date(), shown.getFullYear(), shown.getMonth(), dayNum)
                    width: 32; height: 32; radius: 16
                    color: dayHover.hovered ? Theme.selection : "transparent"
                    border.width: isToday ? 1 : 0
                    border.color: Theme.accent
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Text {
                        anchors.centerIn: parent
                        text: cell.dayNum
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: Theme.textPrimary
                    }
                    HoverHandler { id: dayHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler {
                        onTapped: {
                            root.picked(root._iso(shown.getFullYear(), shown.getMonth(), cell.dayNum))
                            root.close()
                        }
                    }
                }
            }
        }
    }
}
