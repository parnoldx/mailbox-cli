import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

ScrollView {
    id: root
    contentWidth: availableWidth

    ListModel {
        id: screenerModel
        ListElement {
            sender: "newsletter@example.com"
            name: "Tech Daily"
            subject: "Industry news"
            date: "Today, 13:00"
            snippet: "Preview text."
            color: "#61AFEF"
            initials: "TC"
        }
        ListElement {
            sender: "billing@example.com"
            name: "Acme Hosting Inc"
            subject: "Your Invoice #INV-1000 is ready"
            date: "Today, 09:30"
            snippet: "Preview text."
            color: "#E06C75"
            initials: "HO"
        }
        ListElement {
            sender: "promotions@example.net"
            name: "Promo Mailer"
            subject: "Claim your exclusive voucher now!"
            date: "Aug 29"
            snippet: "Preview text."
            color: "#E5C07B"
            initials: "SD"
        }
    }

    ColumnLayout {
        width: Math.min(root.width - 48, 860)
        anchors.horizontalCenter: parent.horizontalCenter
        spacing: 20

        Item { height: 12 }

        // Header info
        RowLayout {
            Layout.fillWidth: true
            Text {
                text: "THE SCREENER"
                color: Theme.warning
                font.family: Theme.fontFamily
                font.pixelSize: 14
                font.bold: true
                font.letterSpacing: 1.5
            }
            Text {
                text: "· First-time senders awaiting a routing decision"
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

        // Empty state
        Text {
            visible: screenerModel.count === 0
            text: "🎉 All screened! No new senders waiting."
            color: Theme.dim
            font.family: Theme.fontFamily
            font.pixelSize: 16
            Layout.alignment: Qt.AlignHCenter
        }

        // Screener Cards
        Repeater {
            model: screenerModel

            delegate: Rectangle {
                Layout.fillWidth: true
                height: 140
                radius: Theme.radiusLarge
                color: Theme.surface
                border.color: Theme.border
                border.width: 1

                ColumnLayout {
                    anchors.fill: parent
                    anchors.margins: 16
                    spacing: 12

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: 12

                        Rectangle {
                            width: 38; height: 38
                            radius: 19
                            color: model.color
                            Text {
                                anchors.centerIn: parent
                                text: model.initials
                                color: "#181825"
                                font.pixelSize: 12
                                font.bold: true
                            }
                        }

                        ColumnLayout {
                            spacing: 2
                            Text {
                                text: model.name + " <" + model.sender + ">"
                                color: Theme.foreground
                                font.family: Theme.fontFamily
                                font.pixelSize: 14
                                font.bold: true
                            }
                            Text {
                                text: "First email: " + model.subject
                                color: Theme.dim
                                font.family: Theme.fontFamily
                                font.pixelSize: 12
                            }
                        }

                        Item { Layout.fillWidth: true }

                        Text {
                            text: model.date
                            color: Theme.dim
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                        }
                    }

                    Rectangle {
                        Layout.fillWidth: true
                        height: 1
                        color: Theme.border
                    }

                    // 4-Way Decision Action Buttons
                    RowLayout {
                        Layout.fillWidth: true
                        spacing: 10

                        AppButton {
                            text: "Inbox (I)"
                            iconText: "📥"
                            variant: "primary"
                            implicitHeight: 32
                            onClicked: {
                                if (mailboxClient) mailboxClient.routeSender(model.sender, "inbox")
                                screenerModel.remove(index)
                            }
                        }

                        AppButton {
                            text: "The Feed (F)"
                            iconText: "📰"
                            variant: "secondary"
                            implicitHeight: 32
                            onClicked: {
                                if (mailboxClient) mailboxClient.routeSender(model.sender, "feed")
                                screenerModel.remove(index)
                            }
                        }

                        AppButton {
                            text: "Paper Trail (L)"
                            iconText: "🧾"
                            variant: "secondary"
                            implicitHeight: 32
                            onClicked: {
                                if (mailboxClient) mailboxClient.routeSender(model.sender, "paper")
                                screenerModel.remove(index)
                            }
                        }

                        Item { Layout.fillWidth: true }

                        AppButton {
                            text: "Block Sender (B)"
                            iconText: "🚫"
                            variant: "danger"
                            implicitHeight: 32
                            onClicked: {
                                if (mailboxClient) mailboxClient.routeSender(model.sender, "block")
                                screenerModel.remove(index)
                            }
                        }
                    }
                }
            }
        }

        Item { height: 24 }
    }
}
