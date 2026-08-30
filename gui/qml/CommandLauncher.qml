import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Rectangle {
    id: root
    visible: false
    anchors.fill: parent
    color: "#80000000"

    signal navigateTo(string destination)
    signal closeRequested()

    function open() {
        visible = true
        searchInput.text = ""
        searchInput.forceActiveFocus()
    }

    function close() {
        visible = false
        closeRequested()
    }

    MouseArea {
        anchors.fill: parent
        onClicked: root.close()
    }

    // Centered modal card
    Rectangle {
        id: dialogCard
        width: Math.min(parent.width - 48, 560)
        height: Math.min(parent.height - 80, 520)
        anchors.centerIn: parent
        color: Theme.surface
        radius: Theme.radiusLarge
        border.color: Theme.border
        border.width: 1

        MouseArea {
            anchors.fill: parent
        }

        ColumnLayout {
            anchors.fill: parent
            anchors.margins: 20
            spacing: 16

            // Search Header
            RowLayout {
                Layout.fillWidth: true
                spacing: 12

                Text {
                    text: "🔍"
                    font.pixelSize: 18
                }

                TextField {
                    id: searchInput
                    Layout.fillWidth: true
                    placeholderText: "Go to a person, place, or label... (1-6 for quick jump)"
                    placeholderTextColor: Theme.dim
                    color: Theme.foreground
                    font.family: Theme.fontFamily
                    font.pixelSize: 16
                    background: null

                    onTextChanged: {
                        filterItems(text)
                    }

                    Keys.onEscapePressed: root.close()

                    Keys.onPressed: function(event) {
                        if (event.key === Qt.Key_1) {
                            root.navigateTo("inbox"); root.close(); event.accepted = true;
                        } else if (event.key === Qt.Key_2) {
                            root.navigateTo("feed"); root.close(); event.accepted = true;
                        } else if (event.key === Qt.Key_3) {
                            root.navigateTo("paper"); root.close(); event.accepted = true;
                        } else if (event.key === Qt.Key_4) {
                            root.navigateTo("reply_later"); root.close(); event.accepted = true;
                        } else if (event.key === Qt.Key_5) {
                            root.navigateTo("aside"); root.close(); event.accepted = true;
                        } else if (event.key === Qt.Key_6) {
                            root.navigateTo("bubble_up"); root.close(); event.accepted = true;
                        } else if (event.key === Qt.Key_Down) {
                            primaryList.currentIndex = (primaryList.currentIndex + 1) % primaryList.count
                            event.accepted = true
                        } else if (event.key === Qt.Key_Up) {
                            primaryList.currentIndex = (primaryList.currentIndex - 1 + primaryList.count) % primaryList.count
                            event.accepted = true
                        } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
                            var currentItem = primaryList.model.get(primaryList.currentIndex)
                            if (currentItem) {
                                root.navigateTo(currentItem.dest)
                                root.close()
                            }
                            event.accepted = true
                        }
                    }
                }

                Rectangle {
                    width: 32; height: 24
                    radius: Theme.radiusSmall
                    color: Theme.surfaceAlt
                    border.color: Theme.border
                    Text {
                        anchors.centerIn: parent
                        text: "ESC"
                        color: Theme.dim
                        font.pixelSize: 10
                        font.bold: true
                    }
                }
            }

            Rectangle {
                Layout.fillWidth: true
                height: 1
                color: Theme.border
            }

            // Primary Destinations (1-6) - Clean titles without subtitles
            Text {
                text: "PRIMARY DESTINATIONS"
                color: Theme.dim
                font.pixelSize: 11
                font.bold: true
                font.letterSpacing: 1
            }

            ListModel {
                id: primaryModel
                ListElement { key: "1"; name: "Inbox"; icon: "📥"; dest: "inbox"; badge: "2" }
                ListElement { key: "2"; name: "The Feed"; icon: "📰"; dest: "feed"; badge: "" }
                ListElement { key: "3"; name: "Paper Trail"; icon: "🧾"; dest: "paper"; badge: "" }
                ListElement { key: "4"; name: "Reply Later"; icon: "↩️"; dest: "reply_later"; badge: "1" }
                ListElement { key: "5"; name: "Set Aside"; icon: "⏳"; dest: "aside"; badge: "" }
                ListElement { key: "6"; name: "Bubble Up"; icon: "🫧"; dest: "bubble_up"; badge: "" }
            }

            ListView {
                id: primaryList
                Layout.fillWidth: true
                Layout.fillHeight: true
                model: primaryModel
                clip: true
                spacing: 4

                delegate: Rectangle {
                    width: primaryList.width
                    height: 42
                    radius: Theme.radiusMedium
                    color: (primaryList.currentIndex === index || hoverArea.containsMouse) ? Theme.surfaceHover : "transparent"

                    MouseArea {
                        id: hoverArea
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: {
                            root.navigateTo(model.dest)
                            root.close()
                        }
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 12
                        anchors.rightMargin: 12
                        spacing: 12

                        // Numbered hotkey badge (1-6)
                        Rectangle {
                            width: 24; height: 24
                            radius: Theme.radiusSmall
                            color: Theme.surfaceAlt
                            border.color: Theme.border
                            Text {
                                anchors.centerIn: parent
                                text: model.key
                                color: Theme.accent
                                font.pixelSize: 12
                                font.bold: true
                            }
                        }

                        Text {
                            text: model.icon
                            font.pixelSize: 16
                        }

                        Text {
                            text: model.name
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 14
                            font.bold: true
                            Layout.fillWidth: true
                        }

                        // Badge if any
                        Rectangle {
                            visible: model.badge !== ""
                            width: 20; height: 20
                            radius: 10
                            color: Theme.accent
                            Text {
                                anchors.centerIn: parent
                                text: model.badge
                                color: "#181825"
                                font.pixelSize: 11
                                font.bold: true
                            }
                        }
                    }
                }
            }

            Rectangle {
                Layout.fillWidth: true
                height: 1
                color: Theme.border
            }

            // Secondary Destinations
            Text {
                text: "ALL DESTINATIONS & LABELS"
                color: Theme.dim
                font.pixelSize: 11
                font.bold: true
                font.letterSpacing: 1
            }

            Flow {
                Layout.fillWidth: true
                spacing: 8

                Repeater {
                    model: [
                        { name: "Drafts", dest: "drafts", icon: "📝" },
                        { name: "Sent", dest: "sent", icon: "🚀" },
                        { name: "Screener", dest: "screener", icon: "❓" },
                        { name: "Contacts", dest: "contacts", icon: "👥" },
                        { name: "Everything", dest: "everything", icon: "🌐" },
                        { name: "Trash", dest: "trash", icon: "🗑" },
                        { name: "Spam", dest: "spam", icon: "🚫" }
                    ]

                    Rectangle {
                        width: tagText.implicitWidth + 24
                        height: 28
                        radius: Theme.radiusSmall
                        color: secondaryHover.containsMouse ? Theme.surfaceHover : Theme.surfaceAlt
                        border.color: Theme.border

                        MouseArea {
                            id: secondaryHover
                            anchors.fill: parent
                            hoverEnabled: true
                            onClicked: {
                                root.navigateTo(modelData.dest)
                                root.close()
                            }
                        }

                        RowLayout {
                            anchors.centerIn: parent
                            spacing: 6
                            Text { text: modelData.icon; font.pixelSize: 12 }
                            Text {
                                id: tagText
                                text: modelData.name
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                            }
                        }
                    }
                }
            }
        }
    }

    function filterItems(query) {
        if (query.trim() === "") {
            primaryList.currentIndex = 0
            return
        }
        for (var i = 0; i < primaryModel.count; i++) {
            var item = primaryModel.get(i)
            if (item.name.toLowerCase().indexOf(query.toLowerCase()) !== -1) {
                primaryList.currentIndex = i
                break
            }
        }
    }
}
