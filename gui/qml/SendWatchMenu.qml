import QtQuick
import QtQuick.Controls.Basic

// The Composer's Send caret. Unlike BubbleMenu (bubbling a thread already in
// the Inbox is one reversible move, so picking a time fires it at once), this
// is attached to a draft that has not gone out yet, and offers two staged,
// mutually exclusive holds — picking one shows the resolved choice and waits
// here for an explicit Send rather than firing immediately:
//
//   - "Bubble up if no reply": sends now, but the copy in Sent comes back to
//     the Inbox on its own if the thread stays quiet (four timing choices).
//   - "Send later": does not send now at all — the outbox holds the mail
//     until the picked date and hour (internal/daemon/send.go, sendAt).
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
    // Fired once, when Send is pressed after picking a date and hour — the
    // local "YYYY-MM-DDTHH:mm" send_at takes.
    signal sendLater(string sendAtIso)

    property var timing: null
    property string label: ""
    property string sendAtIso: ""
    property string sendAtLabel: ""
    // "" | "watch-choosing" | "later-choosing" — which staged flow, if any, is
    // showing its picker right now.
    property string mode: ""
    property var dateOptions: []
    property var hourOptions: []

    function _pad(n) { return n < 10 ? "0" + n : "" + n }
    // Today, tomorrow, then one date a day out to a month from now — the
    // range HEY-style schedule-send pickers offer, capped rather than open
    // ended so the list stays one scroll, not a calendar to hunt through.
    function _dateOptions() {
        var out = []
        var base = new Date(); base.setHours(0, 0, 0, 0)
        for (var i = 0; i < 31; i++) {
            var d = new Date(base.getTime() + i * 86400000)
            var label = i === 0 ? "Today" : i === 1 ? "Tomorrow" : Qt.formatDate(d, "ddd d MMM")
            out.push({ label: label, value: Qt.formatDate(d, "yyyy-MM-dd") })
        }
        return out
    }
    // Hourly slots for one date, in whatever the OS locale shows a short time
    // as (24h or 12h with AM/PM) — Qt.formatTime already knows which. Today
    // only offers hours still ahead: picking one always lands in the future.
    function _hourOptions(dateValue) {
        var todayValue = Qt.formatDate(new Date(), "yyyy-MM-dd")
        var startHour = dateValue === todayValue ? new Date().getHours() + 1 : 0
        var out = []
        for (var h = startHour; h < 24; h++) {
            var t = new Date(); t.setHours(h, 0, 0, 0)
            out.push({ label: Qt.formatTime(t), value: h })
        }
        return out
    }

    onOpened: {
        root.timing = null; root.label = ""
        root.sendAtIso = ""; root.sendAtLabel = ""
        root.mode = ""
        root.dateOptions = root._dateOptions()
        root.hourOptions = root._hourOptions(root.dateOptions[0].value)
        dateDrop.currentIndex = 0
        hourDrop.currentIndex = 0
        dateDrop.open = false
        hourDrop.open = false
    }

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

    // A dropdown styled like the rest of this popup rather than the platform's
    // own combo box, so it reads as one menu: a field showing the current
    // choice. The options list is NOT a floating box over the field — that
    // box used to open upward, outside this popup's bounds, where the modal
    // overlay ate the mouse wheel before it ever reached the list and nothing
    // scrolled. The options now render in-flow below the field row
    // (listArea in the "Send later" stage), inside the popup.
    component Dropdown: Rectangle {
        id: field
        property var options: []
        property int currentIndex: 0
        property bool open: false
        // The sibling dropdown — opening one closes the other.
        property var other: null
        readonly property var currentValue: field.options.length > 0 ? field.options[field.currentIndex].value : null
        readonly property string currentLabel: field.options.length > 0 ? field.options[field.currentIndex].label : ""
        height: 30
        radius: Theme.radiusSmall
        color: fieldHover.hovered ? Theme.cardHover : Theme.cardBg
        border.width: 1
        border.color: Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }

        Text {
            anchors.verticalCenter: parent.verticalCenter
            anchors.left: parent.left
            anchors.leftMargin: 8
            anchors.right: caret.left
            elide: Text.ElideRight
            text: field.currentLabel
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textPrimary
        }
        Text {
            id: caret
            anchors.verticalCenter: parent.verticalCenter
            anchors.right: parent.right
            anchors.rightMargin: 8
            text: ""
            font.family: Theme.fontFamily
            font.pixelSize: 9
            color: Theme.textDim
        }
        HoverHandler { id: fieldHover; cursorShape: Qt.PointingHandCursor }
        TapHandler {
            onTapped: {
                field.open = !field.open
                if (field.other) field.other.open = false
            }
        }
    }

    contentItem: Column {
        spacing: 2

        Column {
            visible: root.mode === "" && root.timing === null && root.sendAtIso === ""
            width: parent.width
            spacing: 2
            Choice { label: "Bubble up if no reply"; onChosen: root.mode = "watch-choosing" }
            Choice { label: "Send later…"; onChosen: root.mode = "later-choosing" }
        }

        Column {
            visible: root.mode === "watch-choosing"
            width: parent.width
            spacing: 2
            Choice {
                label: "Tomorrow"
                onChosen: { root.timing = { tomorrow: true }; root.label = "tomorrow"; root.mode = "" }
            }
            Choice {
                label: "This weekend"
                onChosen: { root.timing = { weekend: true }; root.label = "this weekend"; root.mode = "" }
            }
            Choice {
                label: "Next week"
                onChosen: { root.timing = { next_week: true }; root.label = "next week"; root.mode = "" }
            }
            Choice {
                label: "Pick a date…"
                onChosen: datePicker.open()
            }
        }

        // The resolved "if no reply" choice, shown until Send confirms it or
        // "change" reopens the timing list above.
        Column {
            visible: root.mode === "" && root.timing !== null
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
                    TapHandler { cursorShape: Qt.PointingHandCursor; onTapped: root.mode = "watch-choosing" }
                }
            }

            AppButton {
                width: parent.width - 20
                kind: "primary"
                text: "Send"
                onClicked: { root.send(root.timing); root.close() }
            }
        }

        // Send later: a date and an hour, side by side, then a button that
        // resolves them into one send_at rather than sending anything yet.
        Column {
            visible: root.mode === "later-choosing"
            width: parent.width
            topPadding: 4
            bottomPadding: 10
            leftPadding: 10
            rightPadding: 10
            spacing: 10

            Row {
                width: parent.width - 20
                spacing: 8
                Dropdown {
                    id: dateDrop
                    width: (parent.width - 8) * 0.55
                    options: root.dateOptions
                    other: hourDrop
                    // Reads options[currentIndex] directly rather than the
                    // currentValue alias: that alias is itself a binding on
                    // currentIndex, and picking it up here raced its own
                    // update — this always sees the date that was just
                    // chosen, so tomorrow (and beyond) always gets the full
                    // 24 hours, not today's "hours still ahead" list.
                    onCurrentIndexChanged: {
                        var picked = options[currentIndex] ? options[currentIndex].value : ""
                        root.hourOptions = root._hourOptions(picked)
                        hourDrop.currentIndex = 0
                    }
                }
                Dropdown {
                    id: hourDrop
                    width: (parent.width - 8) * 0.45
                    options: root.hourOptions
                    other: dateDrop
                }
            }

            // The open dropdown's options, rendered in-flow below the fields so
            // they live inside the popup's bounds — the old floating list
            // opened upward outside the popup, where the modal overlay
            // swallowed the mouse wheel and nothing scrolled. Capped at eight
            // rows and scrolls beyond that; the always-on scrollbar says so.
            Rectangle {
                id: listArea
                readonly property var d: dateDrop.open ? dateDrop : (hourDrop.open ? hourDrop : null)
                visible: d !== null
                width: parent.width - 20
                height: visible ? Math.min(8, d.options.length) * 28 + 8 : 0
                radius: Theme.radiusSmall
                color: Theme.railBg
                border.width: 1
                border.color: Theme.hairline
                clip: true
                ListView {
                    id: optionsView
                    anchors.fill: parent
                    anchors.margins: 4
                    model: listArea.d ? listArea.d.options : []
                    clip: true
                    boundsBehavior: Flickable.StopAtBounds
                    // The wheel drives contentY directly (same lid as the
                    // web-view fix in ThreadMessage): the Flickable's native
                    // wheel handling is unreliable in popup nesting.
                    // `acceptedButtons: NoButton` lets clicks fall through to
                    // the options.
                    MouseArea {
                        anchors.fill: parent
                        acceptedButtons: Qt.NoButton
                        onWheel: function (wheel) {
                            var max = Math.max(0, optionsView.contentHeight - optionsView.height)
                            var dy = wheel.angleDelta.y !== 0 ? wheel.angleDelta.y : wheel.pixelDelta.y
                            optionsView.contentY = Math.max(0, Math.min(max, optionsView.contentY - dy))
                            wheel.accepted = true
                        }
                    }
                    ScrollBar.vertical: ScrollBar {
                        policy: ScrollBar.AlwaysOn
                        contentItem: Rectangle {
                            implicitWidth: 4
                            radius: 2
                            color: Theme.textDim
                        }
                    }
                    delegate: Rectangle {
                        width: ListView.view.width
                        height: 28
                        color: itemHover.hovered ? Theme.selection : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            leftPadding: 8
                            text: modelData.label
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            color: Theme.textPrimary
                        }
                        HoverHandler { id: itemHover; cursorShape: Qt.PointingHandCursor }
                        TapHandler {
                            onTapped: { listArea.d.currentIndex = index; listArea.d.open = false }
                        }
                    }
                }
            }

            AppButton {
                width: parent.width - 20
                kind: "primary"
                text: "Schedule send"
                enabled: root.hourOptions.length > 0
                onClicked: {
                    root.sendAtIso = dateDrop.currentValue + "T" + root._pad(hourDrop.currentValue) + ":00"
                    root.sendAtLabel = dateDrop.currentLabel + " at " + hourDrop.currentLabel
                    root.mode = ""
                }
            }
        }

        // The resolved "send later" choice, shown until Send confirms it or
        // "change" reopens the date/hour pickers above.
        Column {
            visible: root.mode === "" && root.sendAtIso !== ""
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
                    width: parent.width - changeLater.implicitWidth - 8
                    wrapMode: Text.Wrap
                    text: "Send " + root.sendAtLabel
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textPrimary
                }
                Text {
                    id: changeLater
                    text: "change"
                    font.family: Theme.fontFamily
                    font.pixelSize: 11
                    color: Theme.accent
                    TapHandler { cursorShape: Qt.PointingHandCursor; onTapped: root.mode = "later-choosing" }
                }
            }

            AppButton {
                width: parent.width - 20
                kind: "primary"
                text: "Send"
                onClicked: { root.sendLater(root.sendAtIso); root.close() }
            }
        }
    }

    DatePickerPopup {
        id: datePicker
        onPicked: function (isoDate) {
            root.timing = { on: isoDate }
            root.label = isoDate
            root.mode = ""
        }
    }
}
