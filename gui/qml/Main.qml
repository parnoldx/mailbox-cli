import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtQuick.Window

ApplicationWindow {
    id: window
    width: 1060
    height: 760
    minimumWidth: 720
    minimumHeight: 540
    visible: true
    title: "Mailbox"
    color: Theme.background

    property string currentView: "inbox" // "inbox" | "feed" | "paper" | "reply_later" | "aside" | "bubble_up" | "screener" | "search" | "detail" | "compose"
    property var activeMessageData: ({})

    function navigate(dest) {
        if (dest === "inbox" || dest === "feed" || dest === "paper" || dest === "reply_later" || dest === "aside" || dest === "bubble_up") {
            if (messageListModel) messageListModel.setCurrentBucket(dest)
            currentView = dest
        } else if (dest === "screener") {
            currentView = "screener"
        } else if (dest === "search" || dest === "everything") {
            currentView = "search"
        } else if (dest === "write" || dest === "compose") {
            currentView = "compose"
        }
    }

    // Global Key Handlers
    Item {
        anchors.fill: parent
        focus: true

        Keys.onPressed: function(event) {
            if (commandLauncher.visible) return;

            if (event.key === Qt.Key_H) {
                commandLauncher.open(); event.accepted = true;
            } else if (event.key === Qt.Key_Slash) {
                window.navigate("search"); event.accepted = true;
            } else if (event.key === Qt.Key_C) {
                window.navigate("compose"); event.accepted = true;
            } else if (event.key === Qt.Key_1) {
                window.navigate("inbox"); event.accepted = true;
            } else if (event.key === Qt.Key_2) {
                window.navigate("feed"); event.accepted = true;
            } else if (event.key === Qt.Key_3) {
                window.navigate("paper"); event.accepted = true;
            } else if (event.key === Qt.Key_4) {
                window.navigate("reply_later"); event.accepted = true;
            } else if (event.key === Qt.Key_5) {
                window.navigate("aside"); event.accepted = true;
            } else if (event.key === Qt.Key_6) {
                window.navigate("bubble_up"); event.accepted = true;
            } else if (event.key === Qt.Key_Escape) {
                if (currentView !== "inbox") {
                    window.navigate("inbox"); event.accepted = true;
                }
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        spacing: 0

        // Sleek Top Application Bar
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
                spacing: 12

                // Command Launcher Trigger Button
                AppButton {
                    text: "Menu"
                    iconText: "✱"
                    variant: "secondary"
                    implicitHeight: 36
                    onClicked: commandLauncher.open()
                }

                // Screener Button (with count badge)
                AppButton {
                    visible: mailboxClient ? mailboxClient.screenerCount > 0 : true
                    text: "Screener (" + (mailboxClient ? mailboxClient.screenerCount : 3) + ")"
                    iconText: "❓"
                    variant: "warning"
                    implicitHeight: 36
                    onClicked: window.navigate("screener")
                }

                Item { Layout.fillWidth: true }

                // Current View Title
                Text {
                    text: currentView === "inbox" ? "📥 Inbox" :
                          currentView === "feed" ? "📰 The Feed" :
                          currentView === "paper" ? "🧾 Paper Trail" :
                          currentView === "reply_later" ? "↩️ Reply Later" :
                          currentView === "aside" ? "⏳ Set Aside" :
                          currentView === "screener" ? "❓ Screener" :
                          currentView === "search" ? "🔍 Advanced Search" :
                          currentView === "compose" ? "✏️ Write Email" : "✉️ Message"
                    color: Theme.foreground
                    font.family: Theme.fontFamily
                    font.pixelSize: 16
                    font.bold: true
                }

                Item { Layout.fillWidth: true }

                // Search Trigger
                AppButton {
                    text: "Search (/)"
                    iconText: "🔍"
                    variant: "ghost"
                    implicitHeight: 36
                    onClicked: window.navigate("search")
                }

                // Primary Write Email Button
                AppButton {
                    text: "Write"
                    iconText: "✏️"
                    variant: "primary"
                    pillShape: true
                    implicitHeight: 36
                    implicitWidth: 96
                    onClicked: window.navigate("compose")
                }
            }
        }

        // View Area
        StackLayout {
            id: viewStack
            Layout.fillWidth: true
            Layout.fillHeight: true
            currentIndex: currentView === "inbox" || currentView === "reply_later" || currentView === "aside" ? 0 :
                          currentView === "feed" ? 1 :
                          currentView === "paper" ? 2 :
                          currentView === "screener" ? 3 :
                          currentView === "search" ? 4 :
                          currentView === "detail" ? 5 :
                          currentView === "compose" ? 6 : 0

            // 0: Inbox / Reply Later / Set Aside view
            ImboxView {
                onOpenMessage: function(index, data) {
                    window.activeMessageData = data
                    window.currentView = "detail"
                }
            }

            // 1: The Feed View
            FeedView {
                onOpenMessage: function(index, data) {
                    window.activeMessageData = data
                    window.currentView = "detail"
                }
            }

            // 2: Paper Trail View
            PaperTrailView {
                onOpenMessage: function(index, data) {
                    window.activeMessageData = data
                    window.currentView = "detail"
                }
            }

            // 3: Screener Deck
            ScreenerDeck {}

            // 4: Dedicated Search View with Left Filter Sidebar
            SearchView {
                onOpenMessage: function(index, data) {
                    window.activeMessageData = data
                    window.currentView = "detail"
                }
                onCloseRequested: window.navigate("inbox")
            }

            // 5: Message Detail View
            MessageDetailView {
                messageData: window.activeMessageData
                onBackRequested: window.navigate(messageListModel ? messageListModel.currentBucket : "inbox")
                onReplyRequested: function(msg) {
                    window.currentView = "compose"
                }
            }

            // 6: Composer View
            ComposerView {
                onCloseRequested: window.navigate("inbox")
            }
        }
    }

    // Floating 5-second Delayed Send & Undo Toast
    SendUndoToast {
        id: sendToast
        onUndoRequested: {
            window.currentView = "compose"
        }
    }

    // Command Launcher Modal Overlay
    CommandLauncher {
        id: commandLauncher
        onNavigateTo: function(destination) {
            window.navigate(destination)
        }
    }
}
