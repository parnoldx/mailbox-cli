import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

// Date/time label for the bar, and the host for the calendar popup.
//
// Left click reveals the calendar — asking "what is the date?" is what a
// click on a clock means — right click walks the common label formats, and
// middle click opens the timezone picker.
//
// In the 5 minutes before a timed event, the clock keeps the time and
// appends the reminder. The label stays the bar color until the last
// minute, then turns urgent. Click opens the calendar popup (Join lives
// there).
BarWidget {
  id: root
  moduleName: "omarchy.clock"

  property date displayDate: clock.date

  readonly property string configuredFormat: vertical
    ? setting("verticalFormat", "HH\n—\nmm")
    : setting("format", "dddd HH:mm")
  readonly property string configuredAltFormat: vertical
    ? setting("verticalFormatAlt", "dd\nMMM\n'W'ww\n''yy")
    : setting("formatAlt", "d MMMM 'W'ww yyyy")

  readonly property var formatRing: Model.clockFormatRing(configuredFormat, configuredAltFormat, Model.clockFormats(vertical))

  // What the bar shows is what shell.json stores, so a cycled format is the
  // format from then on rather than something that reverts on restart.
  readonly property string activeFormat: configuredFormat

  // The panel owns the events file. The loader stays active while the popup
  // is closed, so the bar can count down without anyone opening the calendar.
  readonly property var visibleEventList: panelLoader.item ? panelLoader.item.visibleEventList : []
  readonly property real nowMs: displayDate.getTime()
  readonly property int announceLeadMinutes: setting("announceLeadMinutes", 5)
  readonly property int startedLeadMinutes: setting("startedLeadMinutes", 5)
  readonly property var upcomingEvent: Model.nextEvent(visibleEventList, nowMs)
  property string dismissedKey: ""
  readonly property bool announcing: announceLeadMinutes > 0
    && Model.shouldAnnounce(upcomingEvent, nowMs, announceLeadMinutes, startedLeadMinutes)
    && !Model.isDismissed(upcomingEvent, dismissedKey)
  readonly property bool joinable: Model.meetingUrlFor(upcomingEvent) !== ""
  readonly property bool showReminder: announcing && !vertical
  readonly property bool reminderUrgent: showReminder
    && Model.isImminent(Model.millisUntil(upcomingEvent, nowMs))

  readonly property string displayText: {
    var clockText = formatted(displayDate)
    if (!showReminder) return clockText
    var title = Model.truncateTitle(upcomingEvent && upcomingEvent.title)
    var phrase = Model.formatStartsIn(Model.millisUntil(upcomingEvent, nowMs))
    var reminder = title && phrase ? title + "  " + phrase : (title || phrase)
    if (!reminder) return clockText
    return clockText + "  ·  " + reminder
  }
  readonly property var verticalLines: formatted(displayDate).split("\n")

  function refresh() {
    displayDate = new Date()
    if (panelLoader.item && panelLoader.item.refresh) panelLoader.item.refresh()
  }

  function dismissReminder(event) {
    var key = Model.occurrenceKey(event)
    if (!key) return
    var items = bar && typeof bar.moduleWidgets === "function"
      ? bar.moduleWidgets(root.moduleName) : [root]
    for (var i = 0; i < items.length; i++)
      if (items[i]) items[i].dismissedKey = key
    root.dismissedKey = key
  }

  // The URL never goes through a shell — Panel.openMeeting uses
  // Qt.openUrlExternally after Model.safeUrl has refused anything that is
  // not plain https. Joining also puts the reminder away.
  function joinUpcoming() {
    if (!root.joinable) return
    if (panelLoader.item && panelLoader.item.openMeeting)
      panelLoader.item.openMeeting(root.upcomingEvent)
  }

  function cycleFormat() {
    var current = String(configuredFormat)
    var next = Model.nextClockFormat(formatRing, current)
    if (next === "" || next === current) return

    var entry = { id: root.moduleName }
    for (var key in root.settings) if (key !== "id") entry[key] = root.settings[key]
    entry[vertical ? "verticalFormat" : "format"] = next

    // Applied locally first so the label changes on the click itself; the
    // shell.json write comes back through the bar as the same value.
    root.settings = entry
    if (root.bar && root.bar.shell && typeof root.bar.shell.updateEntryInline === "function")
      root.bar.shell.updateEntryInline(root.moduleName, entry)
  }

  function formatted(date) {
    return Qt.formatDateTime(date, activeFormat.replace(/ww/g, Model.isoWeekLiteral(date.getFullYear(), date.getMonth(), date.getDate())))
  }

  // ---- Calendar popup. Shape contract for shell.summon/hide/toggle
  //      routing: Bar.findPanelWidget requires open/close/opened on the
  //      bar-widget root.
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

  function toggleWeekStart() {
    if (panelLoader.item) panelLoader.item.toggleWeekStart()
  }

  // The clock fills more slot than it paints a mark for, at both
  // orientations: horizontally it is a text label in a padded slot, so the
  // dot takes the label width; vertically it is a stack of icon-sized lines,
  // so the dot takes one line — the same mark every icon widget gets, rather
  // than a rule running the height of the whole stack.
  readonly property real openPanelIndicatorWidth: button.labelWidth
  readonly property real openPanelIndicatorHeight: Math.max(Style.space(10), Math.round(Style.bar.iconSlot * 0.55))

  // Forwarded so this widget can stand in for the panel as the bar's popout
  // identity: Bar.requestPopout prefers closeForPopoutSwitch over close, and
  // KeyboardPanel reads popoutSwitchClosing back off its owner.
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
  }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  onBarChanged: injectPanel()
  onSettingsChanged: injectPanel()

  // A join button nobody clicked: between one and five minutes into a
  // meeting whose Join link is still unclicked (dismissReminder, which only
  // Panel.openMeeting calls), play one sound. nudgedKey keeps that single
  // sound single — every later tick sees nudgeDue still true.
  readonly property bool nudgeDue: Model.shouldNudge(upcomingEvent, nowMs)
    && !Model.isDismissed(upcomingEvent, dismissedKey)
  property string nudgedKey: ""

  function maybeNudge() {
    if (!nudgeDue) return
    var key = Model.occurrenceKey(upcomingEvent)
    if (!key || key === nudgedKey) return
    nudgedKey = key
    nudgeSound.running = true
  }

  // paplay rather than anything Qt owns, so the sound follows the default
  // PipeWire sink and its volume like every other desktop sound.
  Process {
    id: nudgeSound
    command: ["paplay", "/usr/share/sounds/freedesktop/stereo/alarm-clock-elapsed.oga"]
  }

  SystemClock {
    id: clock
    precision: SystemClock.Seconds
    onDateChanged: {
      root.displayDate = date
      root.maybeNudge()
    }
  }

  Loader {
    id: panelLoader
    active: true
    source: Qt.resolvedUrl("Panel.qml")
    visible: false
    onLoaded: {
      root.injectPanel()
      Qt.callLater(root.injectPanel)
    }
  }

  IpcHandler {
    target: "omarchy.clock"

    function refresh(): void { root.broadcast("refresh") }
    function cycleFormat(): void { root.cycleFormat() }
    function toggleWeekStart(): void { root.toggleWeekStart() }
    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.togglePanel() }
    function join(): void { root.joinUpcoming() }
    function entry(): void {
      root.open()
      Qt.callLater(function() {
        if (panelLoader.item && panelLoader.item.openEntry) panelLoader.item.openEntry("")
      })
    }
  }

  IpcHandler {
    target: "mailbox.clock"

    function refresh(): void { root.broadcast("refresh") }
    function cycleFormat(): void { root.cycleFormat() }
    function toggleWeekStart(): void { root.toggleWeekStart() }
    function open(): void { root.open() }
    function close(): void { root.close() }
    function show(): void { root.open() }
    function hide(): void { root.close() }
    function toggle(): void { root.togglePanel() }
    function join(): void { root.joinUpcoming() }
    function entry(): void {
      root.open()
      Qt.callLater(function() {
        if (panelLoader.item && panelLoader.item.openEntry) panelLoader.item.openEntry("")
      })
    }
  }

  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: root.vertical ? "" : root.displayText
    foreground: root.reminderUrgent
      ? (root.bar ? root.bar.urgent : Color.urgent)
      : (root.bar ? root.bar.barForeground : Color.foreground)
    labelVisible: !root.vertical
    hasVisualContent: root.vertical ? root.verticalLines.length > 0 : text !== ""
    fixedHeight: root.vertical ? root.verticalLines.length * Style.bar.iconSlot : -1
    horizontalMargin: 6
    verticalPadding: 8.75

    onPressed: function(b) {
      if (b === Qt.RightButton) root.cycleFormat()
      else if (b === Qt.MiddleButton) { if (root.bar) root.bar.run("omarchy-menu-timezone") }
      else root.togglePanel()
    }

    Column {
      visible: root.vertical
      anchors.fill: parent

      Repeater {
        model: root.verticalLines

        OpticalGlyph {
          required property string modelData
          width: button.width
          height: Style.bar.iconSlot
          text: modelData
          fontFamily: button.fontFamily
          fontSize: modelData.length > 3
            ? button.fontSize * 0.9
            : button.fontSize
          color: button.foreground
        }
      }
    }
  }
}
