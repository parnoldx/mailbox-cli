import QtQuick
import QtQuick.Layouts
import Quickshell
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Panel.qml — Dropdown popup panel for Mailbox email notifications and screening.
//
// Features:
// - One reverse-chron stream of unread inbox mail. Read mail is not shown, and
//   neither is the Screener — both are the desktop client's job. This panel
//   only ever answers "did anything new arrive?"
// - 1-click mail actions (mark read, set aside, trash) inline on the row
// - Sender initials in deterministic colored avatars
// - Account filtering with per-account unread badges
// - Keyboard navigation (T, A, M, R, arrows, Enter, Esc)
// - Flip settings page for toast alerts and bar visibility
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
  property double openedAtMs: 0
  property double closedAtMs: 0
  property string accountFilter: ""
  property bool settingsOpen: false

  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color dim: Qt.darker(foreground, 1.55)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family

  // The one list. Everything — cursor, keys, scrolling — walks this.
  readonly property var feed: Model.feedItems(service.messages, accountFilter)

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

  // Circular sender initials.
  component Avatar: Rectangle {
    id: avatar

    property var item: null
    property real diameter: Style.space(30)
    property real fontSize: Style.font.caption

    implicitWidth: diameter
    implicitHeight: diameter
    radius: diameter / 2
    color: root.avatarColor(item)

    Text {
      anchors.centerIn: parent
      text: avatar.item ? avatar.item.initials : "?"
      color: "#FFFFFF"
      font.family: root.fontFamily
      font.pixelSize: avatar.fontSize
      font.bold: true
    }
  }

  property int phraseIndex: 0
  readonly property var loadingPhrases: [
    "Checking the mirror…",
    "Fetching new mail…",
    "Syncing with server…"
  ]
  readonly property bool rotatingPhrases: service.refreshing

  readonly property string heroStatusText: {
    if (service.actionStatus !== "") return service.actionStatus
    if (service.lastError !== "") return service.lastError
    if (rotatingPhrases) return loadingPhrases[phraseIndex % loadingPhrases.length]
    if (service.unreadCount > 0) return service.unreadCount + " UNREAD"
    return "ALL CAUGHT UP"
  }

  function resetSelection() {
    selectedIndex = 0
    cursorActive = false
    pointerGate.reset()
    if (panelFlick) panelFlick.contentY = 0
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

  // Top-left header icon: opens the full desktop client on the inbox, same
  // launch path as openMail() but with no message to jump to. Guarded against
  // the click that opened the panel itself: the bar button's press opens the
  // popup immediately, so the matching release can land on this icon if it
  // ends up under the still-down pointer, firing a phantom click here too.
  // Same phantom on close: the bar button's press closes the panel and the
  // release lands here, so ignore clicks shortly after a close as well.
  function openGui() {
    if (Date.now() - root.openedAtMs < 300) return
    if (Date.now() - root.closedAtMs < 300) return
    if (root.bar && typeof root.bar.run === "function") {
      root.bar.run("mailbox-gui")
    } else {
      Quickshell.execDetached(["bash", "-lc", "mailbox-gui"])
    }
    root.close()
  }

  function selectedItem() {
    return selectedIndex >= 0 && selectedIndex < feed.length ? feed[selectedIndex] : null
  }

  function moveSelection(delta) {
    var count = feed.length
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
    openMail(selectedItem())
  }

  function trashSelected() {
    var item = selectedItem()
    if (item) service.trashMessage(item.id)
  }

  function setAsideSelected() {
    var item = selectedItem()
    if (item) service.setAside(item.id)
  }

  function markSeenSelected() {
    var item = selectedItem()
    if (item) service.setSeen(item.id, !item.seen)
  }

  function scrollSelectionIntoView() {
    // Delegates are parented to the Column, not to the Repeater, so ask the
    // Repeater for them by index rather than walking its children.
    var wrapper = selectedIndex >= 0 ? feedRepeater.itemAt(selectedIndex) : null
    if (!wrapper) return
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
    openedAtMs = nowMs
    if (panelFlick) panelFlick.contentY = 0
    service.refresh()
    Qt.callLater(function() { keyCatcher.forceActiveFocus() })
  } else {
    closedAtMs = Date.now()
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
        else if (t === ",") root.settingsOpen = !root.settingsOpen
        else if (t === "t") root.trashSelected()
        else if (t === "a") root.setAsideSelected()
        else if (t === "m") root.markSeenSelected()
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

              MouseArea {
                anchors.fill: parent
                anchors.margins: -Style.space(4)
                cursorShape: Qt.PointingHandCursor
                onClicked: root.openGui()
              }
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

          // Account Dropdown (if multiple accounts)
          Dropdown {
            id: accountDropdown
            visible: !root.settingsOpen && service.accountCount > 1
            width: parent.width
            showLabel: false
            options: Model.accountFilterOptions(service.accounts, service.messages)
            foreground: root.foreground
            background: Color.popups.background
            accent: Color.accent
            fontFamily: root.fontFamily
            onChanged: function(value) { root.setAccount(value) }

            Binding on value {
              value: root.accountFilter
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
                spacing: Style.space(8)

                PanelSectionHeader {
                  text: "SETTINGS"
                  foreground: root.foreground
                  fontFamily: root.fontFamily
                }

                Repeater {
                  model: [
                    {
                      key: "hideWhenEmpty", fallback: true,
                      label: "Hide bar icon when no new mail",
                      description: "Icon appears only when new inbox mail arrives"
                    },
                    {
                      key: "notify", fallback: false,
                      label: "Desktop toast notifications",
                      description: "Show brief toast notification when new email arrives"
                    },
                    {
                      key: "sound", fallback: true,
                      label: "Notification sound",
                      description: "Play sound effect when new email arrives"
                    }
                  ]

                  Toggle {
                    required property var modelData

                    width: contentColumn.width
                    label: modelData.label
                    description: modelData.description
                    checked: root.setting(modelData.key, modelData.fallback)
                    foreground: root.foreground
                    fontFamily: root.fontFamily
                    titleSize: Style.font.bodySmall
                    onClicked: {
                      var change = {}
                      change[modelData.key] = !checked
                      root.persistSettings(change)
                    }
                  }
                }
              }

              // 2. THE FEED — unread inbox mail, newest first.
              Column {
                id: feedView
                visible: !root.settingsOpen
                width: parent.width
                spacing: Style.space(4)

                Text {
                  visible: root.feed.length === 0
                  width: parent.width
                  horizontalAlignment: Text.AlignHCenter
                  topPadding: Style.space(24)
                  bottomPadding: Style.space(24)
                  text: "You're all caught up."
                  color: root.dim
                  font.family: root.fontFamily
                  font.pixelSize: Style.font.body
                }

                Repeater {
                  id: feedRepeater
                  model: root.feed

                  Rectangle {
                    id: feedRow
                    required property var modelData
                    required property int index

                    readonly property bool current: index === root.selectedIndex && root.cursorActive
                    readonly property bool showActions: rowHover.containsMouse || current

                    width: contentColumn.width
                    implicitHeight: rowLayout.implicitHeight + Style.space(12)
                    radius: Style.space(6)
                    color: current
                      ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.12)
                      : (rowHover.containsMouse ? Qt.rgba(root.foreground.r, root.foreground.g, root.foreground.b, 0.05) : "transparent")

                    MouseArea {
                      id: rowHover
                      anchors.fill: parent
                      hoverEnabled: true
                      cursorShape: Qt.PointingHandCursor
                      onClicked: root.openMail(feedRow.modelData)
                    }

                    RowLayout {
                      id: rowLayout
                      anchors.left: parent.left
                      anchors.right: parent.right
                      anchors.top: parent.top
                      anchors.margins: Style.space(6)
                      spacing: Style.space(10)

                      Avatar { item: feedRow.modelData }

                      Column {
                        Layout.fillWidth: true
                        spacing: Style.space(2)

                        RowLayout {
                          width: parent.width
                          spacing: Style.space(6)

                          Text {
                            Layout.fillWidth: true
                            text: feedRow.modelData.name
                            color: root.foreground
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.bodySmall
                            font.bold: !feedRow.modelData.seen
                            elide: Text.ElideRight
                          }

                          Text {
                            text: feedRow.modelData.sortMs > 0
                              ? Model.formatRelativeTime(feedRow.modelData.sortMs, root.nowMs)
                              : ""
                            color: root.dim
                            font.family: root.fontFamily
                            font.pixelSize: Style.font.caption
                          }
                        }

                        Text {
                          width: parent.width
                          text: feedRow.modelData.subject
                          color: root.foreground
                          font.family: root.fontFamily
                          font.pixelSize: Style.font.body
                          font.bold: !feedRow.modelData.seen
                          elide: Text.ElideRight
                        }
                      }

                      // Mail quick actions, on hover or under the cursor.
                      Row {
                        visible: feedRow.showActions
                        spacing: Style.space(2)

                        PanelActionButton {
                          iconText: "󰄬"
                          foreground: root.foreground
                          tooltipText: "Mark read"
                          onClicked: service.setSeen(feedRow.modelData.id, true)
                        }

                        PanelActionButton {
                          iconText: "󰔛"
                          foreground: root.foreground
                          tooltipText: "Set aside"
                          onClicked: service.setAside(feedRow.modelData.id)
                        }

                        PanelActionButton {
                          iconText: "󰆴"
                          foreground: root.foreground
                          hoverColor: root.urgent
                          tooltipText: "Move to Trash"
                          onClicked: service.trashMessage(feedRow.modelData.id)
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
