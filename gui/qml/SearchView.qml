import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Item {
    id: root

    property string searchKeyword: ""
    property string selectedScope: "all" // "all" | "inbox" | "feed" | "paper" | "aside" | "trash"
    property string filterFrom: ""
    property string filterTo: ""
    property bool filterHasAttachments: false
    property bool filterUnreadOnly: false
    property string filterDateRange: "any" // "any" | "7d" | "30d" | "1y"

    signal openMessage(int index, var messageData)
    signal closeRequested()

    function performSearch() {
        if (messageListModel) {
            messageListModel.setSearchQuery(searchKeyword)
        }
    }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // 1. Left Filter Sidebar
        Rectangle {
            Layout.preferredWidth: 260
            Layout.fillHeight: true
            color: Theme.surface
            border.color: Theme.border
            border.width: 1

            ScrollView {
                anchors.fill: parent
                contentWidth: availableWidth

                ColumnLayout {
                    width: parent.width - 24
                    anchors.horizontalCenter: parent.horizontalCenter
                    spacing: 16

                    Item { height: 8 }

                    RowLayout {
                        Layout.fillWidth: true
                        Text {
                            text: "SEARCH FILTERS"
                            color: Theme.dim
                            font.family: Theme.fontFamily
                            font.pixelSize: 11
                            font.bold: true
                            font.letterSpacing: 1.2
                        }
                        Item { Layout.fillWidth: true }
                        AppButton {
                            text: "Reset"
                            variant: "ghost"
                            implicitHeight: 24
                            font.pixelSize: 11
                            onClicked: {
                                searchKeyword = ""
                                searchField.text = ""
                                selectedScope = "all"
                                fromField.text = ""
                                toField.text = ""
                                filterHasAttachments = false
                                filterUnreadOnly = false
                                filterDateRange = "any"
                                performSearch()
                            }
                        }
                    }

                    Rectangle { Layout.fillWidth: true; height: 1; color: Theme.border }

                    // Scope / Folder
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 6

                        Text {
                            text: "Search In"
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            font.bold: true
                        }

                        ComboBox {
                            Layout.fillWidth: true
                            model: ["All Mail", "Inbox", "The Feed", "Paper Trail", "Set Aside", "Trash"]
                            currentIndex: 0
                            onActivated: function(index) {
                                var scopes = ["all", "inbox", "feed", "paper", "aside", "trash"]
                                root.selectedScope = scopes[index]
                                root.performSearch()
                            }
                            background: Rectangle {
                                color: Theme.surfaceAlt
                                radius: Theme.radiusSmall
                                border.color: Theme.border
                            }
                        }
                    }

                    // From Sender Filter
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 6

                        Text {
                            text: "From"
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            font.bold: true
                        }

                        Rectangle {
                            Layout.fillWidth: true
                            height: 34
                            radius: Theme.radiusSmall
                            color: Theme.surfaceAlt
                            border.color: Theme.border

                            TextField {
                                id: fromField
                                anchors.fill: parent
                                anchors.margins: 6
                                placeholderText: "sender@example.com"
                                placeholderTextColor: Theme.dim
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                                background: null
                                onTextChanged: {
                                    root.filterFrom = text
                                    root.performSearch()
                                }
                            }
                        }
                    }

                    // To Recipient Filter
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 6

                        Text {
                            text: "To"
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            font.bold: true
                        }

                        Rectangle {
                            Layout.fillWidth: true
                            height: 34
                            radius: Theme.radiusSmall
                            color: Theme.surfaceAlt
                            border.color: Theme.border

                            TextField {
                                id: toField
                                anchors.fill: parent
                                anchors.margins: 6
                                placeholderText: "recipient@example.com"
                                placeholderTextColor: Theme.dim
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                                background: null
                                onTextChanged: {
                                    root.filterTo = text
                                    root.performSearch()
                                }
                            }
                        }
                    }

                    // Checkboxes: Has attachments & Unread only
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 8

                        CheckBox {
                            text: "Has attachments"
                            checked: root.filterHasAttachments
                            onToggled: {
                                root.filterHasAttachments = checked
                                root.performSearch()
                            }
                        }

                        CheckBox {
                            text: "Unread messages only"
                            checked: root.filterUnreadOnly
                            onToggled: {
                                root.filterUnreadOnly = checked
                                root.performSearch()
                            }
                        }
                    }

                    // Date Range
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 6

                        Text {
                            text: "Date Range"
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            font.bold: true
                        }

                        ComboBox {
                            Layout.fillWidth: true
                            model: ["Any time", "Past 24 hours", "Past 7 days", "Past 30 days", "Past year"]
                            currentIndex: 0
                            onActivated: function(index) {
                                var ranges = ["any", "24h", "7d", "30d", "1y"]
                                root.filterDateRange = ranges[index]
                                root.performSearch()
                            }
                            background: Rectangle {
                                color: Theme.surfaceAlt
                                radius: Theme.radiusSmall
                                border.color: Theme.border
                            }
                        }
                    }

                    Item { height: 16 }
                }
            }
        }

        // 2. Right / Main Search Area
        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // Top Search Input Header Bar
            Rectangle {
                Layout.fillWidth: true
                height: 60
                color: Theme.surface
                border.color: Theme.border
                border.width: 1

                RowLayout {
                    anchors.fill: parent
                    anchors.leftMargin: 20
                    anchors.rightMargin: 20
                    spacing: 12

                    Text {
                        text: "🔍"
                        font.pixelSize: 18
                    }

                    Rectangle {
                        Layout.fillWidth: true
                        height: 38
                        radius: Theme.radiusSmall
                        color: Theme.surfaceAlt
                        border.color: Theme.border

                        RowLayout {
                            anchors.fill: parent
                            anchors.leftMargin: 12
                            anchors.rightMargin: 8
                            spacing: 8

                            TextField {
                                id: searchField
                                Layout.fillWidth: true
                                placeholderText: "Search in messages, subjects, senders, text..."
                                placeholderTextColor: Theme.dim
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 14
                                background: null
                                focus: true

                                onTextChanged: {
                                    root.searchKeyword = text
                                    root.performSearch()
                                }
                            }

                            AppButton {
                                visible: searchField.text !== ""
                                text: "Clear"
                                iconText: "✕"
                                variant: "ghost"
                                implicitHeight: 26
                                onClicked: {
                                    searchField.text = ""
                                    root.searchKeyword = ""
                                    root.performSearch()
                                }
                            }
                        }
                    }

                    AppButton {
                        text: "Back"
                        iconText: "←"
                        variant: "secondary"
                        onClicked: root.closeRequested()
                    }
                }
            }

            // Results List
            ScrollView {
                Layout.fillWidth: true
                Layout.fillHeight: true
                contentWidth: availableWidth

                ColumnLayout {
                    width: Math.min(parent.width - 48, 860)
                    anchors.horizontalCenter: parent.horizontalCenter
                    spacing: 12

                    Item { height: 8 }

                    Text {
                        text: "SEARCH RESULTS (" + messageListModel.count + ")"
                        color: Theme.dim
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        font.bold: true
                        font.letterSpacing: 1.2
                    }

                    Repeater {
                        model: messageListModel

                        delegate: Rectangle {
                            Layout.fillWidth: true
                            height: 78
                            radius: Theme.radiusMedium
                            color: resultHover.containsMouse ? Theme.surfaceHover : Theme.surface
                            border.color: Theme.border
                            border.width: 1

                            MouseArea {
                                id: resultHover
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: {
                                    root.openMessage(index, messageListModel.getMessage(index))
                                }
                            }

                            RowLayout {
                                anchors.fill: parent
                                anchors.margins: 14
                                spacing: 14

                                Rectangle {
                                    width: 38; height: 38
                                    radius: 19
                                    color: model.avatarColor || Theme.accent
                                    Text {
                                        anchors.centerIn: parent
                                        text: model.initials || "PA"
                                        color: "#0f1f28"
                                        font.family: Theme.fontFamily
                                        font.pixelSize: 13
                                        font.bold: true
                                    }
                                }

                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 3

                                    RowLayout {
                                        Layout.fillWidth: true
                                        Text {
                                            text: model.fromName || model.fromEmail
                                            color: Theme.foreground
                                            font.family: Theme.fontFamily
                                            font.pixelSize: 14
                                            font.bold: true
                                        }
                                        Item { Layout.fillWidth: true }
                                        Text {
                                            visible: model.hasAttachments
                                            text: "📎"
                                            font.pixelSize: 12
                                        }
                                        Text {
                                            text: model.date
                                            color: Theme.dim
                                            font.family: Theme.fontFamily
                                            font.pixelSize: 12
                                        }
                                    }

                                    Text {
                                        text: model.subject
                                        color: Theme.foreground
                                        font.family: Theme.fontFamily
                                        font.pixelSize: 13
                                        font.bold: true
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }

                                    Text {
                                        text: model.snippet
                                        color: Theme.dim
                                        font.family: Theme.fontFamily
                                        font.pixelSize: 12
                                        elide: Text.ElideRight
                                        Layout.fillWidth: true
                                    }
                                }
                            }
                        }
                    }

                    Item { height: 24 }
                }
            }
        }
    }
}
