import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

ScrollView {
    id: root
    contentWidth: availableWidth

    signal openMessage(int index, var messageData)

    ColumnLayout {
        width: Math.min(root.width - 48, 860)
        anchors.horizontalCenter: parent.horizontalCenter
        spacing: 24

        Item { height: 12 }

        // Header info
        RowLayout {
            Layout.fillWidth: true
            Text {
                text: "THE FEED"
                color: Theme.accent
                font.family: Theme.fontFamily
                font.pixelSize: 14
                font.bold: true
                font.letterSpacing: 1.5
            }
            Text {
                text: "· Newsletters & reading material (marked read on arrival)"
                color: Theme.dim
                font.family: Theme.fontFamily
                font.pixelSize: 12
            }
            Rectangle {
                Layout.fillWidth: true
                height: 1
                color: Theme.border
            }
        }

        // Magazine / RSS Feed Stream
        Repeater {
            model: messageListModel

            delegate: Rectangle {
                Layout.fillWidth: true
                implicitHeight: cardContent.implicitHeight + 36
                radius: Theme.radiusLarge
                color: Theme.surface
                border.color: Theme.border
                border.width: 1

                ColumnLayout {
                    id: cardContent
                    anchors.fill: parent
                    anchors.margins: 20
                    spacing: 14

                    // Publication Header
                    RowLayout {
                        Layout.fillWidth: true
                        spacing: 12

                        Rectangle {
                            width: 36; height: 36
                            radius: 18
                            color: model.avatarColor || Theme.accent
                            Text {
                                anchors.centerIn: parent
                                text: model.initials || "NW"
                                color: "#181825"
                                font.pixelSize: 12
                                font.bold: true
                            }
                        }

                        ColumnLayout {
                            spacing: 2
                            Text {
                                text: model.fromName || model.fromEmail
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 14
                                font.bold: true
                            }
                            Text {
                                text: model.fromEmail
                                color: Theme.dim
                                font.family: Theme.fontFamily
                                font.pixelSize: 11
                            }
                        }

                        Item { Layout.fillWidth: true }

                        Text {
                            text: model.date
                            color: Theme.dim
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                        }

                        // Set Aside button
                        AppButton {
                            text: "Read Later"
                            iconText: "⏳"
                            variant: "ghost"
                            implicitHeight: 28
                            font.pixelSize: 11
                            onClicked: messageListModel.setAside(index)
                        }
                    }

                    // Large Subject
                    Text {
                        text: model.subject
                        color: Theme.foreground
                        font.family: Theme.fontFamily
                        font.pixelSize: 18
                        font.bold: true
                        Layout.fillWidth: true
                        wrapMode: Text.Wrap
                    }

                    // Rendered excerpt / Body preview
                    Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: excerptText.implicitHeight + 20
                        radius: Theme.radiusMedium
                        color: Theme.surfaceAlt

                        Text {
                            id: excerptText
                            anchors.fill: parent
                            anchors.margins: 14
                            text: model.bodyHtml ? model.bodyHtml : model.snippet
                            textFormat: Text.RichText
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 14
                            lineHeight: 1.4
                            wrapMode: Text.Wrap
                        }
                    }

                    // Read Full Article Link
                    RowLayout {
                        Layout.fillWidth: true
                        spacing: 10

                        AppButton {
                            text: "Open Full View ➔"
                            variant: "secondary"
                            implicitHeight: 32
                            onClicked: {
                                root.openMessage(index, messageListModel.getMessage(index))
                            }
                        }

                        Item { Layout.fillWidth: true }

                        AppButton {
                            text: "Trash"
                            iconText: "🗑"
                            variant: "danger"
                            implicitHeight: 32
                            onClicked: messageListModel.removeMessage(index)
                        }
                    }
                }
            }
        }

        Item { height: 24 }
    }
}
