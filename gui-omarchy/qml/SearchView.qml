import QtQuick
import QtQuick.Controls.Basic

// Full-screen search over every Box. `search` is a ranked full-text query
// answered entirely from the Mirror (never a server query); a hit opens in the
// reader like any other message. Open only — no triage from here.
Item {
    id: root
    property bool opened: false
    property int active: -1

    function open() {
        query.text = ""
        searchModel.setRows([])
        root.rows = []
        root.active = -1
        opened = true
        Qt.callLater(function () { query.forceActiveFocus() })
    }
    function close() {
        opened = false
        query.focus = false
        win.navView().forceActiveFocus()
    }
    function toggle() { opened ? close() : open() }

    // A plain-array copy of the model the ListView binds to, the way BucketView
    // keeps one for its list.
    property var rows: []
    function rebuild() {
        root.rows = searchModel.rows
        if (root.rows.length === 0) root.active = -1
        else if (root.active < 0 || root.active >= root.rows.length) root.active = 0
    }
    Connections { target: searchModel; function onChanged() { root.rebuild() } }

    function move(d) {
        if (rows.length === 0) return
        active = Math.max(0, Math.min(rows.length - 1, (active < 0 ? 0 : active) + d))
        list.positionViewAtIndex(active, ListView.Contain)
    }
    function openActive() {
        if (rows.length === 0) return
        var i = (active >= 0 && active < rows.length) ? active : 0
        win.openSearchResult(rows[i].id)
    }

    // The empty state (no query) and the "no matches" line sit just below the
    // field; the results area gets whatever height is left.
    readonly property bool _typed: query.text.trim().length > 0
    readonly property int _resultsH:
        rows.length > 0 ? Math.min(rows.length * 78, 372) : (_typed ? 34 : 0)

    visible: opened || scrim.opacity > 0.01

    Rectangle {
        id: scrim
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.55)
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        TapHandler { onTapped: root.close() }
    }

    Rectangle {
        id: card
        width: Math.min(620, root.width - 80)
        height: 66 + root._resultsH + (root._resultsH > 0 ? 12 : 0)
        anchors.horizontalCenter: parent.horizontalCenter
        y: root.opened ? root.height * 0.12 : root.height * 0.12 - 12
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        Behavior on height { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        clip: true

        // Search field
        Rectangle {
            id: field
            anchors { top: parent.top; left: parent.left; right: parent.right; margins: 12 }
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
                    text: ""
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                TextField {
                    id: query
                    width: parent.width - 34
                    anchors.verticalCenter: parent.verticalCenter
                    placeholderText: "Search every box…"
                    color: Theme.textPrimary
                    placeholderTextColor: Theme.textDim
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    background: null
                    leftPadding: 0
                    onTextChanged: debounce.restart()
                    Keys.onDownPressed: root.move(1)
                    Keys.onUpPressed: root.move(-1)
                    Keys.onReturnPressed: {
                        if (root.rows.length > 0) root.openActive()
                        else win.runSearch(query.text)
                    }
                    Keys.onEscapePressed: root.close()
                }
            }
        }

        // Count / empty line
        Text {
            id: countLine
            anchors { top: field.bottom; left: parent.left; leftMargin: 16; topMargin: 8 }
            visible: root._typed
            text: root.rows.length === 0
                ? "No matches"
                : root.rows.length + (root.rows.length === 1 ? " match" : " matches")
            font.family: Theme.fontFamily
            font.pixelSize: 10
            font.weight: Font.DemiBold
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }

        // Results
        ListView {
            id: list
            anchors {
                top: countLine.visible ? countLine.bottom : field.bottom
                left: parent.left; right: parent.right; bottom: parent.bottom
                leftMargin: 12; rightMargin: 12; topMargin: 4; bottomMargin: 12
            }
            clip: true
            boundsBehavior: Flickable.StopAtBounds
            ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
            model: root.rows
            delegate: MailRow {
                width: list.width
                row: modelData
                fresh: modelData.seen === false
                highlighted: index === root.active
                menuEnabled: false
                openAction: (function (id) { win.openSearchResult(id) })
            }
        }
    }

    // Live search, lightly debounced so a fast typist does not fire a query a
    // keystroke.
    Timer {
        id: debounce
        interval: 220
        onTriggered: win.runSearch(query.text)
    }
}
