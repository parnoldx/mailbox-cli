import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtWebEngine

Item {
    id: root

    property var messageData: ({})
    property bool originalStyle: false

    signal backRequested()
    signal replyRequested(var messageData)

    onMessageDataChanged: {
        if (pixelBlockInterceptor) {
            pixelBlockInterceptor.resetCount()
        }
        loadHtmlContent()
    }

    onOriginalStyleChanged: {
        loadHtmlContent()
    }

    function loadHtmlContent() {
        if (!messageData || !messageData.body_html) return
        var css = themeBridge ? themeBridge.generateEmailCss(root.originalStyle) : ""
        var fullHtml = "<!DOCTYPE html><html><head><meta charset='utf-8'><style>" + css + "</style></head><body>"
                     + messageData.body_html
                     + "</body></html>"
        webViewer.loadHtml(fullHtml, "https://email.local")
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Sticky Top Action Bar with sleek AppButtons
        Rectangle {
            Layout.fillWidth: true
            height: 56
            color: Theme.surface
            border.color: Theme.border
            border.width: 1

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 16
                anchors.rightMargin: 16
                spacing: 10

                AppButton {
                    text: "Back"
                    iconText: "←"
                    variant: "secondary"
                    implicitHeight: 34
                    onClicked: root.backRequested()
                }

                Item { Layout.fillWidth: true }

                // PixelBlock Tracker Stripping Badge
                Rectangle {
                    visible: pixelBlockInterceptor && pixelBlockInterceptor.blockedCount > 0
                    width: trackerText.implicitWidth + 20
                    height: 32
                    radius: Theme.radiusSmall
                    color: Theme.surfaceAlt
                    border.color: Theme.success

                    RowLayout {
                        anchors.centerIn: parent
                        spacing: 6
                        Text { text: "🛡️"; font.pixelSize: 12 }
                        Text {
                            id: trackerText
                            text: (pixelBlockInterceptor ? pixelBlockInterceptor.blockedCount : 1) + " tracker blocked"
                            color: Theme.success
                            font.family: Theme.fontFamily
                            font.pixelSize: 11
                            font.bold: true
                        }
                    }
                }

                // Toggle Original Style Switch
                AppButton {
                    text: root.originalStyle ? "Theme View" : "Original Style"
                    iconText: "🎨"
                    variant: root.originalStyle ? "primary" : "secondary"
                    implicitHeight: 34
                    onClicked: root.originalStyle = !root.originalStyle
                }

                AppButton {
                    text: "Aside"
                    iconText: "⏳"
                    variant: "secondary"
                    implicitHeight: 34
                    onClicked: {
                        if (mailboxClient && root.messageData.id) {
                            mailboxClient.setAside(root.messageData.id)
                        }
                        root.backRequested()
                    }
                }

                AppButton {
                    text: "Trash"
                    iconText: "🗑"
                    variant: "danger"
                    implicitHeight: 34
                    onClicked: {
                        if (mailboxClient && root.messageData.id) {
                            mailboxClient.moveToTrash(root.messageData.id)
                        }
                        root.backRequested()
                    }
                }

                AppButton {
                    text: "Reply"
                    iconText: "↩"
                    variant: "primary"
                    pillShape: true
                    implicitHeight: 34
                    implicitWidth: 96
                    onClicked: root.replyRequested(root.messageData)
                }
            }
        }

        // Scrollable Message Content Area
        ScrollView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentWidth: availableWidth

            ColumnLayout {
                width: Math.min(root.width - 48, 860)
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: 18

                Item { height: 8 }

                // 1. Large Bold Subject Headline (Pure HEY style)
                Text {
                    text: root.messageData.subject || "No Subject"
                    color: Theme.foreground
                    font.family: Theme.fontFamily
                    font.pixelSize: 26
                    font.bold: true
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }

                // 2. Uncluttered Sender & Recipient Header
                Rectangle {
                    Layout.fillWidth: true
                    height: 64
                    radius: Theme.radiusMedium
                    color: Theme.surface
                    border.color: Theme.border

                    RowLayout {
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 14

                        // Sender Avatar
                        Rectangle {
                            width: 40; height: 40
                            radius: 20
                            color: root.messageData.avatar_color || Theme.accent
                            Text {
                                anchors.centerIn: parent
                                text: root.messageData.initials || "PA"
                                color: "#181825"
                                font.family: Theme.fontFamily
                                font.pixelSize: 14
                                font.bold: true
                            }
                        }

                        // Clean Two-line Address block
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 2

                            Text {
                                text: (root.messageData.from_name ? root.messageData.from_name + " " : "") + "<" + (root.messageData.from_email || "") + ">"
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 14
                                font.bold: true
                            }

                            Text {
                                text: "to " + (root.messageData.to_name || "Peter") + " <" + (root.messageData.to_email || "you@example.com") + ">"
                                color: Theme.dim
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                            }
                        }

                        // Relative timestamp (No "Date:" prefix)
                        Text {
                            text: root.messageData.date || "Today"
                            color: Theme.dim
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                        }
                    }
                }

                // 3. Attachment Tray with Drag-Out
                ColumnLayout {
                    visible: root.messageData && root.messageData.has_attachments ? true : false
                    Layout.fillWidth: true
                    spacing: 8

                    Text {
                        text: "ATTACHMENTS (" + (root.messageData.attachments_count || 1) + ")"
                        color: Theme.dim
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        font.bold: true
                        font.letterSpacing: 1
                    }

                    RowLayout {
                        spacing: 10

                        Rectangle {
                            width: 220; height: 44
                            radius: Theme.radiusSmall
                            color: Theme.surfaceAlt
                            border.color: Theme.border

                            RowLayout {
                                anchors.fill: parent
                                anchors.margins: 8
                                spacing: 8
                                Text { text: "📄"; font.pixelSize: 16 }
                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 1
                                    Text {
                                        text: "document_spec.pdf"
                                        color: Theme.foreground
                                        font.pixelSize: 12
                                        font.bold: true
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        text: "142 KB"
                                        color: Theme.dim
                                        font.pixelSize: 10
                                    }
                                }
                                Text { text: "⬇️"; font.pixelSize: 12 }
                            }
                        }

                        Rectangle {
                            width: 200; height: 44
                            radius: Theme.radiusSmall
                            color: Theme.surfaceAlt
                            border.color: Theme.border

                            RowLayout {
                                anchors.fill: parent
                                anchors.margins: 8
                                spacing: 8
                                Text { text: "🖼️"; font.pixelSize: 16 }
                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 1
                                    Text {
                                        text: "architecture.png"
                                        color: Theme.foreground
                                        font.pixelSize: 12
                                        font.bold: true
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        text: "48 KB"
                                        color: Theme.dim
                                        font.pixelSize: 10
                                    }
                                }
                                Text { text: "⬇️"; font.pixelSize: 12 }
                            }
                        }
                    }
                }

                // 4. HTML Message Body in WebEngineView
                Rectangle {
                    Layout.fillWidth: true
                    height: 480
                    radius: Theme.radiusMedium
                    color: root.originalStyle ? "#FFFFFF" : Theme.surface
                    border.color: Theme.border
                    clip: true

                    WebEngineView {
                        id: webViewer
                        anchors.fill: parent
                        backgroundColor: root.originalStyle ? "#FFFFFF" : Theme.surface
                    }
                }

                Item { height: 24 }
            }
        }
    }
}
