import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import Quickshell
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Panel.qml — Dropdown popup panel for Mailbox email notifications and screening.
//
// Features:
// - New for you (unread) vs Previously seen tabs
// - Dedicated in-panel Screener with 1-click triage (Inbox, Block, Trash)
// - Sender initials in deterministic colored avatars
// - Account filtering with per-account unread badges
// - Keyboard navigation (S, U, P, I, T, B, R, N, arrows, Enter, Esc)
// - Flip settings page for open command, toast alerts, and bar visibility
Panel {
  id: root
  moduleName: "mailbox.email"
  ipcTarget: "mailbox.email"
  manageIpc: false

  property var anchorItem: null
  property var hostWidget: null
  readonly property var barIdentity: hostWidget || root

  property var externalService: null
  readonly property var service: externalService || localService

  MailboxService {
    id: localService
    settings: root.settings
  }

  property int selectedIndex: 0
  property bool cursorActive: false
  property double nowMs: Date.now()
  property string accountFilter: ""
  property string tabFilter: "unread" // "unread" | "previous" | "screener"
  property bool settingsOpen: false

  readonly property color foreground: bar ? bar.barForeground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color dim: Qt.darker(foreground, 1.55)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  readonly property var filteredMessages: Model.filterMessages(service.messages, accountFilter, tabFilter)
  readonly property var screenerCards: Model.screenerCards(service.screenerList, avatarPalette.length)

  readonly property var accountDropdownOptions: {
    var opts = Model.accountFilterOptions(service.accounts)
    var out = []
    for (var i = 0; i < opts.length; i++) {
      var count = 0
      for (var j = 0; j < service.messages.length; j++) {
        var m = service.messages[j]
        if (!m.seen && (opts[i].value === "" || String(m.account) === opts[i].value)) count++
      }
      out.push({
        value: opts[i].value,
        label: count > 0 ? opts[i].label + " (" + count + ")" : opts[i].label
      })
    }
    return out
  }

  readonly property var avatarPalette: [
    "#E06C75", "#98C379", "#E5C07B", "#61AFEF",
    "#C678DD", "#56B6C2", "#D19A66", "#4EC9B0"
  ]

  function avatarColor(item) {
    if (!item) return Color.accent
    var idx = typeof item.colorIndex === "number"
      ? item.colorIndex
      : Model.avatarColorIndex(item.address || item.from || item.name, avatarPalette.length)
    return avatarPalette[idx % avatarPalette.length]
  }

  property int phraseIndex: 0
  readonly property var loadingPhrases: [
    "Checking the mirror…",
    "Fetching new mail…",
    "Syncing with server…",
    "Screening new senders…"
  ]
  readonly property bool rotatingPhrases: service.refreshing

  readonly property string heroStatusText: {
    if (service.actionStatus !== "") return service.actionStatus
    if (service.lastError !== "") return service.lastError
    if (rotatingPhrases) return loadingPhrases[phraseIndex % loadingPhrases.length]
    if (service.screenerCount > 0 && service.unreadCount > 0) {
      return service.unreadCount + " UNREAD  ·  " + service.screenerCount + " TO SCREEN"
    }
    if (service.screenerCount > 0) return service.screenerCount + " TO SCREEN"
    if (service.unreadCount > 0) return service.unreadCount + " UNREAD"
    return "ALL CAUGHT UP"
  }

  function resetSelection() {
    selectedIndex = 0
    cursorActive = false
    pointerGate.reset()
    if (panelFlick) panelFlick.contentY = 0
  }

  function setTab(tab) {
    tabFilter = tab
    resetSelection()
  }

  function setAccount(acc) {
    accountFilter = String(acc || "")
    resetSelection()
  }

  function persistSettings(values) {
    var entry = { id: root.moduleName }
    for (var existing in root.settings) {
      if (existing !== "id") entry[existing] = root.settings[existing]
    }
    for (var key in values) {
      if (values[key] === undefined) delete entry[key]
      else entry[key] = values[key]
    }
    root.settings = entry
    // Push the merged entry back to the bar widget too, so the live service
    // (bound to the widget's settings, not the panel's) picks up the change
    // without waiting for a shell reload. Matches the first-party plugins.
    if (root.hostWidget && "settings" in root.hostWidget) root.hostWidget.settings = entry
    if (root.bar && root.bar.shell && typeof root.bar.shell.updateEntryInline === "function") {
      root.bar.shell.updateEntryInline(root.moduleName, entry)
    }
  }

  function openMail(item) {
    if (!item) return
    service.setSeen(item.id, true)
    var cmd = Model.buildOpenCommand(item.id)
    if (root.bar && typeof root.bar.run === "function") {
      root.bar.run(cmd)
    } else {
      // Detached login shell: survives the panel closing on the next line and
      // gets the same PATH (~/.local/bin) the desktop client is installed to.
      Quickshell.execDetached(["bash", "-lc", cmd])
    }
    root.close()
  }

  function moveSelection(delta) {
    var count = tabFilter === "screener" ? screenerCards.length : filteredMessages.length
    if (count === 0) return
    if (!cursorActive) {
      selectedIndex = 0
      cursorActive = true
      return
    }
    selectedIndex = Math.max(0, Math.min(count - 1, selectedIndex + delta))
    scrollSelectionIntoView()
  }

  function activateSelection() {
    if (tabFilter === "screener") {
      if (selectedIndex >= 0 && selectedIndex < screenerCards.length) {
        // Default action on screener item enter: route to inbox
        service.routeSender(screenerCards[selectedIndex].address, "inbox")
      }
    } else {
      if (selectedIndex >= 0 && selectedIndex < filteredMessages.length) {
        openMail(filteredMessages[selectedIndex])
      }
    }
  }

  function routeSelected(destination) {
    if (tabFilter !== "screener") return
    if (selectedIndex >= 0 && selectedIndex < screenerCards.length) {
      var item = screenerCards[selectedIndex]
      service.routeSender(item.address, destination)
    }
  }

  function trashSelected() {
    if (tabFilter === "screener") {
      if (selectedIndex >= 0 && selectedIndex < screenerCards.length) {
        var card = screenerCards[selectedIndex]
        service.trashMessage(card.id)
      }
      return
    }
    if (selectedIndex >= 0 && selectedIndex < filteredMessages.length) {
      var item = filteredMessages[selectedIndex]
      service.trashMessage(item.id)
    }
  }

  function setAsideSelected() {
    if (tabFilter === "screener") return
    if (selectedIndex >= 0 && selectedIndex < filteredMessages.length) {
      var item = filteredMessages[selectedIndex]
      service.setAside(item.id)
    }
  }

  function markSeenSelected() {
    if (tabFilter === "screener") return
    if (selectedIndex >= 0 && selectedIndex < filteredMessages.length) {
      var item = filteredMessages[selectedIndex]
      service.setSeen(item.id, !item.seen)
    }
  }

  function scrollSelectionIntoView() {
    if (!listColumn || selectedIndex < 0 || selectedIndex >= listColumn.children.length) return
    var wrapper = listColumn.children[selectedIndex]
    Qt.callLater(function() {
      if (!wrapper || !panelFlick) return
      var point = wrapper.mapToItem(panelFlick.contentItem, 0, 0)
      var margin = Style.space(8)
      var top = point.y
      var bottom = top + wrapper.height
      var viewTop = panelFlick.contentY
      var viewBottom = viewTop + panelFlick.height
      var maxY = Math.max(0, panelFlick.contentHeight - panelFlick.height)
      if (top < viewTop + margin) panelFlick.contentY = Math.max(0, top - margin)
      else if (bottom > viewBottom - margin) panelFlick.contentY = Math.min(maxY, bottom + margin - panelFlick.height)
    })
  }

  onOpenedChanged: if (opened) {
    cursorActive = false
    nowMs = Date.now()
    if (panelFlick) panelFlick.contentY = 0
    service.refresh()
    Qt.callLater(function() { keyCatcher.forceActiveFocus() })
  }

  PointerMoveGate {
    id: pointerGate
    referenceItem: panelFlick
  }

  Timer {
    id: phraseTimer
    interval: 1800
    running: root.opened && root.rotatingPhrases
    repeat: true
    onTriggered: root.phraseIndex = (root.phraseIndex + 1) % root.loadingPhrases.length
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(440))
    contentHeight: panel.fittedContentHeight(fixedHeader.implicitHeight + scrollContainer.implicitHeight + Style.space(16), Style.space(620))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      blocked: accountDropdown.popupOpen
      onMoveRequested: function(dx, dy) {
        if (dy !== 0) root.moveSelection(dy)
      }
      onActivateRequested: root.activateSelection()
      onCloseRequested: {
        if (root.settingsOpen) root.settingsOpen = false
        else root.close()
      }
      onTextKey: function(text) {
        var t = String(text || "").toLowerCase()
        if (t === "r") service.refresh()
        else if (t === "1" || t === "u") root.setTab("unread")
        else if (t === "2" || t === "p") root.setTab("previous")
        else if (t === "3" || t === "s") root.setTab("screener")
        else if (t === ",") root.settingsOpen = !root.settingsOpen
        else if (t === "t") root.trashSelected()
        else if (t === "a" && root.tabFilter !== "screener") root.setAsideSelected()
        else if (t === "m" && root.tabFilter !== "screener") root.markSeenSelected()
        else if (t === "i" && root.tabFilter === "screener") root.routeSelected("inbox")
        else if (t === "b" && root.tabFilter === "screener") root.routeSelected("block")
      }

      ColumnLayout {
        id: mainLayout
        anchors.fill: parent
        spacing: Style.space(10)

        // HEADER SECTION
        Column {
          id: fixedHeader
          Layout.fillWidth: true
          spacing: Style.space(10)

          Item {
            width: parent.width
            implicitHeight: Math.max(heroIcon.implicitHeight, heroLabels.implicitHeight, headerActions.implicitHeight)

            MailIcon {
              id: heroIcon
              anchors.left: parent.left
              anchors.verticalCenter: parent.verticalCenter
              iconSize: Style.font.display
              color: root.foreground
            }

            Column {
              id: heroLabels
              anchors.left: heroIcon.right
              anchors.leftMargin: Style.space(12)
              anchors.right: headerActions.left
              anchors.rightMargin: Style.space(10)
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(2)

              Text {
                text: "Mailbox"
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.title
                font.bold: true
              }

              Text {
                id: heroStatus
                width: parent.width
                text: root.heroStatusText.toUpperCase()
                color: service.lastError !== "" && service.actionStatus === "" ? root.urgent : root.dim
                font.family: root.fontFamily
                font.pixelSize: Style.font.bodySmall
                font.bold: true
                elide: Text.ElideRight
              }
            }

            Row {
              id: headerActions
              anchors.right: parent.right
              anchors.verticalCenter: parent.verticalCenter
              spacing: Style.space(4)

              PanelActionButton {
                iconText: service.notify ? "󰂚" : "󰂛"
                foreground: root.foreground
                tooltipText: service.notify ? "Mute notifications" : "Enable notifications"
                onClicked: root.persistSettings({ notify: !service.notify })
              }

              PanelActionButton {
                iconText: service.refreshing ? "󰑓" : "󰑐"
                foreground: root.foreground
                enabled: !service.refreshing
                tooltipText: "Refresh mail"
                onClicked: service.refresh()
              }

              PanelActionButton {
                iconText: root.settingsOpen ? "󰁝" : "󰒓"
                foreground: root.foreground
                tooltipText: root.settingsOpen ? "Back to mail" : "Settings"
                onClicked: root.settingsOpen = !root.settingsOpen
              }
            }
          }

          PanelSeparator {
            foreground: root.foreground
          }

          // Screener Alert Banner (when mail is waiting in screener)
          Rectangle {
            id: screenerAlertBanner
            visible: !root.settingsOpen && service.screenerCount > 0 && root.tabFilter !== "screener"
            width: parent.width
            height: Style.space(32)
            radius: Style.space(6)
            color: Qt.rgba(root.urgent.r, root.urgent.g, root.urgent.b, 0.15)
            border.color: Qt.rgba(root.urgent.r, root.urgent.g, root.urgent.b, 0.4)
            border.width: 1

            MouseArea {
              anchors.fill: parent
              cursorShape: Qt.PointingHandCursor
              onClicked: root.setTab("screener")
            }

            Row {
              anchors.centerIn: parent
              spacing: Style.space(6)

              Text {
                text: "󰍉"
                color: root.urgent
                font.family: root.fontFamily
                font.pixelSize: Style.font.body
              }

              Text {
                text: service.screenerCount + " sender" + (service.screenerCount === 1 ? "" : "s") + " waiting to be screened"
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: Style.font.bodySmall
                font.bold: true
              }
            }
          }

          // Account Dropdown (if multiple accounts)
          Dropdown {
            id: accountDropdown
            visible: !root.settingsOpen && service.accountCount > 1
            width: parent.width
            showLabel: false
            options: root.accountDropdownOptions
            foreground: root.foreground
            background: Color.popups.background
            accent: Color.accent
            fontFamily: root.fontFamily
            onChanged: function(value) { root.setAccount(value) }

            Binding on value {
              value: root.accountFilter
            }
          }

          // Tab Bar
          Row {
            visible: !root.settingsOpen
            spacing: Style.space(4)

            Button {
              text: "NEW FOR YOU"
              selected: root.tabFilter === "unread"
              foreground: root.foreground
              background: "transparent"
              accent: Color.accent
              fontFamily: root.fontFamily
              fontSize: Style.font.caption
              horizontalPadding: Style.space(8)
              verticalPadding: Style.space(2)
              onClicked: root.setTab("unread")
            }

            Button {
              text: "PREVIOUSLY SEEN"
              selected: root.tabFilter === "previous"
              foreground: root.foreground
              background: "transparent"
              accent: Color.accent
              fontFamily: root.fontFamily
              fontSize: Style.font.caption
              horizontalPadding: Style.space(8)
              verticalPadding: Style.space(2)
              onClicked: root.setTab("previous")
            }

            Button {
              text: service.screenerCount > 0 ? "SCREENER (" + service.screenerCount + ")" : "SCREENER"
              selected: root.tabFilter === "screener"
              foreground: service.screenerCount > 0 ? root.urgent : root.foreground
              background: "transparent"
              accent: root.urgent
              fontFamily: root.fontFamily
              fontSize: Style.font.caption
              horizontalPadding: Style.space(8)
              verticalPadding: Style.space(2)
              onClicked: root.setTab("screener")
            }
          }
        }

        // SCROLLABLE BODY
        Item {
          id: scrollContainer
          Layout.fillWidth: true
          Layout.fillHeight: true
          implicitHeight: Math.min(Style.space(480), contentColumn.implicitHeight)

          Flickable {
            id: panelFlick
            anchors.fill: parent
            contentWidth: width
            contentHeight: contentColumn.implicitHeight
            clip: true
            boundsBehavior: Flickable.StopAtBounds

            Column {
              id: contentColumn
              width: parent.width
              spacing: Style.space(8)

              // 1. SETTINGS VIEW
              Column {
                id: settingsView
                visible: root.settingsOpen
                width: parent.width
                spacing: Style.space(12)

                Text {
                  text: "SETTINGS"
                  color: root.dim
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.caption
                  font.bold: true
                }

                // Hide bar icon toggle
                RowLayout {
                  width: parent.width

                  Column {
                    Layout.fillWidth: true
                    spacing: Style.space(2)

                    Text {
                      text: "Hide bar icon when no new mail"
                      color: root.foreground
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.bodySmall
                    }
                    Text {
                      text: "Icon appears only when unread mail or screener items arrive"
                      color: root.dim
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.caption
                    }
                  }

                  CheckBox {
                    checked: root.setting("hideWhenEmpty", true)
                    onToggled: root.persistSettings({ hideWhenEmpty: checked })
                  }
                }

                // Notifications toggle
                RowLayout {
                  width: parent.width

                  Column {
                    Layout.fillWidth: true
                    spacing: Style.space(2)

                    Text {
                      text: "Desktop toast notifications"
                      color: root.foreground
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.bodySmall
                    }
                    Text {
                      text: "Show brief toast notification when new email arrives"
                      color: root.dim
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.caption
                    }
                  }

                  CheckBox {
                    checked: service.notify
                    onToggled: root.persistSettings({ notify: checked })
                  }
                }

                // Notification sound toggle
                RowLayout {
                  width: parent.width

                  Column {
                    Layout.fillWidth: true
                    spacing: Style.space(2)

                    Text {
                      text: "Notification sound"
                      color: root.foreground
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.bodySmall
                    }
                    Text {
                      text: "Play sound effect when new email arrives"
                      color: root.dim
                      font.family: root.fontFamily
                      font.pixelSize: Style.font.caption
                    }
                  }

                  CheckBox {
                    checked: service.sound
                    onToggled: root.persistSettings({ sound: checked })
                  }
                }
              }

              // 2. SCREENER VIEW
              Column {
                id: screenerView
                visible: !root.settingsOpen && root.tabFilter === "screener"
                width: parent.width
                spacing: Style.space(8)

                Text {
                  visible: root.screenerCards.length === 0
                  width: parent.width
                  horizontalAlignment: Text.AlignHCenter
                  topPadding: Style.space(24)
                  bottomPadding: Style.space(24)
                  text: "No senders waiting in Screener."
                  color: root.dim
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.body
                }

                Repeater {
                  model: root.screenerCards

                  Rectangle {
                    id: screenerCard
                    required property var modelData
                    required property int index

                    width: contentColumn.width
                    implicitHeight: cardContent.implicitHeight + Style.space(16)
                    radius: Style.space(8)
                    color: index === root.selectedIndex && root.cursorActive
                      ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.12)
                      : Color.popups.background
                    border.color: index === root.selectedIndex && root.cursorActive
                      ? Color.accent
                      : Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.1)
                    border.width: 1

                    Column {
                      id: cardContent
                      anchors.left: parent.left
                      anchors.right: parent.right
                      anchors.top: parent.top
                      anchors.margins: Style.space(8)
                      spacing: Style.space(8)

                      // Top Row: Avatar + Info + Count & Time
                      RowLayout {
                        width: parent.width
                        spacing: Style.space(8)

                        // Colored Initial Avatar
                        Rectangle {
                          width: Style.space(32)
                          height: Style.space(32)
                          radius: Style.space(16)
                          color: root.avatarColor(screenerCard.modelData)

                          Text {
                            anchors.centerIn: parent
                            text: screenerCard.modelData.initials
                            color: "#FFFFFF"
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.bodySmall
                            font.bold: true
                          }
                        }

                        Column {
                          Layout.fillWidth: true
                          spacing: Style.space(2)

                          Text {
                            text: screenerCard.modelData.name
                            color: root.foreground
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.body
                            font.bold: true
                            elide: Text.ElideRight
                          }

                          Text {
                            text: screenerCard.modelData.address
                            color: root.dim
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.caption
                            elide: Text.ElideRight
                          }
                        }

                        Column {
                          spacing: Style.space(2)
                          Text {
                            anchors.right: parent.right
                            text: screenerCard.modelData.count + " email" + (screenerCard.modelData.count === 1 ? "" : "s")
                            color: root.urgent
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.caption
                            font.bold: true
                          }
                          Text {
                            anchors.right: parent.right
                            text: screenerCard.modelData.time
                            color: root.dim
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.caption
                          }
                        }
                      }

                      // Subject line
                      Text {
                        width: parent.width
                        text: screenerCard.modelData.subject
                        color: root.foreground
                        font.family: root.fontFamily
                        font.pixelSize: Style.font.bodySmall
                        elide: Text.ElideRight
                      }

                      // Action buttons: Inbox, Block, Trash. Feed and Paper
                      // Trail are deliberately not here — this widget only
                      // screens a sender in or out; sorting them into a bucket
                      // is a decision for the full client.
                      Row {
                        spacing: Style.space(4)

                        Button {
                          text: "📥 INBOX (I)"
                          foreground: root.foreground
                          background: Color.accent
                          fontFamily: root.fontFamily
                          fontSize: Style.font.caption
                          horizontalPadding: Style.space(6)
                          verticalPadding: Style.space(4)
                          onClicked: service.routeSender(screenerCard.modelData.address, "inbox")
                        }

                        Button {
                          text: "🚫 BLOCK (B)"
                          foreground: "#FFFFFF"
                          background: root.urgent
                          fontFamily: root.fontFamily
                          fontSize: Style.font.caption
                          horizontalPadding: Style.space(6)
                          verticalPadding: Style.space(4)
                          onClicked: service.routeSender(screenerCard.modelData.address, "block")
                        }

                        Button {
                          text: "🗑 TRASH (T)"
                          foreground: root.foreground
                          background: Qt.darker(root.urgent, 1.5)
                          fontFamily: root.fontFamily
                          fontSize: Style.font.caption
                          horizontalPadding: Style.space(6)
                          verticalPadding: Style.space(4)
                          onClicked: service.trashMessage(screenerCard.modelData.id)
                        }
                      }
                    }
                  }
                }
              }

              // 3. MAIL LIST VIEW (Unread & Previous)
              Column {
                id: mailListView
                visible: !root.settingsOpen && root.tabFilter !== "screener"
                width: parent.width
                spacing: Style.space(4)

                Text {
                  visible: root.filteredMessages.length === 0
                  width: parent.width
                  horizontalAlignment: Text.AlignHCenter
                  topPadding: Style.space(24)
                  bottomPadding: Style.space(24)
                  text: root.tabFilter === "unread" ? "You're all caught up." : "No previously seen email."
                  color: root.dim
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.body
                }

                Repeater {
                  id: listColumn
                  model: root.filteredMessages

                  Rectangle {
                    id: mailRow
                    required property var modelData
                    required property int index

                    width: contentColumn.width
                    implicitHeight: rowLayout.implicitHeight + Style.space(12)
                    radius: Style.space(6)
                    color: index === root.selectedIndex && root.cursorActive
                      ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.12)
                      : (rowHover.containsMouse ? Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.05) : "transparent")

                    MouseArea {
                      id: rowHover
                      anchors.fill: parent
                      hoverEnabled: true
                      cursorShape: Qt.PointingHandCursor
                      onClicked: root.openMail(mailRow.modelData)
                    }

                    RowLayout {
                      id: rowLayout
                      anchors.left: parent.left
                      anchors.right: parent.right
                      anchors.top: parent.top
                      anchors.margins: Style.space(6)
                      spacing: Style.space(10)

                      // Avatar
                      Rectangle {
                        width: Style.space(30)
                        height: Style.space(30)
                        radius: Style.space(15)
                        color: root.avatarColor(mailRow.modelData)

                        Text {
                          anchors.centerIn: parent
                          text: mailRow.modelData.initials
                          color: "#FFFFFF"
                          font.family: root.fontFamily
                          font.pixelSize: Style.font.caption
                          font.bold: true
                        }
                      }

                      // Main Content
                      Column {
                        Layout.fillWidth: true
                        spacing: Style.space(2)

                        RowLayout {
                          width: parent.width

                          Text {
                            Layout.fillWidth: true
                            text: mailRow.modelData.name
                            color: root.foreground
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.bodySmall
                            font.bold: !mailRow.modelData.seen
                            elide: Text.ElideRight
                          }

                          Text {
                            text: Model.formatRelativeTime(mailRow.modelData.date, root.nowMs)
                            color: root.dim
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.caption
                          }
                        }

                        Text {
                          width: parent.width
                          text: mailRow.modelData.subject
                          color: root.foreground
                          font.family: root.fontFamily
                          font.pixelSize: Style.font.body
                          font.bold: !mailRow.modelData.seen
                          elide: Text.ElideRight
                        }
                      }

                      // Quick Action Buttons on Hover
                      Row {
                        visible: rowHover.containsMouse || (mailRow.index === root.selectedIndex && root.cursorActive)
                        spacing: Style.space(2)

                        // Mark read (only on unread mail)
                        PanelActionButton {
                          visible: !mailRow.modelData.seen
                          iconText: "󰄬"
                          foreground: root.foreground
                          tooltipText: "Mark read"
                          onClicked: service.setSeen(mailRow.modelData.id, true)
                        }

                        // Set aside
                        PanelActionButton {
                          iconText: "󰔛"
                          foreground: root.foreground
                          tooltipText: "Set aside"
                          onClicked: service.setAside(mailRow.modelData.id)
                        }

                        // Move to trash
                        PanelActionButton {
                          iconText: "󰆴"
                          foreground: root.foreground
                          hoverColor: root.urgent
                          tooltipText: "Move to Trash"
                          onClicked: service.trashMessage(mailRow.modelData.id)
                        }
                      }
                    }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
