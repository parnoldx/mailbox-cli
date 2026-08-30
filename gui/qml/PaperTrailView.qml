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
        spacing: 16

        Item { height: 12 }

        // Header info
        RowLayout {
            Layout.fillWidth: true
            Text {
                text: "PAPER TRAIL"
                color: Theme.accent
                font.family: Theme.fontFamily
                font.pixelSize: 14
                font.bold: true
                font.letterSpacing: 1.5
            }
            Text {
                text: "· Receipts, confirmations & bills (filed automatically)"
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

        // Receipt Ledger Stack
        Repeater {
            model: messageListModel

            delegate: Rectangle {
                Layout.fillWidth: true
                height: 80
                radius: Theme.radiusMedium
                color: cardHover.containsMouse ? Theme.surfaceHover : Theme.surface
                border.color: Theme.border
                border.width: 1

                MouseArea {
                    id: cardHover
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

                    // Receipt badge icon
                    Rectangle {
                        width: 40; height: 40
                        radius: Theme.radiusSmall
                        color: Theme.surfaceAlt
                        border.color: Theme.border
                        Text {
                            anchors.centerIn: parent
                            text: "🧾"
                            font.pixelSize: 18
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
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }

                        Text {
                            text: model.snippet
                            color: Theme.dim
                            font.family: Theme.monospaceFont
                            font.pixelSize: 11
                            elide: Text.ElideRight
                            Layout.fillWidth: true
                        }
                    }

                    // Attachment indicator (e.g. PDF receipt)
                    Rectangle {
                        visible: model.hasAttachments
                        width: 72; height: 28
                        radius: Theme.radiusSmall
                        color: Theme.surfaceAlt
                        border.color: Theme.border
                        RowLayout {
                            anchors.centerIn: parent
                            spacing: 4
                            Text { text: "📄"; font.pixelSize: 11 }
                            Text { text: "PDF"; color: Theme.accent; font.pixelSize: 11; font.bold: true }
                        }
                    }
                }
            }
        }

        Item { height: 24 }
    }
}
