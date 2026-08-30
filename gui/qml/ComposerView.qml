import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtWebEngine

Item {
    id: root

    property string replySubject: ""
    property string replyTo: ""
    property string initialBody: ""
    property bool showCcBcc: false
    // Array of objects: [{ name: "invoice.pdf", size: "142 KB", icon: "📄" }, ...]
    property var attachedFiles: []

    signal closeRequested()

    Component.onCompleted: {
        if (replyTo !== "") {
            toPills.addRecipient("", replyTo)
        }
        if (replySubject !== "") {
            subjectInput.text = replySubject.startsWith("Re:") ? replySubject : "Re: " + replySubject
        }
    }

    function addAttachment(fileName, sizeStr, icon) {
        var list = attachedFiles.slice()
        list.push({
            name: fileName,
            size: sizeStr || "48 KB",
            icon: icon || (fileName.endsWith(".pdf") ? "📄" : (fileName.endsWith(".png") || fileName.endsWith(".jpg")) ? "🖼️" : "📎")
        })
        attachedFiles = list
    }

    function removeAttachment(idx) {
        var list = attachedFiles.slice()
        list.splice(idx, 1)
        attachedFiles = list
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 20
        spacing: 12

        // Top Header
        RowLayout {
            Layout.fillWidth: true

            Text {
                text: "Write Email"
                color: Theme.foreground
                font.family: Theme.fontFamily
                font.pixelSize: 22
                font.bold: true
            }

            Item { Layout.fillWidth: true }

            AppButton {
                text: "Close"
                iconText: "✕"
                variant: "ghost"
                onClicked: root.closeRequested()
            }
        }

        // To Field
        RecipientPills {
            id: toPills
            Layout.fillWidth: true
            label: "To:"
            showCcBccToggle: true
            onToggleCcBcc: root.showCcBcc = !root.showCcBcc
        }

        // Optional Cc / Bcc Fields
        ColumnLayout {
            visible: root.showCcBcc
            Layout.fillWidth: true
            spacing: 8

            RecipientPills {
                id: ccPills
                Layout.fillWidth: true
                label: "Cc:"
                showCcBccToggle: false
            }

            RecipientPills {
                id: bccPills
                Layout.fillWidth: true
                label: "Bcc:"
                showCcBccToggle: false
            }
        }

        // Subject Line
        Rectangle {
            Layout.fillWidth: true
            height: 44
            color: Theme.surface
            radius: Theme.radiusSmall
            border.color: Theme.border
            border.width: 1

            RowLayout {
                anchors.fill: parent
                anchors.leftMargin: 12
                anchors.rightMargin: 12
                spacing: 8

                Text {
                    text: "Subject:"
                    color: Theme.dim
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
                    font.bold: true
                }

                TextField {
                    id: subjectInput
                    Layout.fillWidth: true
                    placeholderText: "Subject..."
                    placeholderTextColor: Theme.dim
                    color: Theme.foreground
                    font.family: Theme.fontFamily
                    font.pixelSize: 14
                    font.bold: true
                    background: null
                }
            }
        }

        // Lexxy Rich-Text / Markdown Editor in WebEngineView
        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            color: Theme.surface
            radius: Theme.radiusMedium
            border.color: Theme.border
            border.width: 1
            clip: true

            WebEngineView {
                id: lexxyEditor
                anchors.fill: parent
                url: "qrc:/assets/lexxy_editor.html"
                backgroundColor: Theme.surface
            }

            // Drop Area for attachments
            DropArea {
                anchors.fill: parent
                onEntered: drag.accept()
                onDropped: function(drop) {
                    if (drop.hasUrls) {
                        for (var i = 0; i < drop.urls.length; i++) {
                            var u = drop.urls[i].toString()
                            var fname = u.substring(u.lastIndexOf('/') + 1)
                            root.addAttachment(fname, "120 KB", "")
                        }
                        drop.acceptProposedAction()
                    }
                }
            }
        }

        // Visual Attachment Tray (Chips like in email viewer with ✕ remove button)
        ColumnLayout {
            Layout.fillWidth: true
            spacing: 6

            RowLayout {
                Layout.fillWidth: true
                spacing: 8

                Text {
                    text: "ATTACHMENTS (" + root.attachedFiles.length + ")"
                    color: Theme.dim
                    font.family: Theme.fontFamily
                    font.pixelSize: 11
                    font.bold: true
                    font.letterSpacing: 1
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    text: "Attach File"
                    iconText: "📎"
                    variant: "ghost"
                    implicitHeight: 26
                    font.pixelSize: 11
                    onClicked: {
                        root.addAttachment("document_spec.pdf", "142 KB", "📄")
                    }
                }
            }

            Flow {
                Layout.fillWidth: true
                spacing: 8

                Repeater {
                    model: root.attachedFiles

                    Rectangle {
                        height: 40
                        width: attachRow.implicitWidth + 24
                        radius: Theme.radiusSmall
                        color: Theme.surfaceAlt
                        border.color: Theme.border
                        border.width: 1

                        RowLayout {
                            id: attachRow
                            anchors.fill: parent
                            anchors.leftMargin: 10
                            anchors.rightMargin: 8
                            spacing: 8

                            Text {
                                text: modelData.icon || "📎"
                                font.pixelSize: 15
                            }

                            ColumnLayout {
                                spacing: 1
                                Text {
                                    text: modelData.name
                                    color: Theme.foreground
                                    font.family: Theme.fontFamily
                                    font.pixelSize: 12
                                    font.bold: true
                                }
                                Text {
                                    text: modelData.size
                                    color: Theme.dim
                                    font.family: Theme.fontFamily
                                    font.pixelSize: 10
                                }
                            }

                            // ✕ Remove Button
                            Rectangle {
                                width: 18; height: 18
                                radius: 9
                                color: removeAttachArea.containsMouse ? Theme.surfaceHover : "transparent"

                                Text {
                                    anchors.centerIn: parent
                                    text: "✕"
                                    color: removeAttachArea.containsMouse ? Theme.urgent : Theme.dim
                                    font.pixelSize: 10
                                    font.bold: true
                                }

                                MouseArea {
                                    id: removeAttachArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: root.removeAttachment(index)
                                }
                            }
                        }
                    }
                }
            }
        }

        // Bottom Action Bar
        Rectangle {
            Layout.fillWidth: true
            height: 52
            color: "transparent"

            RowLayout {
                anchors.fill: parent
                spacing: 12

                AppButton {
                    text: "Send Message"
                    iconText: "✉️"
                    variant: "primary"
                    pillShape: true
                    implicitHeight: 40
                    implicitWidth: 140
                    font.pixelSize: 14

                    onClicked: {
                        var to = toPills.getRecipientsString()
                        var cc = ccPills.getRecipientsString()
                        var bcc = bccPills.getRecipientsString()
                        var subject = subjectInput.text

                        lexxyEditor.runJavaScript("window.getText()", function(bodyText) {
                            var attachNames = []
                            for (var i = 0; i < root.attachedFiles.length; i++) {
                                attachNames.push(root.attachedFiles[i].name)
                            }
                            if (sendManager) {
                                sendManager.scheduleSend(to, cc, bcc, subject, bodyText || "", attachNames)
                            }
                            root.closeRequested()
                        })
                    }
                }

                AppButton {
                    text: "Save Draft"
                    variant: "secondary"
                    implicitHeight: 40
                    onClicked: {
                        lexxyEditor.runJavaScript("window.getHtml()", function(bodyHtml) {
                            if (mailboxClient) {
                                mailboxClient.saveDraft(toPills.getRecipientsString(), subjectInput.text, bodyHtml || "")
                            }
                            root.closeRequested()
                        })
                    }
                }

                Item { Layout.fillWidth: true }

                AppButton {
                    text: "Discard"
                    iconText: "🗑"
                    variant: "danger"
                    implicitHeight: 40
                    onClicked: root.closeRequested()
                }
            }
        }
    }
}
