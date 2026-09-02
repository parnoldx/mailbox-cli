import QtQuick
import QtQuick.Controls.Basic

Item {
    id: root

    readonly property bool isInbox: win.currentKey() === "INBOX"

    // Senders sitting in the Screener, waiting on a decision. Drives the one
    // entry point to the Screener — the button top-left of the Inbox.
    readonly property int screenerWaiting:
        (win.counts["Screener"] && win.counts["Screener"].count) || 0

    // The bucket split, straight off the model's `rows` mirror (which re-emits
    // on every setRows).
    readonly property var newRows: listModel.rows.filter(function (r) { return r.seen === false })
    readonly property var seenRows: listModel.rows.filter(function (r) { return r.seen === true })
    readonly property var flatRows: newRows.concat(seenRows)
    property int hi: -1

    function move(d) {
        if (flatRows.length === 0) return
        hi = Math.max(0, Math.min(flatRows.length - 1, (hi < 0 ? 0 : hi) + d))
    }
    function openHighlighted() {
        if (hi >= 0 && hi < flatRows.length) win.openMessage(flatRows[hi].id)
        else if (flatRows.length > 0) win.openMessage(flatRows[0].id)
    }
    // The id of the highlighted row, for the triage keys and the Command
    // Launcher's action rows. "" when nothing is highlighted.
    function currentRowId() {
        return (hi >= 0 && hi < flatRows.length) ? flatRows[hi].id : ""
    }
    // Pop the row menu for `row` at the cursor, and make it the highlighted
    // row so the keys and the launcher line up with what was clicked.
    function showRowMenu(row) {
        if (!row || !row.id || win.isDraftsBucket()) return
        for (var i = 0; i < flatRows.length; i++)
            if (flatRows[i].id === row.id) { hi = i; break }
        rowMenu.row = row
        rowMenu.popup()
    }

    Connections {
        target: listModel
        function onChanged() { root.hi = listModel.count > 0 ? 0 : -1 }
    }

    Flickable {
        anchors.fill: parent
        contentWidth: width
        contentHeight: col.implicitHeight + 120 + (bottomStacks.visible ? bottomStacks.height + 16 : 0)
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
                    Row {
                        width: parent.width
                        spacing: 12
                        Text {
                            id: bucketTitle
                            text: win.buckets[win.bucketIndex].label
                            font.family: Theme.fontFamily
                            font.pixelSize: 30
                            font.weight: Font.Bold
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }

                        // Search. A magnifier just right of the bucket title,
                        // mirroring the `/` shortcut for the pointer. Only the
                        // Inbox carries it; the other buckets stay bare.
                        Rectangle {
                            id: searchBtn
                            visible: root.isInbox
                            anchors.verticalCenter: parent.verticalCenter
                            width: 34; height: 34; radius: 17
                            color: searchHover.hovered ? Theme.cardHover : Theme.selection
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                            Text {
                                anchors.centerIn: parent
                                text: ""
                                font.family: Theme.fontFamily
                                font.pixelSize: 13
                                color: searchHover.hovered ? Theme.accent : Theme.textDim
                                Behavior on color { ColorAnimation { duration: Theme.anim } }
                            }
                            HoverHandler { id: searchHover; cursorShape: Qt.PointingHandCursor }
                            TapHandler { onTapped: win.openSearch() }
                        }
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
                        // Drafts: a row opens the composer (via win.openMessage,
                        // which routes drafts on), the right-click menu is off,
                        // and each row carries its own fast-delete.
                        showDelete: win.isDraftsBucket()
                        menuEnabled: !win.isDraftsBucket()
                        onDeleteClicked: win.deleteDraft(modelData.id)
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
                        showDelete: win.isDraftsBucket()
                        menuEnabled: !win.isDraftsBucket()
                        onDeleteClicked: win.deleteDraft(modelData.id)
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

    // Right-click any row for the same triage the reading view and the Command
    // Launcher offer. One shared menu, re-pointed at whichever row opened it.
    RowActions {
        id: rowMenu
        bucketKey: win.currentKey()
    }

    // Into the compose view. Mirrors the `c` shortcut, for the pointer. Only in
    // the Inbox \u2014 writing a new mail is an Inbox action, not something you do
    // from Paper Trail or Set Aside.
    AppButton {
        id: composeBtn
        anchors { right: parent.right; top: parent.top; margins: 24 }
        visible: root.isInbox
        kind: "primary"
        glyph: "\uf040"
        text: "Compose"
        onClicked: win.startCompose()
    }

    // The Screener lives here: a button just left of Compose, shown only when
    // senders are actually waiting. It is the one way in \u2014 there is no bucket
    // key or launcher entry for it \u2014 and `screener` decisions send you back.
    AppButton {
        anchors { right: composeBtn.left; rightMargin: 10; verticalCenter: composeBtn.verticalCenter }
        visible: root.isInbox && root.screenerWaiting > 0
        kind: "soft"
        glyph: "\uf0c0"
        text: "Screener \u00b7 " + root.screenerWaiting
        onClicked: win.switchToKey("Screener")
    }

    // The two hand-tended piles, fanned along the bottom of the Inbox.
    BottomStacks {
        id: bottomStacks
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
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
