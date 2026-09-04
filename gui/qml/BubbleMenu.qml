import QtQuick
import QtQuick.Controls.Basic

// The four timing choices Bubble Up offers — HEY, matched (ADR-0023): three
// one-tap shortcuts and a date picker for anything else. Fires `chosen(timing)`
// with the same shape the daemon's `bubble` and `send`/`reply`/`forward` RPCs
// take ({tomorrow:true} | {weekend:true} | {next_week:true} | {on:"YYYY-MM-DD"})
// rather than acting itself, so both a reading-view "Bubble up" chip and the
// Composer's Send caret can reuse this one menu for two different actions.
Menu {
    id: menu

    signal chosen(var timing)

    implicitWidth: 208
    topPadding: 6
    bottomPadding: 6

    background: Rectangle {
        implicitWidth: 208
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        radius: Theme.radiusSmall
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    component Choice: MenuItem {
        id: mi
        height: 34
        contentItem: Text {
            anchors.verticalCenter: parent.verticalCenter
            leftPadding: 14
            text: mi.text
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textPrimary
        }
        background: Rectangle {
            color: mi.highlighted ? Theme.selection : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Choice { text: "Tomorrow"; onTriggered: menu.chosen({ tomorrow: true }) }
    Choice { text: "This weekend"; onTriggered: menu.chosen({ weekend: true }) }
    Choice { text: "Next week"; onTriggered: menu.chosen({ next_week: true }) }
    Choice { text: "Pick a date…"; onTriggered: datePicker.open() }

    DatePickerPopup {
        id: datePicker
        onPicked: function (isoDate) { menu.chosen({ on: isoDate }) }
    }
}
