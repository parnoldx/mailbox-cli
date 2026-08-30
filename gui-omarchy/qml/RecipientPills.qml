import QtQuick
import QtQuick.Controls.Basic

// A tokenised recipient field: one pill per address, an inline input that
// autocompletes against the address book (`contact search`), and Backspace on an
// empty input eats the last pill. `recipients` is an array of {name, email};
// the display favours the name and falls back to the raw address.
Item {
    id: root
    property string label: "To"
    property var recipients: []
    signal changed()

    implicitHeight: Math.max(34, box.implicitHeight)

    function _norm(s) { return (s || "").trim().toLowerCase() }

    function addRecipient(name, email) {
        email = (email || "").trim()
        if (!email) return
        for (var i = 0; i < recipients.length; i++)
            if (root._norm(recipients[i].email) === root._norm(email)) return
        var next = recipients.slice()
        next.push({ name: (name || "").trim(), email: email })
        recipients = next
        root.changed()
    }
    // Accept "Name <a@b>", a bare address, or a comma/semicolon-separated run.
    function addRaw(text) {
        var parts = String(text || "").split(/[,;]/)
        for (var i = 0; i < parts.length; i++) {
            var t = parts[i].trim()
            if (!t) continue
            var m = t.match(/^"?(.*?)"?\s*<([^>]+)>$/)
            if (m) root.addRecipient(m[1], m[2])
            else root.addRecipient("", t)
        }
    }
    function removeAt(i) {
        if (i < 0 || i >= recipients.length) return
        var next = recipients.slice()
        next.splice(i, 1)
        recipients = next
        root.changed()
    }
    function focusInput() { input.forceActiveFocus() }

    property var suggestions: []
    property int sugActive: 0

    function refreshSuggestions() {
        var q = input.text.trim()
        if (q.length < 2) { root.suggestions = []; return }
        Mailbox.call(["contact", "search"], { positional: q, limit: 6 }, function (r) {
            if (input.text.trim() !== q) return
            var out = []
            var rows = (r.ok && r.data) ? r.data : []
            for (var i = 0; i < rows.length; i++) {
                var em = rows[i].emails || []
                for (var j = 0; j < em.length; j++) {
                    var lc = root._norm(em[j])
                    var dup = false
                    for (var k = 0; k < root.recipients.length; k++)
                        if (root._norm(root.recipients[k].email) === lc) dup = true
                    if (!dup) out.push({ name: rows[i].name || "", email: em[j] })
                }
            }
            root.suggestions = out.slice(0, 6)
            root.sugActive = 0
        })
    }
    function commitSuggestion(i) {
        var s = root.suggestions[i]
        if (!s) return
        root.addRecipient(s.name, s.email)
        input.text = ""
        root.suggestions = []
    }
    function commitInput() {
        if (root.suggestions.length > 0) { root.commitSuggestion(root.sugActive); return }
        if (input.text.trim().length > 0) { root.addRaw(input.text); input.text = "" }
    }

    Row {
        id: box
        width: parent.width
        spacing: 10

        Text {
            width: 34
            y: 8
            text: root.label
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }

        Flow {
            id: flow
            width: parent.width - 44
            spacing: 6

            Repeater {
                model: root.recipients
                Rectangle {
                    height: 24
                    width: pillRow.implicitWidth + 18
                    radius: 12
                    color: pillHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        id: pillRow
                        anchors.verticalCenter: parent.verticalCenter
                        anchors.left: parent.left
                        anchors.leftMargin: 9
                        spacing: 7
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: (modelData.name && modelData.name.length > 0) ? modelData.name : modelData.email
                            font.family: Theme.fontFamily
                            font.pixelSize: 11
                            font.weight: (modelData.name && modelData.name.length > 0) ? Font.DemiBold : Font.Normal
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            anchors.verticalCenter: parent.verticalCenter
                            text: "\uf00d"
                            font.family: Theme.fontFamily
                            font.pixelSize: 9
                            color: rmHover.hovered ? Theme.red : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                            HoverHandler { id: rmHover; cursorShape: Qt.PointingHandCursor }
                            TapHandler { onTapped: root.removeAt(index) }
                        }
                    }
                    HoverHandler { id: pillHover }
                }
            }

            TextField {
                id: input
                // A fixed width keeps the Flow layout out of a width⇄x loop; it
                // wraps to its own line when the pills fill the row.
                width: 180
                height: 26
                placeholderText: root.recipients.length === 0 ? "name or address" : ""
                color: Theme.textPrimary
                placeholderTextColor: Theme.textDim
                font.family: Theme.fontFamily
                font.pixelSize: 12
                background: null
                leftPadding: 2
                onTextChanged: root.refreshSuggestions()
                Keys.onPressed: function (e) {
                    if ((e.key === Qt.Key_Backspace) && input.text.length === 0 && root.recipients.length > 0) {
                        root.removeAt(root.recipients.length - 1); e.accepted = true
                    } else if (e.key === Qt.Key_Down && root.suggestions.length > 0) {
                        root.sugActive = Math.min(root.suggestions.length - 1, root.sugActive + 1); e.accepted = true
                    } else if (e.key === Qt.Key_Up && root.suggestions.length > 0) {
                        root.sugActive = Math.max(0, root.sugActive - 1); e.accepted = true
                    } else if (e.key === Qt.Key_Escape && root.suggestions.length > 0) {
                        root.suggestions = []; e.accepted = true
                    }
                }
                Keys.onReturnPressed: root.commitInput()
                Keys.onEnterPressed: root.commitInput()
                Keys.onTabPressed: function (e) {
                    if (root.suggestions.length > 0 || input.text.trim().length > 0) { root.commitInput(); e.accepted = true }
                    else e.accepted = false
                }
                // On blur, only keep what was actually typed — do not silently
                // pick a highlighted suggestion (that is Enter's / Tab's job and
                // would race a click on a different row).
                onActiveFocusChanged: if (!activeFocus) {
                    if (input.text.trim().length > 0) { root.addRaw(input.text); input.text = "" }
                    root.suggestions = []
                }
            }
        }
    }

    // Autocomplete list. A Popup, so it renders in the window overlay above the
    // subject and the editor rather than being painted over by them.
    Popup {
        id: pop
        parent: root
        x: 44
        y: box.implicitHeight + 4
        width: Math.min(360, root.width - 44)
        height: sugCol.implicitHeight + 8
        padding: 0
        visible: root.suggestions.length > 0 && input.activeFocus
        closePolicy: Popup.NoAutoClose
        focus: false

        background: Rectangle {
            color: Theme.railBg
            radius: Theme.radiusSmall
            border.width: 1
            border.color: Theme.hairline
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }

        contentItem: Column {
            id: sugCol
            width: parent ? parent.width : pop.width
            padding: 4
            Repeater {
                model: root.suggestions
                Rectangle {
                    width: sugCol.width - 8
                    height: 34
                    radius: 6
                    color: index === root.sugActive ? Theme.selection
                         : sHover.hovered ? Theme.cardHover : "transparent"
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Column {
                        anchors.verticalCenter: parent.verticalCenter
                        anchors.left: parent.left
                        anchors.leftMargin: 10
                        spacing: 1
                        Text {
                            text: (modelData.name && modelData.name.length > 0) ? modelData.name : modelData.email
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            color: Theme.textPrimary
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            visible: modelData.name && modelData.name.length > 0
                            text: modelData.email
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: sHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: root.commitSuggestion(index) }
                }
            }
        }
    }
}
