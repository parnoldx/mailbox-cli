import QtQuick
import QtQuick.Controls.Basic

// The Composer's Send caret. Unlike BubbleMenu (bubbling a thread already in
// the Inbox is one reversible move, so picking a time fires it at once), this
// is attached to a draft that has not gone out yet: "Bubble up if no reply"
// first, then the same four timing choices, but staged — picking one shows
// the resolved date and waits here for an explicit Send rather than firing
// immediately, so the reminder is visible before the mail goes anywhere.
Popup {
    id: root
    modal: true
    dim: true
    focus: true
    padding: 6
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside

    // Fired once, when Send is pressed here — the same timing shape `bubble`
    // and `send --if-no-reply` take: {tomorrow:true} | {weekend:true} |
    // {next_week:true} | {on:"YYYY-MM-DD"}.
    signal send(var timing)

    property var timing: null
    property string label: ""
    // The timing choices are showing — either "Bubble up if no reply" was
    // just tapped, or a chosen date is being changed.
    property bool choosing: false

    onOpened: { root.timing = null; root.label = ""; root.choosing = false }

    implicitWidth: 240

    background: Rectangle {
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        radius: Theme.radiusSmall
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    component Choice: Rectangle {
        id: row
        property string label: ""
        signal chosen()
        width: parent.width
        height: 34
        color: hover.hovered ? Theme.selection : "transparent"
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            leftPadding: 10
            text: row.label
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textPrimary
        }
        HoverHandler { id: hover; cursorShape: Qt.PointingHandCursor }
        TapHandler { onTapped: row.chosen() }
    }

    contentItem: Column {
        spacing: 2

        Choice {
            visible: !root.choosing && root.timing === null
            label: "Bubble up if no reply"
            onChosen: root.choosing = true
        }

        Column {
            visible: root.choosing
            width: parent.width
            spacing: 2
            Choice {
                label: "Tomorrow"
                onChosen: { root.timing = { tomorrow: true }; root.label = "tomorrow"; root.choosing = false }
            }
            Choice {
                label: "This weekend"
                onChosen: { root.timing = { weekend: true }; root.label = "this weekend"; root.choosing = false }
            }
            Choice {
                label: "Next week"
                onChosen: { root.timing = { next_week: true }; root.label = "next week"; root.choosing = false }
            }
            Choice {
                label: "Pick a date…"
                onChosen: datePicker.open()
            }
        }

        // The resolved choice, shown until Send confirms it or "change" reopens
        // the timing list above.
        Column {
            visible: !root.choosing && root.timing !== null
            width: parent.width
            topPadding: 8
            bottomPadding: 10
            leftPadding: 10
            rightPadding: 10
            spacing: 10

            Row {
                width: parent.width - 20
                spacing: 8
                Text {
                    width: parent.width - change.implicitWidth - 8
                    wrapMode: Text.Wrap
                    text: "Bubble up if no reply by " + root.label
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textPrimary
                }
                Text {
                    id: change
                    text: "change"
                    font.family: Theme.fontFamily
                    font.pixelSize: 11
                    color: Theme.accent
                    TapHandler { cursorShape: Qt.PointingHandCursor; onTapped: root.choosing = true }
                }
            }

            AppButton {
                width: parent.width - 20
                kind: "primary"
                text: "Send"
                onClicked: { root.send(root.timing); root.close() }
            }
        }
    }

    DatePickerPopup {
        id: datePicker
        onPicked: function (isoDate) {
            root.timing = { on: isoDate }
            root.label = isoDate
            root.choosing = false
        }
    }
}
