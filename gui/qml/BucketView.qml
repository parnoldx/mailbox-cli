import QtQuick
import QtQuick.Controls.Basic
import "Fuzzy.js" as Fuzzy

Item {
    id: root

    readonly property bool isInbox: win.currentKey() === "INBOX"
    // The archive view: the picker at the top is its own state, the list
    // underneath is a plain box view of the picked Box.
    readonly property bool isArchive: win.currentKey() === "Archive"
    property var pickBoxes: []
    property int pickActive: 0
    // The picker's query. Root owns it so the root-level functions can clear
    // it: pickQuery itself lives inside the header Component (ids there are
    // invisible to outer bindings), and two-way binds to this.
    property string pickFilter: ""
    // The picker's list: the boxes that have no view or menu route of their
    // own and would only be reachable by moving. Every account's tree is in:
    // a Secondary's boxes carry its prefix on the name ("work/Archive/2021"),
    // and the folder glyph carries that account's color to tell the trees
    // apart.
    readonly property var pickMatches:
        Fuzzy.pickerBoxes(pickBoxes).filter(function (b) {
            var q = pickFilter.trim()
            return !q || Fuzzy.fuzzy(q, b.box)
        })

    // Root owns only properties — pickQuery itself is inside the header
    // Component, unreachable from here. Setting archiveBoxOpen makes the
    // field's Row visible, whose onVisibleChanged focuses the field.
    function openPicker() {
        pickFilter = ""
        pickActive = 0
        win.archiveBoxOpen = true
    }
    function chooseBox(i) {
        var m = pickMatches[i]
        if (!m) return
        win.archiveBox = m.box
        win.loadBucket()
        pickActive = 0
        Qt.callLater(function () { win.navView().forceActiveFocus() })
    }
    // Escape inside the picker: fold back to the chosen box's button, or, with
    // none chosen, leave the archive view for the Inbox.
    function pickerEscape() {
        if (win.archiveBox !== "") {
            root.pickFilter = ""
            win.navView().forceActiveFocus()
        } else {
            win.switchToKey("INBOX")
        }
    }
    onIsArchiveChanged: {
        if (!isArchive) return
        win.loadArchiveBoxes(function (l) { root.pickBoxes = l || [] })
        if (win.archiveBox === "") root.openPicker()
    }

    // Back to the header top: a bucket switch lands at the first row, so the
    // archive picker's input and its dropdown open where the header is drawn.
    function toTop() {
        list.contentY = 0
    }

    // Senders sitting in the Screener, waiting on a decision. Drives the one
    // entry point to the Screener: the button top-left of the Inbox.
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
        list.positionViewAtIndex(hi, ListView.Contain)
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

    // The rows, lazily. A ListView instantiates only the MailRows on screen
    // and, on a model reset, rebuilds just those; the Column + Repeater it
    // replaces built every row of the bucket (up to 200) the moment setRows
    // landed: a ~200 ms main-thread stall right after the first frame, and
    // again after every background refresh. One list over the flat rows (the
    // order move()/openHighlighted() index into), with the unseen/seen split
    // drawn as ListView sections.
    ListView {
        id: list
        x: Math.max(40, (parent.width - 880) / 2)
        width: Math.min(880, parent.width - 80)
        anchors { top: parent.top; bottom: parent.bottom }
        contentWidth: width
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        spacing: 4
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        model: root.flatRows
        section { property: "seen"; criteria: ViewSection.FullString; delegate: sectionHeading }

        // The bucket title, search and account pills scroll away with the
        // list, as they always did; the empty state sits under them when the
        // bucket has no rows at all.
        header: Column {
            width: list.width
            topPadding: 56
            spacing: 4
            Row {
                width: parent.width
                spacing: 14
                Text {
                    text: win.viewGlyph()
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
                            text: win.viewTitle()
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
                                text: "\uf002"
                                font.family: Theme.fontFamily
                                font.pixelSize: 13
                                color: searchHover.hovered ? Theme.accent : Theme.textDim
                                Behavior on color { ColorAnimation { duration: Theme.anim } }
                            }
                            HoverHandler { id: searchHover; cursorShape: Qt.PointingHandCursor }
                            TapHandler { onTapped: win.openSearch() }
                        }

                        // Which account the list shows: All, then one pill per
                        // account outlined in its colour. Only with more than
                        // one account, and only on buckets a Secondary has too.
                        Row {
                            visible: win.filterApplies()
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 6
                            Repeater {
                                model: [{ name: "", label: "All" }].concat(win.accounts)
                                delegate: Rectangle {
                                    readonly property bool on: win.accountFilter === modelData.name
                                    readonly property color tint: modelData.name ? win.accountColor(modelData.name) : Theme.textDim
                                    width: pillText.implicitWidth + 24; height: 28; radius: 14
                                    color: on ? tint
                                         : pillHover.hovered ? Theme.cardHover : "transparent"
                                    border.width: 1
                                    // The tint is always on: text and border
                                    // wear the account's colour unselected too,
                                    // so a row's coloured line maps to its pill
                                    // at a glance. Selected is the solid fill.
                                    border.color: on ? "transparent" : tint
                                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                                    Text {
                                        id: pillText
                                        anchors.centerIn: parent
                                        text: modelData.label
                                        font.family: Theme.fontFamily
                                        font.pixelSize: 12
                                        font.weight: parent.on ? Font.DemiBold : Font.Normal
                                        color: parent.on ? "#ffffff" : parent.tint
                                    }
                                    HoverHandler { id: pillHover; cursorShape: Qt.PointingHandCursor }
                                    TapHandler { onTapped: win.setAccountFilter(modelData.name) }
                                }
                            }
                        }
                    }
                }
            }

            Item { width: 1; height: 24 }

            // ---- archive view -----------------------------------------------------
            // Key 8's view: a fuzzy box picker whose list is the same one the
            // "Move to…" menus offer. When a box is chosen the
            // picker is a quiet button and the mail follows below.
            Rectangle {
                id: pickLine
                visible: root.isArchive
                width: Math.min(560, parent.width)
                height: 38
                radius: Theme.radiusSmall
                color: Theme.windowBg
                border.width: 1
                border.color: win.archiveBoxOpen ? Theme.accent : Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }

                // Editing: the field. It is up whenever a box is being hunted
                // for or none is chosen yet.
                Row {
                    visible: win.archiveBox === "" || win.archiveBoxOpen
                    onVisibleChanged: if (visible) pickQuery.forceActiveFocus()
                    anchors.fill: parent
                    anchors.leftMargin: 12
                    anchors.rightMargin: 12
                    spacing: 10
                    // The bucket load under the header can strip the field's
                    // focus again after the Row's own visible-changed focus
                    // ran (switchTo loads right beside openPicker); refocus
                    // once the wheel has stopped turning.
                    Connections {
                        target: root
                        function onIsArchiveChanged() {
                            if (root.isArchive && win.archiveBox === "") pickFocus.restart()
                        }
                    }
                    Timer { id: pickFocus; interval: 1; onTriggered: pickQuery.forceActiveFocus() }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "\uf187"
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    TextField {
                        id: pickQuery
                        text: root.pickFilter
                        onTextChanged: if (text !== root.pickFilter) root.pickFilter = text
                        width: parent.width - 34
                        anchors.verticalCenter: parent.verticalCenter
                        placeholderText: root.pickMatches.length === 0 ? "No boxes loaded" : "Pick an archive box…"
                        color: Theme.textPrimary
                        placeholderTextColor: Theme.textDim
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        background: null
                        leftPadding: 0
                        // The focus mirror: the Shortcuts read win.archiveBoxOpen,
                        // so the digits and the j/k/o letters know when the
                        // field is typing text instead.
                        onActiveFocusChanged: win.archiveBoxOpen = activeFocus
                        Keys.onDownPressed: root.pickActive = Math.min(root.pickMatches.length - 1, root.pickActive + 1)
                        Keys.onUpPressed: root.pickActive = Math.max(0, root.pickActive - 1)
                        Keys.onReturnPressed: root.chooseBox(root.pickActive)
                    }
                }

                // Chosen: box name plus a pencil, one tap to re-pick.
                Row {
                    visible: win.archiveBox !== "" && !win.archiveBoxOpen
                    anchors.fill: parent
                    anchors.leftMargin: 12
                    anchors.rightMargin: 12
                    spacing: 10
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "\uf187"
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        // The picked box's account, same rule as the picker
                        // rows: "work/…" tints work's color, a Primary box
                        // the Primary's.
                        color: win.accountColor(win.accountOfId(win.archiveBox))
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: win.archiveBox
                        elide: Text.ElideMiddle
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        anchors.verticalCenter: parent.verticalCenter
                        text: "\uf040"
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    HoverHandler { cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: root.openPicker() }
                }
            }

            // The picker's dropdown. It sits in the header just under the
            // field, only while that has focus, and pushes the list down the
            // same way the Command Launcher's card grows.
            Rectangle {
                visible: root.isArchive && win.archiveBoxOpen && root.pickMatches.length > 0
                width: Math.min(560, parent.width)
                height: 12 + Math.min(root.pickMatches.length, 8) * 36
                clip: true
                radius: Theme.radiusSmall
                color: Theme.railBg
                border.width: 1
                border.color: Theme.hairline
                ListView {
                    anchors { fill: parent; margins: 6 }
                    clip: true
                    spacing: 2
                    // The matches scroll once they outgrow the eight rows the
                    // dropdown shows, so a Secondary's tree ("work/…") stays
                    // reachable without typing to filter it first. currentIndex
                    // follows pickActive, so j/k and typing keep the row in view.
                    currentIndex: root.pickActive
                    model: root.pickMatches
                    delegate: Rectangle {
                            width: ListView.view.width
                            height: 34
                            radius: Theme.radiusSmall
                            color: index === root.pickActive ? Theme.selection
                                 : rowHover.hovered ? Theme.cardHover : "transparent"
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                            // Anchored children, not a Row: a Row ignores
                            // child anchors and misshapes width-pinned ones,
                            // which is what had icon and text overlapping.
                            Text {
                                id: boxGlyph
                                anchors { left: parent.left; leftMargin: 10; verticalCenter: parent.verticalCenter }
                                text: "\uf07b"
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                                // The owning account's color, read off the
                                // box name's account prefix ("work/…"), the
                                // same rule box view uses to open it.
                                color: win.accountColor(win.accountOfId(modelData.box))
                                Behavior on color { ColorAnimation { duration: Theme.anim } }
                            }
                            Text {
                                anchors {
                                    left: boxGlyph.right; leftMargin: 10
                                    right: parent.right; rightMargin: 44
                                    verticalCenter: parent.verticalCenter
                                }
                                text: modelData.box
                                elide: Text.ElideMiddle
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                                color: Theme.textPrimary
                            }
                            Pill {
                                anchors { right: parent.right; rightMargin: 8; verticalCenter: parent.verticalCenter }
                                value: modelData.count || 0
                            }
                            HoverHandler {
                                id: rowHover
                                onHoveredChanged: if (hovered) root.pickActive = index
                            }
                            TapHandler { onTapped: root.chooseBox(index) }
                        }
                    }
                }

            // The empty bucket: the end-of-list state, shown when the model
            // has no rows at all.
            Column {
                width: parent.width
                spacing: 10
                visible: listModel.count === 0
                         && !(root.isArchive && win.archiveBox === "")
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

        // Room for the pile stacks (and a little air) below the last row, the
        // way the old Flickable reserved it in contentHeight.
        footer: Item { width: 1; height: 120 + (bottomStacks.visible ? bottomStacks.height + 16 : 0) }

        delegate: MailRow {
            width: list.width
            row: modelData
            fresh: modelData.seen === false
            highlighted: root.hi === index
            // Drafts: a row opens the composer (via win.openMessage, which
            // routes drafts on), the right-click menu is off, and each row
            // carries its own fast-delete.
            showDelete: win.isDraftsBucket()
            menuEnabled: !win.isDraftsBucket()
            onDeleteClicked: win.deleteDraft(modelData.id)
        }
    }

    // The unseen/seen section headers: the same two labels the old Column
    // showed, now appearing only when their section has rows. The topPadding
    // stands in for the spacer Items the Column carried between the blocks.
    Component {
        id: sectionHeading
        Column {
            topPadding: 26
            width: list.width
            SectionLabel {
                width: parent.width
                text: section === "false" ? (root.isInbox ? "New for you" : "Unread")
                                          : (root.isInbox ? "Previously seen" : "Everything else")
                count: section === "false" ? root.newRows.length : root.seenRows.length
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
    // the Inbox: writing a new mail is an Inbox action, not something you do
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

    // A label is managed from the view of it: renamed through the launcher's
    // own field, deleted behind one confirming second press: deleting takes
    // the keyword off every message carrying it, and there is no undo.
    AppButton {
        id: renameBtn
        anchors { right: parent.right; top: parent.top; margins: 24 }
        visible: win.labelView !== ""
        kind: "soft"
        glyph: "\uf044"
        text: "Rename"
        onClicked: win.openLabelRename()
    }
    AppButton {
        id: deleteBtn
        property bool armed: false
        anchors { right: renameBtn.left; rightMargin: 10; verticalCenter: renameBtn.verticalCenter }
        visible: win.labelView !== ""
        kind: "danger"
        glyph: "\uf1f8"
        text: armed ? "Really delete?" : "Delete label"
        onClicked: {
            if (!armed) { armed = true; disarm.restart(); return }
            armed = false
            win.deleteLabel(win.labelView)
        }
        Timer { id: disarm; interval: 3000; onTriggered: deleteBtn.armed = false }
        // Leaving the label view puts the safety back on.
        Connections {
            target: win
            function onLabelViewChanged() { deleteBtn.armed = false }
        }
    }

    // The Screener lives here: a button just left of Compose, shown only when
    // senders are actually waiting. It is the one way in: there is no bucket
    // key or launcher entry for it: and `screener` decisions send you back.
    AppButton {
        anchors { right: composeBtn.left; rightMargin: 10; verticalCenter: composeBtn.verticalCenter }
        visible: root.isInbox && root.screenerWaiting > 0
        kind: "soft"
        glyph: "\uf0c0"
        text: "Screener · " + root.screenerWaiting
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
