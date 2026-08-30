import QtQuick
import QtQuick.Controls.Basic

Item {
    id: root

    readonly property bool isInbox: win.currentKey() === "INBOX"

    function rowsWhere(seen) {
        var out = []
        for (var i = 0; i < listModel.count; i++) {
            var r = listModel.get(i)
            if (r.seen === seen) out.push(r)
        }
        return out
    }
    property var newRows: (listModel.count, rowsWhere(false))
    property var seenRows: (listModel.count, rowsWhere(true))
    property var flatRows: newRows.concat(seenRows)
    property int hi: -1

    function move(d) {
        if (flatRows.length === 0) return
        hi = Math.max(0, Math.min(flatRows.length - 1, (hi < 0 ? 0 : hi) + d))
    }
    function openHighlighted() {
        if (hi >= 0 && hi < flatRows.length) win.openMessage(flatRows[hi].id)
        else if (flatRows.length > 0) win.openMessage(flatRows[0].id)
    }

    Connections {
        target: listModel
        function onChanged() { root.hi = listModel.count > 0 ? 0 : -1 }
    }

    Flickable {
        anchors.fill: parent
        contentWidth: width
        contentHeight: col.implicitHeight + 120
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        Column {
            id: col
            x: Math.max(40, (parent.width - 880) / 2)
            width: Math.min(880, parent.width - 80)
            topPadding: 56
            spacing: 4

            Row {
                width: parent.width
                spacing: 14
                Text {
                    text: win.buckets[win.bucketIndex].glyph
                    font.family: Theme.fontFamily
                    font.pixelSize: 30
                    color: Theme.accent
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                Column {
                    width: parent.width - 44
                    spacing: 4
                    Text {
                        text: win.buckets[win.bucketIndex].label
                        font.family: Theme.fontFamily
                        font.pixelSize: 30
                        font.weight: Font.Bold
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: win.buckets[win.bucketIndex].blurb
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
            }

            Item { width: 1; height: 28 }

            SectionLabel {
                width: parent.width
                text: root.isInbox ? "New for you" : "Unread"
                count: root.newRows.length
                visible: root.newRows.length > 0
            }
            Column {
                width: parent.width
                visible: root.newRows.length > 0
                Repeater {
                    model: root.newRows
                    MailRow {
                        width: parent.width
                        row: modelData
                        fresh: true
                        highlighted: root.hi === index
                    }
                }
            }

            Item { width: 1; height: root.newRows.length > 0 && root.seenRows.length > 0 ? 34 : 0 }

            SectionLabel {
                width: parent.width
                text: root.isInbox ? "Previously seen" : "Everything else"
                count: root.seenRows.length
                visible: root.seenRows.length > 0
            }
            Column {
                width: parent.width
                visible: root.seenRows.length > 0
                Repeater {
                    model: root.seenRows
                    MailRow {
                        width: parent.width
                        row: modelData
                        fresh: false
                        highlighted: root.hi === root.newRows.length + index
                    }
                }
            }

            Column {
                width: parent.width
                spacing: 10
                visible: listModel.count === 0
                topPadding: 60
                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "\uf0e0"
                    font.family: Theme.fontFamily
                    font.pixelSize: 34
                    color: Theme.hairline
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "You are all caught up"
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
        }
    }

    Row {
        anchors { right: parent.right; bottom: parent.bottom; margins: 20 }
        spacing: 10
        opacity: 0.75
        Kbd { text: "J" }
        Kbd { text: "K" }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "move"
            font.family: Theme.fontFamily
            font.pixelSize: 11
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Item { width: 8; height: 1 }
        Kbd { text: "Ctrl K" }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: "switch bucket"
            font.family: Theme.fontFamily
            font.pixelSize: 11
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    // Connection status: just the dot.
    Rectangle {
        anchors { left: parent.left; bottom: parent.bottom; margins: 22 }
        width: 8; height: 8; radius: 4
        opacity: 0.85
        color: Mailbox.online ? Theme.green : Theme.yellow
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
}
