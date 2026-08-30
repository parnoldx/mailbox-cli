import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui

// BarWidget.qml — Email notification bar icon and host for the Mailbox popup panel.
//
// Appears in the top bar when there is unread mail or pending screening decisions.
// When all mail is seen and the screener is empty, the widget collapses and stays
// invisible so it acts cleanly as a notification icon.
BarWidget {
  id: root
  moduleName: "mailbox.email"

  readonly property var sharedService: bar && bar.shell && typeof bar.shell.serviceFor === "function"
    ? bar.shell.serviceFor("mailbox.email") : null
  readonly property var service: sharedService || localService

  MailboxService {
    id: localService
    settings: root.settings
  }

  readonly property int unseenCount: service ? service.unreadCount : 0
  readonly property int screenerCount: service ? service.screenerCount : 0
  readonly property int totalAlertCount: unseenCount + screenerCount
  readonly property bool hasNew: totalAlertCount > 0

  readonly property bool hideWhenEmpty: setting("hideWhenEmpty", true)
  readonly property bool widgetVisible: !hideWhenEmpty || hasNew || opened

  visible: widgetVisible
  implicitWidth: widgetVisible ? button.implicitWidth : 0
  implicitHeight: widgetVisible ? button.implicitHeight : 0

  readonly property color foreground: bar ? bar.barForeground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color accent: Color.accent

  // Popout / panel coordinator routing
  readonly property bool opened: panelLoader.item ? panelLoader.item.opened === true : false

  function open() {
    if (panelLoader.item) panelLoader.item.open()
  }

  function close() {
    if (panelLoader.item) panelLoader.item.close()
  }

  function togglePanel() {
    if (panelLoader.item) panelLoader.item.toggle()
  }

  readonly property real openPanelIndicatorWidth: Style.bar.iconSlot
  readonly property real openPanelIndicatorHeight: Math.max(Style.space(10), Math.round(Style.bar.iconSlot * 0.55))

  readonly property bool popoutSwitchClosing: panelLoader.item ? panelLoader.item.popoutSwitchClosing === true : false

  function closeForPopoutSwitch() {
    if (panelLoader.item) panelLoader.item.closeForPopoutSwitch()
  }

  function injectPanel() {
    var target = panelLoader.item
    if (!target) return
    if ("bar" in target) target.bar = root.bar
    if ("settings" in target) target.settings = root.settings
    if ("anchorItem" in target) target.anchorItem = button
    if ("hostWidget" in target) target.hostWidget = root
    if ("externalService" in target) target.externalService = service
  }

  onBarChanged: Qt.callLater(injectPanel)
  onSettingsChanged: Qt.callLater(injectPanel)
  Component.onCompleted: Qt.callLater(injectPanel)

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: root.injectPanel()
  }

  IpcHandler {
    target: "mailbox.email"

    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.togglePanel() }
    function refresh(): string { if (service) service.refresh(); return "ok" }
    function unread(): int { return root.unseenCount }
    function screener(): int { return root.screenerCount }
    function total(): int { return root.totalAlertCount }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    tooltipText: {
      if (root.screenerCount > 0 && root.unseenCount > 0) {
        return root.unseenCount + " unread mail, " + root.screenerCount + " to screen"
      }
      if (root.screenerCount > 0) {
        return root.screenerCount + " sender" + (root.screenerCount === 1 ? "" : "s") + " waiting to be screened"
      }
      if (root.unseenCount > 0) {
        return root.unseenCount + " unread email" + (root.unseenCount === 1 ? "" : "s")
      }
      return "Mailbox"
    }

    iconComponent: Component {
      Item {
        MailIcon {
          anchors.centerIn: parent
          iconSize: Style.space(14)
          color: {
            if (root.screenerCount > 0) return root.urgent
            if (root.unseenCount > 0) return root.accent
            return root.foreground
          }
        }
      }
    }

    onPressed: function(buttonCode) {
      if (buttonCode === Qt.RightButton || buttonCode === Qt.MiddleButton) {
        if (service) service.refresh()
      } else {
        root.togglePanel()
      }
    }
  }
}
