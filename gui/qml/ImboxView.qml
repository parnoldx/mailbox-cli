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

        Item { height: 12 } // Top padding

        // Section: "New for you" (Unread)
        ColumnLayout {
            Layout.fillWidth: true
            spacing: 12

            RowLayout {
                Layout.fillWidth: true
                Text {
                    text: "NEW FOR YOU"
                    color: Theme.accent
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    font.bold: true
                    font.letterSpacing: 1.2
                }
                Rectangle {
                    Layout.fillWidth: true
                    height: 1
                    color: Theme.border
                }
            }

            Repeater {
                model: messageListModel

                delegate: Rectangle {
                    visible: !model.seen
                    Layout.fillWidth: true
                    height: visible ? 84 : 0
                    radius: Theme.radiusMedium
                    color: cardHover.containsMouse ? Theme.surfaceHover : Theme.surface
                    border.color: Theme.border
                    border.width: 1

                    Behavior on color { ColorAnimation { duration: Theme.animFast } }

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

                        // Sender Avatar
                        Rectangle {
                            width: 42; height: 42
                            radius: 21
                            color: model.avatarColor || Theme.accent
                            Text {
                                anchors.centerIn: parent
                                text: model.initials || "PA"
                                color: "#181825"
                                font.family: Theme.fontFamily
                                font.pixelSize: 14
                                font.bold: true
                            }
                        }

                        // Message details
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 4

                            RowLayout {
                                Layout.fillWidth: true
                                spacing: 8

                                Text {
                                    text: model.fromName || model.fromEmail
                                    color: Theme.foreground
                                    font.family: Theme.fontFamily
                                    font.pixelSize: 14
                                    font.bold: true
                                }

                                Item { Layout.fillWidth: true }

                                // Attachments icon if present
                                Text {
                                    visible: model.hasAttachments
                                    text: "📎 " + (model.attachmentsCount > 1 ? model.attachmentsCount : "")
                                    color: Theme.dim
                                    font.pixelSize: 11
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
                                font.pixelSize: 14
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

                        // Hover Action Buttons
                        RowLayout {
                            visible: cardHover.containsMouse
                            spacing: 6

                            Rectangle {
                                width: 30; height: 30
                                radius: Theme.radiusSmall
                                color: Theme.surfaceAlt
                                border.color: Theme.border
                                ToolTip.visible: asideBtnArea.containsMouse
                                ToolTip.text: "Set Aside"
                                Text { anchors.centerIn: parent; text: "⏳"; font.pixelSize: 14 }
                                MouseArea {
                                    id: asideBtnArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    onClicked: messageListModel.setAside(index)
                                }
                            }

                            Rectangle {
                                width: 30; height: 30
                                radius: Theme.radiusSmall
                                color: Theme.surfaceAlt
                                border.color: Theme.border
                                ToolTip.visible: trashBtnArea.containsMouse
                                ToolTip.text: "Trash"
                                Text { anchors.centerIn: parent; text: "🗑"; font.pixelSize: 14 }
                                MouseArea {
                                    id: trashBtnArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    onClicked: messageListModel.removeMessage(index)
                                }
                            }
                        }
                    }
                }
            }
        }

        // Section: "Previously seen" (Read)
        ColumnLayout {
            Layout.fillWidth: true
            spacing: 12

            RowLayout {
                Layout.fillWidth: true
                Text {
                    text: "PREVIOUSLY SEEN"
                    color: Theme.dim
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    font.bold: true
                    font.letterSpacing: 1.2
                }
                Rectangle {
                    Layout.fillWidth: true
                    height: 1
                    color: Theme.border
                }
            }

            Repeater {
                model: messageListModel

                delegate: Rectangle {
                    visible: model.seen
                    Layout.fillWidth: true
                    height: visible ? 64 : 0
                    radius: Theme.radiusMedium
                    color: prevHover.containsMouse ? Theme.surfaceHover : "transparent"
                    border.color: Theme.border
                    border.width: 1
                    opacity: 0.82

                    MouseArea {
                        id: prevHover
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            root.openMessage(index, messageListModel.getMessage(index))
                        }
                    }

                    RowLayout {
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 12

                        Rectangle {
                            width: 32; height: 32
                            radius: 16
                            color: model.avatarColor || Theme.dim
                            opacity: 0.8
                            Text {
                                anchors.centerIn: parent
                                text: model.initials || "PA"
                                color: "#181825"
                                font.family: Theme.fontFamily
                                font.pixelSize: 11
                                font.bold: true
                            }
                        }

                        Text {
                            text: model.fromName || model.fromEmail
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            Layout.preferredWidth: 150
                            elide: Text.ElideRight
                        }

                        Text {
                            text: model.subject
                            color: Theme.foreground
                            font.family: Theme.fontFamily
                            font.pixelSize: 13
                            Layout.fillWidth: true
                            elide: Text.ElideRight
                        }

                        Text {
                            text: model.date
                            color: Theme.dim
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                        }
                    }
                }
            }
        }

        Item { height: 24 } // Bottom padding
    }
}
