import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Rectangle {
    id: root
    visible: sendManager && sendManager.isSending
    width: Math.min(parent.width - 48, 640)
    height: (sendManager && sendManager.hasForgottenAttachment) ? 96 : 58
    radius: Theme.radiusMedium
    color: Theme.surface
    border.color: (sendManager && sendManager.hasForgottenAttachment) ? Theme.warning : Theme.accent
    border.width: 1.5

    anchors.horizontalCenter: parent.horizontalCenter
    anchors.bottom: parent.bottom
    anchors.bottomMargin: 24

    signal undoRequested()

    Behavior on y { NumberAnimation { duration: Theme.animNormal; easing.type: Easing.OutQuad } }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 12
        spacing: 6

        // Primary Row: Sending status + Undo button
        RowLayout {
            Layout.fillWidth: true
            spacing: 12

            Text {
                text: "✉️"
                font.pixelSize: 18
            }

            ColumnLayout {
                Layout.fillWidth: true
                spacing: 2

                Text {
                    text: "Sending to " + (sendManager ? sendManager.pendingRecipient : "") + " in " + (sendManager ? sendManager.secondsRemaining : 5) + "s..."
                    color: Theme.foreground
                    font.family: Theme.fontFamily
                    font.pixelSize: 14
                    font.bold: true
                }

                Text {
                    text: sendManager ? sendManager.pendingSubject : ""
                    color: Theme.dim
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    elide: Text.ElideRight
                    Layout.fillWidth: true
                }
            }

            Button {
                text: "↩ Undo"
                highlighted: true
                font.family: Theme.fontFamily
                font.bold: true
                onClicked: {
                    if (sendManager) sendManager.cancelSend()
                    root.undoRequested()
                }
            }
        }

        // Secondary Row: Forgotten Attachment Warning
        RowLayout {
            visible: sendManager && sendManager.hasForgottenAttachment
            Layout.fillWidth: true
            spacing: 8

            Text {
                text: "⚠️"
                font.pixelSize: 14
            }

            Text {
                text: "You mentioned an attachment, but none is attached. Undo to attach."
                color: Theme.warning
                font.family: Theme.fontFamily
                font.pixelSize: 12
                font.bold: true
                Layout.fillWidth: true
            }
        }

        // Progress countdown bar
        Rectangle {
            Layout.fillWidth: true
            height: 3
            radius: 1.5
            color: Theme.surfaceAlt

            Rectangle {
                height: parent.height
                width: parent.width * (sendManager ? sendManager.progress : 0)
                radius: 1.5
                color: (sendManager && sendManager.hasForgottenAttachment) ? Theme.warning : Theme.accent
            }
        }
    }
}
