import QtQuick
import QtQuick.Controls.Basic

// The contacts pane: a 380px column that slides in from the right and pushes
// the mail views aside (Main anchors them to its left edge). One `contact
// list` call loads every card — the live book is ~143 rows — and the filter
// runs client-side. Phase 2 adds the Add/Edit form, delete confirmation and
// the sender lookup the reader's Shift+P lands on.
Item {
    id: root
    property bool opened: false
    // True while any child of the pane holds keyboard focus. Main reads this
    // for its keyboard gate (anyOverlay / bucketKeys / the Ctrl+O toggle), so the
    // pane's field takes raw text while the mail keys sleep.
    readonly property bool hasFocus: {
        var it = Window.activeFocusItem
        while (it) {
            if (it === root) return true
            it = it.parent
        }
        return false
    }
    property var contacts: []      // everything from `contact list`
    property var selected: null    // contact shown in detail, null = list view
    property int active: 0
    property bool formOpen: false  // the add/edit form is up — a third state beside list and detail
    property var editing: null     // the card the form edits, null = add mode
    property bool saving: false    // a save is in flight; the Save button sleeps
    property bool confirmDelete: false  // the detail's action row shows "Delete <name>? Yes / No"

    function open() {
        opened = true
        selected = null
        formOpen = false
        confirmDelete = false
        query.text = ""
        reload()
        Qt.callLater(function () { query.forceActiveFocus() })
    }
    function close() {
        opened = false
        query.focus = false
        win.navView().forceActiveFocus()
    }
    function toggle() { opened ? close() : open() }
    // Ctrl+O again while the pane sits open behind the mail: focus comes back to
    // where the pane was — the detail if one is showing, the filter otherwise.
    function refocus() {
        Qt.callLater(function () {
            if (root.selected) detail.forceActiveFocus()
            else query.forceActiveFocus()
        })
    }
    function reload() {
        Mailbox.call(["contact", "list"], { limit: 1000 }, function (r) {
            if (!r.ok || !r.data) { win.flash(win._failMsg("Contacts", r)); return }
            root.contacts = r.data
            // Keep the detail on the same card across a reload, by id.
            if (root.selected) {
                root.selected = null
                for (var i = 0; i < r.data.length; i++)
                    if (r.data[i].id === root._selId) { root.selected = r.data[i]; active = i; break }
            }
        })
    }
    // Esc from inside the pane: a detail showing steps back to its list, the
    // list itself lets Main's Esc chain close the pane.
    function esc() {
        if (root.formOpen) cancelForm()
        else if (root.confirmDelete) confirmDelete = false
        else if (root.selected) backToList()
        else close()
    }

    function showDetail(c) {
        root._selId = c.id
        for (var i = 0; i < rows.length; i++)
            if (rows[i].id === c.id) { active = i; break }
        root.selected = c
        root.confirmDelete = false
        Qt.callLater(function () { detail.forceActiveFocus() })
    }
    function backToList() {
        root.selected = null
        root.confirmDelete = false
        list.forceActiveFocus()
        list.positionViewAtIndex(active, ListView.Contain)
    }

    function move(d) {
        if (rows.length === 0) return
        active = Math.max(0, Math.min(rows.length - 1, (active < 0 ? 0 : active) + d))
        list.positionViewAtIndex(active, ListView.Contain)
    }
    function openActive() {
        if (rows.length === 0) return
        showDetail(rows[active >= 0 && active < rows.length ? active : 0])
    }

    // ---- phase 2: add / edit / delete -------------------------------------
    // The form covers list and detail both, and Cancel returns to wherever it
    // was opened from — the list for Add, the detail for Edit.
    function openAdd(name, email) {
        root.editing = null
        f_name.text = name || ""
        f_email.text = email || ""
        f_phone.text = ""
        f_org.text = ""
        f_note.text = ""
        root.confirmDelete = false
        root.formOpen = true
        Qt.callLater(function () { f_name.forceActiveFocus() })
    }
    function openEdit() {
        if (!root.selected) return
        root.editing = root.selected
        f_name.text = root.selected.name || ""
        f_email.text = ""
        f_phone.text = ""
        f_org.text = root.selected.organisation || ""
        f_note.text = root.selected.note || ""
        root.confirmDelete = false
        root.formOpen = true
        Qt.callLater(function () { f_name.forceActiveFocus() })
    }
    function cancelForm() {
        var wasEditing = root.editing
        root.editing = null
        root.formOpen = false
        if (wasEditing) showDetail(wasEditing)
        else backToList()
    }
    // Save: `contact add` for a new card. For an edit, `contact update` with
    // only the fields that changed (empty means "unchanged" on the daemon, so
    // a cleared field is simply not sent), then a new email, then a new phone
    // — one after another, stopping at the first failure.
    function save() {
        if (root.saving) return
        var name = f_name.text.trim()
        if (!root.editing) {
            if (name === "") { win.flash("A contact needs a name"); return }
            var args = { positional: name, org: f_org.text.trim(), note: f_note.text }
            var em = f_email.text.trim(), ph = f_phone.text.trim()
            if (em !== "") args.email = [em]
            if (ph !== "") args.phone = [ph]
            root.saving = true
            Mailbox.call(["contact", "add"], args, function (r) {
                root.saving = false
                if (!r.ok || !r.data) { win.flash(win._failMsg("Add contact", r)); return }
                root._selId = r.data.id
                root.selected = r.data
                root.formOpen = false
                root.reload()
                Qt.callLater(function () { detail.forceActiveFocus() })
                win.flash("Contact added")
            })
            return
        }
        var c = root.editing
        var id = String(c.id)
        var em = f_email.text.trim(), ph = f_phone.text.trim()
        var up = {}
        if (name !== "" && name !== (c.name || "")) up.name = name
        // Empty means "unchanged" to the daemon, so a blanked field is not sent.
        if (f_org.text.trim() !== "" && f_org.text.trim() !== (c.organisation || "")) up.org = f_org.text.trim()
        if (f_note.text.trim() !== "" && f_note.text !== (c.note || "")) up.note = f_note.text
        if (!em && !ph && !up.name && up.org === undefined && up.note === undefined) {
            // Nothing changed and nothing to add — just go back to the detail.
            root.formOpen = false
            showDetail(root.selected)
            return
        }
        root.saving = true
        function fail(label, r) {
            root.saving = false
            win.flash(win._failMsg(label, r))
        }
        function done() {
            root.saving = false
            root.formOpen = false
            root.reload()
            Qt.callLater(function () { detail.forceActiveFocus() })
            win.flash("Contact saved")
        }
        function addPhone() {
            if (ph === "") return done()
            Mailbox.call(["contact", "phone"], { positional: id, value: ph }, function (r) {
                if (!r.ok) return fail("Add phone", r)
                done()
            })
        }
        function addEmail() {
            if (em === "") return addPhone()
            Mailbox.call(["contact", "email"], { positional: id, value: em }, function (r) {
                if (!r.ok) return fail("Add email", r)
                addPhone()
            })
        }
        if (up.name || up.org !== undefined || up.note !== undefined) {
            Mailbox.call(["contact", "update"], Object.assign({ positional: id }, up), function (r) {
                if (!r.ok) return fail("Save contact", r)
                addEmail()
            })
        } else addEmail()
    }
    function dropYes() {
        if (!root.selected) return
        var id = String(root.selected.id)
        confirmDelete = false
        Mailbox.call(["contact", "drop"], { positional: id }, function (r) {
            win.flash(r && r.ok ? "Contact deleted" : win._failMsg("Delete contact", r))
            root._selId = null
            root.selected = null
            root.reload()
            Qt.callLater(function () { list.forceActiveFocus() })
        })
    }
    // Shift+P from the reader: the sender's card when their email is already
    // on one, the Add form prefilled otherwise. The pane reloads in the
    // background either way; reload() re-finds the detail by id.
    function openFor(name, email) {
        open()
        email = String(email || "")
        if (email === "") { openAdd(name, ""); return }
        var lc = email.toLowerCase()
        Mailbox.call(["contact", "search"], { positional: email, limit: 5 }, function (r) {
            var found = (r.ok && r.data) ? r.data : []
            var hit = null
            for (var i = 0; i < found.length && !hit; i++) {
                var es = found[i].emails || []
                for (var j = 0; j < es.length; j++)
                    if (String(es[j]).toLowerCase() === lc) { hit = found[i]; break }
            }
            if (hit) {
                root._selId = hit.id
                root.selected = hit
                Qt.callLater(function () { detail.forceActiveFocus() })
            } else openAdd(name, email)
        })
    }

    // Case-insensitive substring across name, emails, phones and organisation;
    // every whitespace-separated term must match ("jo gmx" finds Jo at gmx).
    function matches(c, q) {
        var hay = (c.name + " " + (c.emails || []).join(" ") + " " + (c.phones || []).join(" ")
                   + " " + (c.organisation || "")).toLowerCase()
        var terms = q.toLowerCase().split(/\s+/).filter(function (t) { return t !== "" })
        for (var i = 0; i < terms.length; i++)
            if (hay.indexOf(terms[i]) < 0) return false
        return true
    }
    function results() {
        var q = query.text
        var out = []
        for (var i = 0; i < contacts.length; i++)
            if (!q || matches(contacts[i], q)) out.push(contacts[i])
        return out
    }
    property var rows: (query.text, contacts, results())

    // Clipboard via a hidden TextEdit: selectAll + copy is the stock QML way.
    TextEdit { id: copier; visible: false; width: 0; height: 0 }
    function copy(text) { copier.text = text; copier.selectAll(); copier.copy() }
    // The id `selected` was opened on, so reload() can re-find it (property
    // bindings read _selId, not the object identity).
    property var _selId: null

    width: opened ? 380 : 0
    Behavior on width { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
    clip: true

    // Opaque background, hairline on the left edge where it meets the mail.
    Rectangle {
        anchors.fill: parent
        color: Theme.windowBg
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }
    Rectangle {
        width: 1
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom }
        color: Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    // ---- list view -------------------------------------------------------
    SectionLabel {
        id: head
        anchors { top: parent.top; left: parent.left; right: parent.right
                  topMargin: 14; leftMargin: 16; rightMargin: 16 }
        text: "Contacts"
        count: root.rows.length
        visible: !root.selected && !root.formOpen
    }

    // New contact — beside the header, list view only.
    Text {
        anchors { top: parent.top; right: parent.right; topMargin: 14; rightMargin: 16 }
        visible: !root.selected && !root.formOpen
        text: "\uf067"
        font.family: Theme.fontFamily
        font.pixelSize: 13
        color: addHover.hovered ? Theme.accent : Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        HoverHandler { id: addHover; cursorShape: Qt.PointingHandCursor }
        TapHandler { onTapped: root.openAdd() }
    }

    Rectangle {
        id: fieldWrap
        anchors { top: head.bottom; left: parent.left; right: parent.right
                  topMargin: 10; leftMargin: 12; rightMargin: 12 }
        height: 42
        radius: Theme.radiusSmall
        color: Theme.railBg
        border.width: 1
        border.color: query.activeFocus ? Theme.accent : Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        visible: !root.selected && !root.formOpen

        Row {
            anchors.fill: parent
            anchors.leftMargin: 14
            anchors.rightMargin: 14
            spacing: 10
            Text {
                anchors.verticalCenter: parent.verticalCenter
                text: "\uf002"
                font.family: Theme.fontFamily
                font.pixelSize: 13
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            TextField {
                id: query
                width: parent.width - 34
                anchors.verticalCenter: parent.verticalCenter
                placeholderText: "Filter contacts…"
                color: Theme.textPrimary
                placeholderTextColor: Theme.textDim
                font.family: Theme.fontFamily
                font.pixelSize: 13
                background: null
                leftPadding: 0
                onTextChanged: root.active = 0
                // `n` opens the Add form — but only from an empty filter,
                // otherwise it is text.
                Keys.onPressed: function (e) {
                    if (e.key === Qt.Key_N && text === "") { root.openAdd(); e.accepted = true }
                }
                Keys.onDownPressed: { list.forceActiveFocus(); root.move(1) }
                Keys.onUpPressed: root.move(-1)
                Keys.onReturnPressed: if (root.rows.length > 0) root.openActive()
            }
        }
    }

    ListView {
        id: list
        anchors { top: fieldWrap.bottom; left: parent.left; right: parent.right
                  bottom: parent.bottom; leftMargin: 8; rightMargin: 8
                  topMargin: 6; bottomMargin: 8 }
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        keyNavigationEnabled: false
        Keys.onPressed: function (e) {
            if (e.key === Qt.Key_J || e.key === Qt.Key_Down) root.move(1)
            else if (e.key === Qt.Key_K || e.key === Qt.Key_Up) root.move(-1)
            else if (e.key === Qt.Key_Return || e.key === Qt.Key_Enter) root.openActive()
            else if (e.key === Qt.Key_N && query.text === "") root.openAdd()
            else return
            e.accepted = true
        }
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
        visible: !root.selected && !root.formOpen
        model: root.rows
        delegate: Item {
            width: list.width
            height: 58
            Rectangle {
                anchors.fill: parent
                anchors.margins: 2
                radius: Theme.radiusSmall
                color: index === root.active ? Theme.selection
                     : rowHover.hovered ? Theme.cardHover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Avatar {
                id: av
                anchors { left: parent.left; leftMargin: 12; verticalCenter: parent.verticalCenter }
                name: modelData.name || ""
                seed: modelData.emails && modelData.emails.length ? modelData.emails[0] : ""
            }
            Column {
                anchors {
                    left: av.right; leftMargin: 12
                    right: parent.right; rightMargin: 12
                    verticalCenter: parent.verticalCenter
                }
                spacing: 3
                Text {
                    width: parent.width
                    text: modelData.name || ""
                    elide: Text.ElideRight
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    font.weight: Font.DemiBold
                    color: Theme.textPrimary
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                Text {
                    width: parent.width
                    text: modelData.emails && modelData.emails.length ? modelData.emails[0]
                        : modelData.phones && modelData.phones.length ? modelData.phones[0] : ""
                    elide: Text.ElideRight
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                Text {
                    width: parent.width
                    visible: !!modelData.organisation
                    text: modelData.organisation || ""
                    elide: Text.ElideRight
                    font.family: Theme.fontFamily
                    font.pixelSize: 10
                    color: Theme.textDim
                    opacity: 0.8
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
            HoverHandler { id: rowHover; cursorShape: Qt.PointingHandCursor }
            TapHandler { onTapped: root.showDetail(modelData) }
        }
    }

    Text {
        anchors { top: fieldWrap.bottom; horizontalCenter: parent.horizontalCenter; topMargin: 24 }
        visible: !root.selected && !root.formOpen && root.rows.length === 0
        text: root.contacts.length === 0 ? "No contacts yet" : "No matches"
        font.family: Theme.fontFamily
        font.pixelSize: 12
        color: Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    // ---- detail view -------------------------------------------------------
    Flickable {
        id: detail
        anchors.fill: parent
        contentHeight: dcol.implicitHeight + 32
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        visible: root.selected !== null && !root.formOpen
        // Backspace steps back to the list (Esc goes through Main's chain,
        // which routes it here via esc()). E edits, D / Delete asks first, and
        // while it asks Return is Yes.
        Keys.onPressed: function (e) {
            if (e.key === Qt.Key_Backspace) { root.backToList(); e.accepted = true }
            else if (e.key === Qt.Key_E) { root.openEdit(); e.accepted = true }
            else if (e.key === Qt.Key_D || e.key === Qt.Key_Delete) { root.confirmDelete = true; e.accepted = true }
            else if ((e.key === Qt.Key_Return || e.key === Qt.Key_Enter) && root.confirmDelete) { root.dropYes(); e.accepted = true }
        }

        Column {
            id: dcol
            x: 16; y: 16
            width: detail.width - 32
            spacing: 8

            Item {
                width: parent.width; height: 28
                Text {
                    anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                    text: "\uf060"
                    font.family: Theme.fontFamily
                    font.pixelSize: 14
                    color: backHover.hovered ? Theme.accent : Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                HoverHandler { id: backHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: root.backToList() }
            }

            Text {
                width: parent.width
                text: root.selected ? root.selected.name || "" : ""
                wrapMode: Text.Wrap
                font.family: Theme.fontFamily
                font.pixelSize: 18
                font.weight: Font.DemiBold
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Text {
                width: parent.width
                visible: root.selected && !!root.selected.organisation
                text: root.selected ? root.selected.organisation || "" : ""
                wrapMode: Text.Wrap
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }

            SectionLabel {
                width: parent.width
                visible: root.selected && (root.selected.emails || []).length > 0
                text: "Emails"
                count: root.selected ? (root.selected.emails || []).length : 0
            }
            Repeater {
                model: root.selected ? root.selected.emails || [] : []
                delegate: Rectangle {
                    width: parent.width
                    height: 30
                    radius: Theme.radiusSmall
                    color: hHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        width: parent.width
                        anchors.verticalCenter: parent.verticalCenter
                        leftPadding: 8
                        spacing: 10
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: "\uf0e0"
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            width: parent.width - 72
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData
                            elide: Text.ElideRight
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    // A click writes to them; the icon on the right copies.
                    HoverHandler { id: hHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler {
                        onTapped: { win.composer().openMailto({ to: modelData }); win.composeOpen = true }
                    }
                    Text {
                        anchors { right: parent.right; rightMargin: 4; verticalCenter: parent.verticalCenter }
                        width: 26; height: 26
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                        text: "\uf0c5"
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        color: copyHover.hovered ? Theme.accent : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                        HoverHandler { id: copyHover; cursorShape: Qt.PointingHandCursor }
                        // ReleaseWithinBounds grabs the press, so the row's tap (compose) stays out of it.
                        TapHandler {
                            gesturePolicy: TapHandler.ReleaseWithinBounds
                            onTapped: { root.copy(modelData); win.flash("Copied") }
                        }
                    }
                }
            }

            SectionLabel {
                width: parent.width
                visible: root.selected && (root.selected.phones || []).length > 0
                text: "Phones"
                count: root.selected ? (root.selected.phones || []).length : 0
            }
            Repeater {
                model: root.selected ? root.selected.phones || [] : []
                delegate: Rectangle {
                    width: parent.width
                    height: 30
                    radius: Theme.radiusSmall
                    color: pHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        width: parent.width
                        anchors.verticalCenter: parent.verticalCenter
                        leftPadding: 8
                        spacing: 10
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: "\uf095"
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            width: parent.width - 42
                            anchors.verticalCenter: parent.verticalCenter
                            text: modelData
                            elide: Text.ElideRight
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: pHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: { root.copy(modelData); win.flash("Copied") } }
                }
            }

            SectionLabel {
                width: parent.width
                visible: root.selected && !!root.selected.note
                text: "Note"
            }
            Text {
                width: parent.width
                visible: root.selected && !!root.selected.note
                text: root.selected ? root.selected.note || "" : ""
                wrapMode: Text.Wrap
                textFormat: Text.PlainText
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }

            // Action row — Edit and Delete, or the confirmation that replaces
            // it in place (Return = Yes, Esc = No).
            Row {
                width: parent.width
                spacing: 10
                visible: !root.confirmDelete
                AppButton { text: "Edit"; kind: "ghost"; onClicked: root.openEdit() }
                AppButton { text: "Delete"; kind: "danger"; onClicked: root.confirmDelete = true }
            }
            Row {
                width: parent.width
                spacing: 10
                visible: root.confirmDelete
                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: "Delete " + (root.selected ? root.selected.name || "this contact" : "") + "?"
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    color: Theme.textPrimary
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                AppButton { text: "Yes"; kind: "danger"; onClicked: root.dropYes() }
                AppButton { text: "No"; kind: "ghost"; onClicked: root.confirmDelete = false }
            }
            Item { width: 1; height: 12 }
        }
    }

    // ---- form view (add / edit) -------------------------------------------
    component FormField: Column {
        id: ff
        property alias text: input.text
        property alias placeholderText: input.placeholderText
        property string label: ""
        property string hint: ""
        property var tabNext: null
        // A Column takes focus itself; hand it on so forceActiveFocus() on the
        // field lands in the input.
        onActiveFocusChanged: if (activeFocus) input.forceActiveFocus()
        width: parent.width
        spacing: 4
        Text {
            text: ff.label.toUpperCase()
            font.family: Theme.fontFamily
            font.pixelSize: 10
            font.weight: Font.DemiBold
            font.letterSpacing: 1.5
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Rectangle {
            width: parent.width
            height: 42
            radius: Theme.radiusSmall
            color: Theme.railBg
            border.width: 1
            border.color: input.activeFocus ? Theme.accent : Theme.hairline
            Behavior on color { ColorAnimation { duration: Theme.anim } }
            Behavior on border.color { ColorAnimation { duration: Theme.anim } }
            TextField {
                id: input
                anchors.fill: parent
                anchors.leftMargin: 12
                anchors.rightMargin: 12
                verticalAlignment: TextInput.AlignVCenter
                color: Theme.textPrimary
                placeholderTextColor: Theme.textDim
                font.family: Theme.fontFamily
                font.pixelSize: 13
                background: null
                selectByMouse: true
                Keys.onTabPressed: function (e) { if (ff.tabNext) ff.tabNext(); e.accepted = true }
            }
        }
        Text {
            width: parent.width
            visible: ff.hint !== ""
            text: ff.hint
            wrapMode: Text.Wrap
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Flickable {
        id: form
        anchors.fill: parent
        contentHeight: fcol.implicitHeight + 32
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        visible: root.formOpen

        Column {
            id: fcol
            x: 16; y: 16
            width: form.width - 32
            spacing: 8

            Item {
                width: parent.width; height: 28
                Text {
                    anchors { left: parent.left; verticalCenter: parent.verticalCenter }
                    text: "\uf060"
                    font.family: Theme.fontFamily
                    font.pixelSize: 14
                    color: fBackHover.hovered ? Theme.accent : Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                HoverHandler { id: fBackHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: root.cancelForm() }
            }

            Text {
                width: parent.width
                text: root.editing ? "Edit contact" : "New contact"
                font.family: Theme.fontFamily
                font.pixelSize: 18
                font.weight: Font.DemiBold
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }

            // In edit mode the addresses already on the card, read-only above
            // the add fields — the daemon never throws an address away.
            Repeater {
                model: root.editing ? root.editing.emails || [] : []
                delegate: Text {
                    width: fcol.width
                    text: "\uf0e0  " + modelData
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
            Repeater {
                model: root.editing ? root.editing.phones || [] : []
                delegate: Text {
                    width: fcol.width
                    text: "\uf095  " + modelData
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }

            FormField {
                id: f_name
                label: "Name"
                placeholderText: "Name"
                tabNext: function () { f_email.forceActiveFocus() }
            }
            FormField {
                id: f_email
                label: root.editing ? "Add email" : "Email"
                placeholderText: root.editing ? "Add email" : "Email"
                tabNext: function () { f_phone.forceActiveFocus() }
            }
            FormField {
                id: f_phone
                label: root.editing ? "Add phone" : "Phone"
                placeholderText: root.editing ? "Add phone" : "Phone"
                tabNext: function () { f_org.forceActiveFocus() }
            }
            FormField {
                id: f_org
                label: "Organisation"
                placeholderText: "Organisation"
                tabNext: function () { f_note.forceActiveFocus() }
                hint: root.editing && f_org.text === "" && (root.editing.organisation || "") !== ""
                      ? "can't be cleared, only changed" : ""
            }

            Text {
                text: "NOTE"
                font.family: Theme.fontFamily
                font.pixelSize: 10
                font.weight: Font.DemiBold
                font.letterSpacing: 1.5
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Rectangle {
                width: parent.width
                height: 84
                radius: Theme.radiusSmall
                color: Theme.railBg
                border.width: 1
                border.color: f_note.activeFocus ? Theme.accent : Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }
                TextArea {
                    id: f_note
                    anchors.fill: parent
                    anchors.margins: 8
                    wrapMode: TextArea.Wrap
                    color: Theme.textPrimary
                    placeholderTextColor: Theme.textDim
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    background: null
                    Keys.onTabPressed: function (e) { f_name.forceActiveFocus(); e.accepted = true }
                }
            }
            Text {
                width: parent.width
                visible: root.editing && f_note.text === "" && (root.editing.note || "") !== ""
                text: "can't be cleared, only changed"
                wrapMode: Text.Wrap
                font.family: Theme.fontFamily
                font.pixelSize: 10
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }

            Item { width: 1; height: 6 }
            Row {
                spacing: 10
                AppButton {
                    text: "Save"
                    kind: "primary"
                    active: !root.saving
                    onClicked: root.save()
                }
                AppButton { text: "Cancel"; kind: "ghost"; onClicked: root.cancelForm() }
            }
            Item { width: 1; height: 12 }
        }
    }

    // Ctrl+Return saves the form from any of its fields; Esc cancels (Main's
    // Esc chain routes here).
    Shortcut {
        sequences: ["Ctrl+Return", "Ctrl+Enter"]
        enabled: root.opened && root.formOpen && !win.composeOpen
        onActivated: root.save()
    }
}
