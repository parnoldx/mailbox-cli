import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

// BarWidget.qml — Email notification bar icon and host for the Mailbox popup panel.
//
// Appears in the top bar when there is unread inbox mail, or when the daemon is
// holding a Pickup. When everything is read and nothing is held the widget
// collapses and stays invisible, so it acts cleanly as a notification icon.
//
// Unread inbox mail and held Pickups are the only two things it knows about.
// The Screener is not here at all: screening is a decision owed whenever you
// next sit down, so it lives in the desktop client. A Pickup is the opposite —
// it is already read, it owes no decision, and it is worthless in minutes,
// which is why it takes the icon: a toast is silenced by Do Not Disturb and
// gone in seconds, and the code is on the clipboard with nothing on screen
// saying so. The key is gone a minute after the code landed; the mail, and the
// panel's list of it, stay until the daemon bins them.
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
  readonly property bool hasNew: unseenCount > 0

  // A held code or magic link the daemon is still holding, for as long as the
  // key means anything (Model.PICKUP_ICON_MS, a minute from arrival). The nerd-
  // font key is the whole of what the icon says about it: one glyph, no badge,
  // nothing to read while a login form is waiting.
  readonly property int pickupCount: service ? service.pickupCount : 0

  // The window is evaluated against a clock that ticks, not against the age a
  // row was fetched with: `pickups` only changes when the daemon says so, and
  // the key has to go on its own minute without waiting for the next arrival.
  property double nowMs: Date.now()
  readonly property bool pickupReady: Model.pickupFresh(service ? service.pickups : [], nowMs)

  Timer {
    interval: 1000
    repeat: true
    // Only while the key is up: freshness never comes back on its own, and a
    // new arrival re-evaluates the binding long before this could have
    // mattered. So one pickup costs at most sixty ticks.
    running: root.pickupReady
    onTriggered: root.nowMs = Date.now()
  }

  Connections {
    target: service
    // A fresh arrival is evaluated against now, not against whenever this
    // widget last looked at a clock.
    function onPickupsChanged() { root.nowMs = Date.now() }
  }

  readonly property bool hideWhenEmpty: setting("hideWhenEmpty", true)
  readonly property bool widgetVisible: !hideWhenEmpty || hasNew || opened || pickupReady

  visible: widgetVisible
  implicitWidth: widgetVisible ? button.implicitWidth : 0
  implicitHeight: widgetVisible ? button.implicitHeight : 0

  readonly property color foreground: bar ? bar.barForeground : Color.foreground
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
    // The held pickups, for anything that wants to ask the bar instead of the
    // daemon — and for a test that has to see the icon's state without eyes.
    // `pickups` is what the panel lists (held until binned); `pickupKey` is
    // only whether the key is on the bar, which is the shorter window.
    function pickups(): int { return root.pickupCount }
    function pickupKey(): bool { return root.pickupReady }
  }

  // The two icons the slot can carry. A Component cannot sit inside the
  // ternary below — QML parses the object literal as a token, not a value —
  // so the envelope is declared here and only chosen there.
  Component {
    id: envelopeIcon
    Item {
      MailIcon {
        anchors.centerIn: parent
        iconSize: Style.space(14)
        color: {
          if (root.unseenCount > 0) return root.accent
          return root.foreground
        }
      }
    }
  }

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    // A held Pickup outranks unread mail in the tooltip as well as the icon:
    // it is the one that stops being useful.
    tooltipText: root.pickupReady
      ? (root.pickupCount === 1
        ? "Code or link ready to paste"
        : root.pickupCount + " codes and links ready to paste")
      : (root.unseenCount > 0
        ? root.unseenCount + " unread email" + (root.unseenCount === 1 ? "" : "s")
        : "Mailbox")

    // The slot carries one icon. When a Pickup is held it is the key glyph and
    // nothing else — the envelope says "mail to read", which is exactly what a
    // collected code is not.
    text: root.pickupReady ? "󰌆" : ""
    active: root.pickupReady
    useActiveColor: true
    activeColor: root.accent
    iconComponent: root.pickupReady ? null : envelopeIcon

    onPressed: function(buttonCode) {
      if (buttonCode === Qt.RightButton || buttonCode === Qt.MiddleButton) {
        if (service) service.refresh()
      } else {
        root.togglePanel()
      }
    }
  }
}
