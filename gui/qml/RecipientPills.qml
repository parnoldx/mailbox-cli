import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Rectangle {
    id: root
    height: Math.max(46, pillsFlow.implicitHeight + 14)
    color: Theme.surface
    radius: Theme.radiusSmall
    border.color: Theme.border
    border.width: 1

    property string label: "To:"
    // Array of objects: [{ name: "Max Mustermann", email: "max@example.org" }, ...]
    property var recipients: []
    property bool showCcBccToggle: true
    signal toggleCcBcc()

    function getRecipientsString() {
        var out = []
        for (var i = 0; i < recipients.length; i++) {
            var r = recipients[i]
            if (r.name && r.name !== r.email) {
                out.push(r.name + " <" + r.email + ">")
            } else {
                out.push(r.email)
            }
        }
        return out.join(", ")
    }

    function addRecipient(name, email) {
        if (!email && name) {
            // Check if name is in "Name <email>" format
            var match = name.match(/^(.*?)\s*<([^>]+)>$/)
            if (match) {
                name = match[1].trim()
                email = match[2].trim()
            } else {
                email = name.trim()
                name = ""
            }
        }
        if (!email || email.trim() === "") return

        var list = recipients.slice()
        // Check duplicates by email
        for (var i = 0; i < list.length; i++) {
            if (list[i].email.toLowerCase() === email.toLowerCase()) {
                inputField.text = ""
                suggestionPopup.close()
                return
            }
        }

        list.push({
            name: name ? name.trim() : "",
            email: email.trim()
        })
        recipients = list
        inputField.text = ""
        suggestionPopup.close()
    }

    function removeRecipient(idx) {
        var list = recipients.slice()
        list.splice(idx, 1)
        recipients = list
    }

    RowLayout {
        anchors.fill: parent
        anchors.leftMargin: 12
        anchors.rightMargin: 12
        spacing: 10

        Text {
            text: root.label
            color: Theme.dim
            font.family: Theme.fontFamily
            font.pixelSize: 13
            font.bold: true
        }

        Flow {
            id: pillsFlow
            Layout.fillWidth: true
            spacing: 6
            Layout.alignment: Qt.AlignVCenter

            Repeater {
                model: root.recipients

                Rectangle {
                    height: 28
                    // Show name if present, otherwise email
                    width: pillRow.implicitWidth + 18
                    radius: 14
                    color: Theme.surfaceAlt
                    border.color: Theme.accent
                    border.width: 1

                    RowLayout {
                        id: pillRow
                        anchors.centerIn: parent
                        spacing: 6

                        Text {
                            text: modelData.name && modelData.name !== "" ? modelData.name : modelData.email
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            font.bold: modelData.name !== ""
                        }

                        // Remove button
                        Rectangle {
                            width: 16; height: 16
                            radius: 8
                            color: removeMouse.containsMouse ? Theme.surfaceHover : "transparent"

                            Text {
                                anchors.centerIn: parent
                                text: "✕"
                                color: removeMouse.containsMouse ? Theme.urgent : Theme.dim
                                font.pixelSize: 9
                                font.bold: true
                            }

                            MouseArea {
                                id: removeMouse
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: root.removeRecipient(index)
                            }
                        }
                    }
                }
            }

            TextField {
                id: inputField
                implicitWidth: 160
                height: 28
                placeholderText: root.recipients.length === 0 ? "Type name or email address..." : ""
                placeholderTextColor: Theme.dim
                color: Theme.foreground
                font.family: Theme.fontFamily
                font.pixelSize: 13
                background: null

                onTextChanged: {
                    if (contactSuggestModel) {
                        contactSuggestModel.filter(text)
                        if (text.trim().length > 0 && !suggestionPopup.opened) {
                            suggestionPopup.open()
                        }
                    }
                }

                Keys.onReturnPressed: {
                    if (suggestionList.count > 0 && suggestionPopup.opened) {
                        var cEmail = contactSuggestModel.data(contactSuggestModel.index(0, 0), Qt.UserRole + 2) // email
                        var cName = contactSuggestModel.data(contactSuggestModel.index(0, 0), Qt.UserRole + 1)  // name
                        root.addRecipient(cName.toString(), cEmail.toString())
                    } else if (text.trim() !== "") {
                        root.addRecipient("", text)
                    }
                }

                Keys.onDownPressed: {
                    if (suggestionPopup.opened) {
                        suggestionList.forceActiveFocus()
                    }
                }
            }
        }

        AppButton {
            visible: root.showCcBccToggle
            text: "+ Cc / Bcc"
            variant: "ghost"
            implicitHeight: 28
            font.pixelSize: 11
            onClicked: root.toggleCcBcc()
        }
    }

    // Auto-complete suggestion popup
    Popup {
        id: suggestionPopup
        x: 40
        y: root.height + 4
        width: 340
        height: Math.min(240, suggestionList.contentHeight + 16)
        padding: 6
        background: Rectangle {
            color: Theme.surface
            radius: Theme.radiusMedium
            border.color: Theme.border
            border.width: 1
        }

        ListView {
            id: suggestionList
            anchors.fill: parent
            model: contactSuggestModel
            clip: true
            spacing: 2

            delegate: Rectangle {
                width: suggestionList.width
                height: 42
                radius: Theme.radiusSmall
                color: suggHover.containsMouse ? Theme.surfaceHover : "transparent"

                MouseArea {
                    id: suggHover
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: {
                        root.addRecipient(model.name, model.email)
                    }
                }

                RowLayout {
                    anchors.fill: parent
                    anchors.margins: 8
                    spacing: 10

                    Rectangle {
                        width: 26; height: 26
                        radius: 13
                        color: model.avatarColor || Theme.accent
                        Text {
                            anchors.centerIn: parent
                            text: model.initials || "PA"
                            color: "#181825"
                            font.pixelSize: 11
                            font.bold: true
                        }
                    }

                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 1
                        Text {
                            text: model.name
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            font.bold: true
                        }
                        Text {
                            text: model.email
                            color: Theme.dim
                            font.family: Theme.fontFamily
                            font.pixelSize: 11
                            elide: Text.ElideRight
                        }
                    }
                }
            }
        }
    }
}
