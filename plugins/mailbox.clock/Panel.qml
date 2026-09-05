import QtQuick
import Quickshell
import Quickshell.Io
import qs.Commons
import qs.Ui
import "Model.js" as Model

// The clock's calendar popup: a month grid with ISO week numbers, built to
// sit beside the weather panel — same hero-over-detail composition, same
// spacing scale, same small-caps labels.
//
// The grid is a read-out rather than a picker: today is the only marked
// day, and the only thing that moves is which month is on screen —
// chevrons, the scroll wheel, and the arrow keys all step it.
//
// BarWidget.qml owns the bar label and hands this panel the button to
// anchor against.
Panel {
  id: root
  moduleName: "omarchy.clock"
  ipcTarget: "omarchy.clock"
  manageIpc: false

  property var anchorItem: null

  // The bar tracks the widget mounted in its slot — BarWidget.qml — not this
  // nested panel. Everything the bar identifies a panel by has to be that
  // widget: the popout coordinator (and with it the open-panel dot under the
  // pill) compares against `slot.activeItem`, and switchPanelFrom looks the
  // slot up the same way.
  property var hostWidget: null
  readonly property var barIdentity: hostWidget || root

  // ---- Today. SystemClock keeps this honest across midnight so the
  //      highlight rolls over without the panel being reopened.
  property date today: new Date()
  readonly property string todayKey: Model.keyForDate(today)

  // The month on screen. Stepping moves this and nothing else: the grid is
  // a read-out, not a picker, so there is no per-day cursor to keep in sync.
  property int viewYear: today.getFullYear()
  property int viewMonth: today.getMonth()

  readonly property date viewDate: new Date(viewYear, viewMonth, 1)
  readonly property bool viewingCurrentMonth: viewYear === today.getFullYear() && viewMonth === today.getMonth()

  // Pinned to today, not to the month being browsed — stepping through the
  // calendar does not change how much of the year is gone.
  readonly property real yearDone: Model.yearProgress(today.getFullYear(), today.getMonth(), today.getDate())
  readonly property int yearDonePercent: Model.yearProgressPercent(today.getFullYear(), today.getMonth(), today.getDate())

  // Memento mori, for anyone who goes looking: double-tapping the year bar
  // asks for a birth year and a life expectancy, and a second bar tracks one
  // against the other. A birth year rather than an age, so it keeps counting
  // on its own. Without one the bar stays hidden.
  readonly property int birthYear: Model.parseBirthYear(setting("birthYear", 0), today.getFullYear())
  readonly property int age: Model.ageFromBirthYear(birthYear, today.getFullYear())
  readonly property int lifeExpectancy: Model.parseLifeExpectancy(setting("lifeExpectancy", 0))
  readonly property real lifeDone: Model.lifeProgress(age, lifeExpectancy)
  readonly property int lifeDonePercent: Model.lifeProgressPercent(age, lifeExpectancy)
  property bool editingLife: false

  // Unset falls through to the locale's own first day, so a fresh install
  // starts out matching the rest of the desktop rather than a hardcoded
  // convention. Clicking the grid's "W" heading writes the choice back to
  // shell.json.
  readonly property int weekStart: Model.normalizedWeekStart(setting("weekStartDay", null), Qt.locale().firstDayOfWeek)
  readonly property string nextWeekStartLabel: Qt.locale().dayName(Model.toggledWeekStart(weekStart), Locale.LongFormat)
  readonly property var weekdays: Model.weekdayOrder(weekStart)
  property int eventRevision: 0
  readonly property var weeks: {
    eventRevision
    return Model.monthGrid(viewYear, viewMonth, weekStart, todayKey, eventIndex, taskIndex)
  }

  property var eventDoc: null
  property var eventIndex: ({})
  property var visibleEventList: []
  property string selectedDayKey: todayKey
  readonly property var selectedEvents: Model.eventsForDateKey(eventIndex, selectedDayKey)

  // ---- Tasks. A month grid has no room for the HEY calendar's "sometime
  //      this week" row per week, and it does not need one: a task with no
  //      due date, and a task whose due date has gone by, both belong to the
  //      work in front of you now. So there is one bucket, always the current
  //      week's — the week holding the selected day, when that is this week.
  //      Click into next week and the bucket is empty, because nothing has
  //      been carried into it yet. Tasks that do have a day ahead of them sit
  //      on that day instead, as a hollow ring in the cell.
  //
  //      The rollover is a read, not a write (Model.taskPlacement): an
  //      unfinished task shows up in this week, and next week, and the one
  //      after, without the server ever being asked to move it.
  property var taskIndex: ({})
  property var visibleTaskList: []
  readonly property var selectedTasks: {
    eventRevision
    return Model.tasksForDateKey(taskIndex, selectedDayKey)
  }
  readonly property string selectedWeekKey: Model.weekStartKeyOf(selectedDayKey, weekStart)
  // The bucket's header shows on the current week even when the week is
  // empty: the + on it is how the first todo gets added.
  readonly property bool bucketWeekSelected: selectedWeekKey === Model.weekStartKeyOf(todayKey, weekStart)
  readonly property string firstTaskCalendar: Model.firstTaskCalendarName(tbCalendars)
  property bool quickTodoOpen: false
  // Todos filed through the + but not yet read back from the calendar. They
  // are merged into the bucket so a new one appears on Enter rather than
  // three seconds later, and Model prunes them as the sync overtakes them.
  property var pendingTodos: []
  readonly property var bucketTasks: {
    eventRevision
    var list = root.pendingTodos.length
      ? root.visibleTaskList.concat(root.pendingTodos)
      : root.visibleTaskList
    return Model.weekTasksFor(list, root.selectedWeekKey, root.todayKey, root.weekStart)
  }
  readonly property date selectedDate: Model.dateFromKey(selectedDayKey, today)
  readonly property string clockPluginDir: (Quickshell.env("HOME") || "") + "/.config/omarchy/plugins/mailbox.clock"
  readonly property string suggestAddressScript: clockPluginDir + "/suggest-address"

  // ---- Quick-add entry pane. Right-click a day to slide this in; Esc
  //      slides back. Create asks the mailbox daemon to write, and the push
  //      that answers it is what reads the calendar back.
  property bool entryOpen: false
  property string entryKind: "event"
  property string nlText: ""
  property string entryStatus: ""
  property var tbCalendars: []

  // ---- Editing an event already on the server, opened by clicking it in
  //      the day list rather than by the + or a right-click. Non-null id
  //      is what tells commitEntry to send `event edit` instead of `event
  //      add`, and the calendar chooser to go read-only -- an edit cannot
  //      move an event to another calendar.
  property var editingEventId: null
  property bool deleteConfirming: false

  // The colour Omarchy's fastfetch logo is painted in — ANSI "green",
  // which is `green` in a named colors.toml and `color2` in an indexed
  // one. Today's cell is filled and ringed in it so the one date you look
  // for first carries the theme's signature highlight instead of the grey
  // the control-state tokens fall back to. Falls back to the shell accent.
  property color todayAccent: Color.accent
  property string formTitle: ""
  property string formDate: ""
  property string formStart: ""
  property string formEnd: ""
  property string formEndDate: ""
  property bool formEndNextDay: false
  // No toggle: an event with no start time is an all-day event, which is
  // what a phrase without a time means anyway. Clearing the time chip is how
  // an event goes back to all-day.
  readonly property bool formAllDay: root.entryKind === "event" && root.formStart === ""
  property string formLocation: ""
  // ---- Address assist. What is typed into the location row is answered
  //      with whole postal addresses; picking one writes that line and
  //      nothing else. The query is kept beside the results so an answer
  //      that lands after the word moved on can be dropped.
  property var locationSuggestions: []
  property string locationSuggestQuery: ""
  property int locationSuggestIndex: -1
  readonly property bool locationSuggestOpen: root.locationSuggestions.length > 0
  property string formDescription: ""
  property string formCalendar: ""
  property string formLink: ""
  // Which summary value is currently an inline editor ("" = display mode).
  property string editingSegment: ""
  // Roles the parser found in the phrase, as offsets into it.
  property var nlSegments: []
  // The calendar the phrase named, kept apart from the one in force: when
  // the phrase names a list this kind cannot be written to, the pane says so
  // rather than quietly filing the entry somewhere else.
  property string nlCalendarName: ""
  // Optional parts the user opened by hand. A part the phrase filled in is
  // open because it has a value, so it never needs to be listed here.
  property var openRows: []
  // What the last parse put into each part. The phrase owns what it says;
  // this is how the pane can tell a value the phrase put there — and has
  // since dropped — from one typed into a row by hand, which another word in
  // the phrase must not wipe.
  property var nlApplied: ({})

  // One value in the entry summary — the title, or a day or a time in the
  // when-card. Display mode is the value over a hairline; a click swaps it
  // for an inline field that takes the same words the phrase does.
  component SlotEdit: Item {
    id: slot
    property string slotName: ""
    property string display: ""
    property string editValue: ""
    property bool hero: false
    property bool strong: false
    property bool dim: false
    property color tint: root.contentForeground
    property int align: Text.AlignLeft
    signal commitText(string text)

    readonly property bool editing: root.editingSegment === slotName

    implicitWidth: Math.max(slotText.implicitWidth + Style.space(8), Style.space(48))
    implicitHeight: slotText.implicitHeight + Style.space(6)
    width: implicitWidth
    height: implicitHeight

    Text {
      id: slotText
      visible: !slot.editing
      anchors.fill: parent
      anchors.bottomMargin: Style.space(4)
      verticalAlignment: Text.AlignVCenter
      horizontalAlignment: slot.align
      elide: Text.ElideRight
      text: slot.display
      color: slot.dim ? Qt.darker(slot.tint, 1.7) : slot.tint
      font.family: root.contentFontFamily
      font.pixelSize: slot.hero ? Style.font.title : (slot.strong ? Style.font.heading : Style.font.body)
      font.bold: slot.hero || slot.strong
    }

    // Hairline under the value: the form-field look of the reference, and
    // the only hint that a value can be clicked.
    Rectangle {
      anchors.left: parent.left
      anchors.right: parent.right
      anchors.bottom: parent.bottom
      height: Math.max(1, Style.space(1))
      color: slot.editing
        ? Color.accent
        : (slotHover.containsMouse ? slot.tint : Qt.darker(slot.tint, 2.6))
    }

    TextField {
      id: slotEdit
      visible: slot.editing
      anchors.fill: parent
      verticalPadding: 0
      foreground: root.contentForeground
      font.family: root.contentFontFamily
      font.pixelSize: slotText.font.pixelSize
      font.bold: slotText.font.bold
      text: slot.editValue
      onAccepted: {
        slot.commitText(text)
        root.editingSegment = ""
      }
      Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Escape) {
          root.editingSegment = ""
          event.accepted = true
        }
      }
      onActiveFocusChanged: if (!activeFocus && slot.editing) {
        slot.commitText(text)
        root.editingSegment = ""
      }
    }

    MouseArea {
      id: slotHover
      anchors.fill: parent
      enabled: !slot.editing
      hoverEnabled: true
      cursorShape: Qt.PointingHandCursor
      onClicked: {
        root.editingSegment = slot.slotName
        slotEdit.text = slot.editValue
        slotEdit.forceActiveFocus()
        slotEdit.selectAll()
      }
    }
  }

  // The phrase field. A plain TextEdit does the editing but paints nothing;
  // a styled Text with identical font, width and wrapping sits exactly on
  // top of it and paints each parsed part in its own colour. Both run the
  // same layout, so the two stay glyph-for-glyph aligned — which is why the
  // editor's own text and cursor are transparent and drawn by us instead.
  component PhraseField: Item {
    id: field
    property string phrase: ""
    property string html: ""
    property string placeholderText: ""
    property real extraRightPadding: 0
    signal edited(string text)
    signal submitted()
    signal cancelled()

    readonly property real padX: Style.spacing.controlPaddingX
    readonly property real padY: Style.spacing.inputPaddingY
    readonly property bool hot: fieldHover.containsMouse
    readonly property var spec: Border.controlSpec(
      edit.activeFocus ? "focus" : (hot ? "hover-cursor" : "normal"),
      root.contentForeground, Color.accent)

    implicitHeight: edit.contentHeight + 2 * padY + Border.top(spec) + Border.bottom(spec)
    height: implicitHeight

    function focusInput() {
      edit.forceActiveFocus()
      edit.selectAll()
    }

    // Typing breaks the binding to `phrase`, so a reset from code has to
    // write the editor directly rather than trust it to follow.
    function setPhrase(text) {
      edit.text = text
    }

    BorderSurface {
      anchors.fill: parent
      radius: Style.cornerRadius
      color: Style.controlFill(edit.activeFocus, field.hot, root.contentForeground, Color.accent)
      borderSpec: field.spec
    }

    TextEdit {
      id: edit
      anchors.fill: parent
      anchors.leftMargin: field.padX + Border.left(field.spec)
      anchors.rightMargin: field.padX + Border.right(field.spec) + field.extraRightPadding
      anchors.topMargin: field.padY + Border.top(field.spec)
      anchors.bottomMargin: field.padY + Border.bottom(field.spec)
      textFormat: TextEdit.PlainText
      wrapMode: TextEdit.Wrap
      // Invisible on purpose: the overlay above paints these same glyphs in
      // the colours of the parts they were parsed as.
      color: "transparent"
      selectionColor: Style.selectionFillFor(root.contentForeground, Color.accent)
      selectedTextColor: "transparent"
      selectByMouse: true
      font.family: root.contentFontFamily
      font.pixelSize: Style.font.title
      text: field.phrase
      onTextChanged: if (text !== field.phrase) field.edited(text)

      // The default cursor takes its colour from `color`, which is
      // transparent here, so it has to be drawn.
      cursorDelegate: Rectangle {
        width: Math.max(1, Style.space(2))
        color: root.contentForeground
        visible: edit.activeFocus
        SequentialAnimation on opacity {
          running: edit.activeFocus
          loops: Animation.Infinite
          NumberAnimation { to: 0; duration: 500; easing.type: Easing.OutQuad }
          NumberAnimation { to: 1; duration: 500; easing.type: Easing.InQuad }
        }
      }

      Keys.onPressed: function(event) {
        if (event.key === Qt.Key_Escape) {
          field.cancelled()
          event.accepted = true
        } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
          field.submitted()
          event.accepted = true
        }
      }
    }

    Text {
      anchors.fill: edit
      visible: edit.text !== ""
      textFormat: Text.StyledText
      wrapMode: Text.Wrap
      font: edit.font
      color: root.contentForeground
      text: field.html
    }

    Text {
      anchors.fill: edit
      visible: edit.text === ""
      wrapMode: Text.Wrap
      font: edit.font
      color: Qt.darker(root.contentForeground, 1.8)
      text: field.placeholderText
      elide: Text.ElideRight
    }

    MouseArea {
      id: fieldHover
      anchors.fill: parent
      hoverEnabled: true
      acceptedButtons: Qt.NoButton
      cursorShape: Qt.IBeamCursor
    }
  }

  // Notes are the one part that runs past a line, so they get a box rather
  // than a field: five lines tall, scrolling once it is full. Enter puts in
  // a newline here — Ctrl+Enter is what creates, since Enter everywhere else
  // in the pane means "create".
  component NotesField: Item {
    id: notes
    property string value: ""
    property string placeholderText: ""
    property int lines: 5
    signal edited(string text)
    signal cancelled()
    signal submitted()

    readonly property real padX: Style.spacing.controlPaddingX
    readonly property real padY: Style.spacing.inputPaddingY
    readonly property bool hot: notesHover.containsMouse
    readonly property var spec: Border.controlSpec(
      area.activeFocus ? "focus" : (hot ? "hover-cursor" : "normal"),
      root.contentForeground, Color.accent)

    implicitHeight: notes.lines * notesMetrics.height + 2 * padY
      + Border.top(spec) + Border.bottom(spec)
    height: implicitHeight

    function focusInput() {
      area.forceActiveFocus()
      area.cursorPosition = area.length
    }

    FontMetrics {
      id: notesMetrics
      font: area.font
    }

    BorderSurface {
      anchors.fill: parent
      radius: Style.cornerRadius
      color: Style.controlFill(area.activeFocus, notes.hot, root.contentForeground, Color.accent)
      borderSpec: notes.spec
    }

    Flickable {
      id: notesScroll
      anchors.fill: parent
      anchors.leftMargin: notes.padX + Border.left(notes.spec)
      anchors.rightMargin: notes.padX + Border.right(notes.spec)
      anchors.topMargin: notes.padY + Border.top(notes.spec)
      anchors.bottomMargin: notes.padY + Border.bottom(notes.spec)
      contentWidth: width
      contentHeight: area.implicitHeight
      clip: true
      boundsBehavior: Flickable.StopAtBounds
      interactive: contentHeight > height

      TextEdit {
        id: area
        width: notesScroll.width
        textFormat: TextEdit.PlainText
        wrapMode: TextEdit.Wrap
        selectByMouse: true
        color: root.contentForeground
        selectionColor: Style.selectionFillFor(root.contentForeground, Color.accent)
        selectedTextColor: root.contentForeground
        font.family: root.contentFontFamily
        font.pixelSize: Style.font.body
        text: notes.value
        onTextChanged: if (text !== notes.value) notes.edited(text)

        // Keep the caret in view as the note grows past the box.
        onCursorRectangleChanged: {
          if (cursorRectangle.y < notesScroll.contentY)
            notesScroll.contentY = cursorRectangle.y
          else if (cursorRectangle.y + cursorRectangle.height > notesScroll.contentY + notesScroll.height)
            notesScroll.contentY = cursorRectangle.y + cursorRectangle.height - notesScroll.height
        }

        Keys.onPressed: function(event) {
          if (event.key === Qt.Key_Escape) {
            notes.cancelled()
            event.accepted = true
          } else if ((event.key === Qt.Key_Return || event.key === Qt.Key_Enter)
                     && (event.modifiers & Qt.ControlModifier)) {
            notes.submitted()
            event.accepted = true
          }
        }
      }
    }

    Text {
      anchors.fill: notesScroll
      visible: area.text === ""
      wrapMode: Text.Wrap
      color: Qt.darker(root.contentForeground, 1.8)
      font.family: area.font.family
      font.pixelSize: area.font.pixelSize
      text: notes.placeholderText
    }

    MouseArea {
      id: notesHover
      anchors.fill: parent
      hoverEnabled: true
      acceptedButtons: Qt.NoButton
      cursorShape: Qt.IBeamCursor
    }
  }

  // Event or task, as a pair of tabs rather than a shortcut nobody sees.
  component KindTab: Item {
    id: tab
    property string label: ""
    property bool active: false
    signal activated()

    implicitWidth: tabText.implicitWidth
    implicitHeight: tabText.implicitHeight + Style.space(6)

    Text {
      id: tabText
      anchors.top: parent.top
      anchors.horizontalCenter: parent.horizontalCenter
      text: tab.label
      color: tab.active ? Color.accent
        : (tabHover.containsMouse ? root.contentForeground : Qt.darker(root.contentForeground, 1.6))
      font.family: root.contentFontFamily
      font.pixelSize: Style.font.bodySmall
      font.bold: tab.active
    }

    Rectangle {
      anchors.bottom: parent.bottom
      anchors.horizontalCenter: parent.horizontalCenter
      width: tabText.width
      height: Math.max(1, Style.space(2))
      visible: tab.active
      color: Color.accent
    }

    MouseArea {
      id: tabHover
      anchors.fill: parent
      hoverEnabled: true
      cursorShape: Qt.PointingHandCursor
      onClicked: tab.activated()
    }
  }

  // ---- One task, as a chip rather than a row: box, title, and a note only
  //      when there is one to make. Chips flow, so a week's worth of small
  //      things takes one line instead of five — a to-do list is mostly two
  //      and three word titles, and a full-width row each throws away the
  //      width the panel already has. Only a title too long for what is left
  //      of the line takes a line of its own.
  component TaskRow: Item {
    id: taskRow
    property var task: null
    // In the bucket a task has no day of its own, so the note says how late
    // it is instead of what time it is due.
    property bool bucket: false
    // The widest the chip may be — the Flow's width, so one long title elides
    // on its own line instead of pushing the panel wider than the grid.
    property real maxWidth: parent ? parent.width : 0

    readonly property int lateDays: task ? Model.taskOverdueDays(task, root.todayKey) : 0
    readonly property color tint: (task && Model.safeColor(task.color)) || Color.accent

    // Measured off the texts' implicit widths, which do not depend on the
    // widths handed back to them — so the chip sizes itself to its content
    // without the binding chasing its own tail.
    readonly property real noteReserve: taskNote.visible ? taskNote.implicitWidth + chip.spacing : 0
    readonly property real naturalWidth: taskBox.width + chip.spacing + taskTitle.implicitWidth + noteReserve

    // Waiting on the calendar to confirm it. Dimmed rather than hidden: it
    // is on screen because you just typed it, and it should look like a thing
    // still in flight until the sync says otherwise.
    readonly property bool waiting: !!(task && task.pending)

    implicitWidth: naturalWidth
    width: maxWidth > 0 ? Math.min(naturalWidth, maxWidth) : naturalWidth
    height: Math.max(taskBox.height, taskTitle.implicitHeight) + Style.space(5)
    opacity: waiting ? 0.55 : 1

    Row {
      id: chip
      anchors.verticalCenter: parent.verticalCenter
      spacing: Style.space(7)

      Rectangle {
        id: taskBox
        anchors.verticalCenter: parent.verticalCenter
        width: Style.space(15)
        height: Style.space(15)
        radius: Style.space(4)
        // Never drawn checked: ticking one takes it off the panel, so the
        // only box that exists is an empty one. The box is where the calendar
        // colour lives — the same colour the todo's ring takes in the grid
        // above. It is why the chips do not name their list: "Aufgaben" on
        // every one is a word you stop reading, and the colour says it
        // without taking the room to say it.
        color: "transparent"
        border.width: Style.spacing.hairline * 1.5
        border.color: taskBoxMouse.containsMouse
          ? taskRow.tint
          : Util.alpha(taskRow.tint, 0.65)

        MouseArea {
          id: taskBoxMouse
          anchors.fill: parent
          // Enough to click at, but not past the gap before the title: the
          // rest of the chip belongs to the other handler.
          anchors.margins: -Style.space(2)
          enabled: !taskRow.waiting
          hoverEnabled: true
          cursorShape: Qt.PointingHandCursor
          onClicked: root.toggleTask(taskRow.task)
        }

        PanelToolTip {
          visible: taskBoxMouse.containsMouse
          text: "Mark as done"
          fontFamily: root.contentFontFamily
        }
      }

      Text {
        id: taskTitle
        anchors.verticalCenter: parent.verticalCenter
        width: Math.max(0, taskRow.width - taskBox.width - chip.spacing - taskRow.noteReserve)
        text: (taskRow.task && taskRow.task.title) || ""
        color: root.contentForeground
        font.family: root.contentFontFamily
        font.pixelSize: Style.font.body
        font.bold: !!(taskRow.task && taskRow.task.priority === "high")
        elide: Text.ElideRight
      }

      Text {
        id: taskNote
        anchors.verticalCenter: parent.verticalCenter
        // Only ever says something the title does not. A task with no due
        // date is the ordinary case and needs no label for it; the note
        // simply is not there.
        text: {
          if (taskRow.lateDays === 1) return "1 day late"
          if (taskRow.lateDays > 1) return taskRow.lateDays + " days late"
          if (!taskRow.bucket && taskRow.task && taskRow.task.time) return taskRow.task.time
          return ""
        }
        visible: text !== ""
        // Overdue is the one thing in this panel worth a warmer colour than
        // the grey the rest of the meta text takes.
        color: taskRow.lateDays > 0
          ? taskRow.tint
          : Qt.darker(root.contentForeground, 1.6)
        font.family: root.contentFontFamily
        font.pixelSize: Style.font.bodySmall
      }
    }

  }

  // A labelled meter: small-caps name on the left, percentage on the right,
  // the track between them. The year rail and the life rail are the same
  // thing measured against different spans.
  component ProgressRail: Item {
    id: rail
    property string label: ""
    property real fraction: 0
    property int percent: 0

    width: parent ? parent.width : 0
    height: Math.max(railLabel.implicitHeight, Style.space(10))

    Text {
      id: railLabel
      anchors.left: parent.left
      anchors.verticalCenter: parent.verticalCenter
      text: rail.label
      color: Qt.darker(root.contentForeground, 1.5)
      font.family: root.contentFontFamily
      font.pixelSize: Style.font.bodySmall
      font.letterSpacing: 1
    }

    Text {
      id: railPercent
      anchors.right: parent.right
      anchors.verticalCenter: parent.verticalCenter
      text: rail.percent + "%"
      color: root.contentForeground
      font.family: root.contentFontFamily
      font.pixelSize: Style.font.bodySmall
    }

    Rectangle {
      anchors.left: railLabel.right
      anchors.right: railPercent.left
      anchors.leftMargin: Style.space(12)
      anchors.rightMargin: Style.space(12)
      anchors.verticalCenter: parent.verticalCenter
      height: Style.space(6)
      radius: Style.cornerRadius > 0 ? height / 2 : 0
      color: Qt.rgba(root.contentForeground.r, root.contentForeground.g, root.contentForeground.b, 0.12)

      Rectangle {
        width: Math.round(parent.width * rail.fraction)
        height: parent.height
        radius: parent.radius
        color: Style.selectedStateColor(root.contentForeground, Color.accent)

        Behavior on width { NumberAnimation { duration: 160; easing.type: Easing.OutCubic } }
      }
    }
  }

  // The pill that opens one optional part. `focusTarget` is the control the
  // row holds, so opening a part puts the caret straight in it.
  component Pill: Button {
    property string rowName: ""
    property var focusTarget: null

    bordered: true
    horizontalPadding: Style.space(7)
    selected: root.rowOpen(rowName)
    foreground: root.contentForeground
    fontFamily: root.contentFontFamily
    onClicked: {
      root.toggleRow(rowName)
      if (!focusTarget || !root.rowOpen(rowName)) return
      // NotesField wraps its editor, so it focuses through a method of its own.
      if (typeof focusTarget.focusInput === "function") focusTarget.focusInput()
      else focusTarget.forceActiveFocus()
    }
  }

  // One optional part of the entry — location, notes, alert, repeat,
  // priority. The pill above opens it, the phrase opens it by mentioning it,
  // and the × takes the part back off the entry.
  component EntryRow: Item {
    id: entryRow
    property string rowName: ""
    property string icon: ""
    default property alias content: rowHolder.data

    width: parent ? parent.width : 0
    visible: root.rowOpen(rowName)
    height: visible ? Math.max(Style.spacing.controlHeight, rowHolder.childrenRect.height) : 0

    // Icon and × centre on the first line rather than on the row, so a tall
    // box (notes) does not push them into its middle.
    Text {
      id: rowIcon
      anchors.left: parent.left
      anchors.top: parent.top
      anchors.topMargin: Math.round((Style.spacing.controlHeight - height) / 2)
      text: entryRow.icon
      color: Qt.darker(root.contentForeground, 1.4)
      font.family: root.contentFontFamily
      font.pixelSize: Style.font.body
    }

    Item {
      id: rowHolder
      anchors.left: rowIcon.right
      anchors.leftMargin: Style.space(8)
      anchors.right: rowClear.left
      anchors.rightMargin: Style.space(6)
      anchors.top: parent.top
      height: childrenRect.height
    }

    PanelActionButton {
      id: rowClear
      anchors.right: parent.right
      anchors.top: parent.top
      anchors.topMargin: Math.round((Style.spacing.controlHeight - height) / 2)
      iconText: "󰅖"
      tooltipText: "Remove"
      foreground: root.contentForeground
      fontFamily: root.contentFontFamily
      onClicked: root.clearRow(entryRow.rowName)
    }
  }

  property int formDuration: 0
  property int formAlertMinutes: 0
  property var formRecurrence: null
  property string formPriority: ""
  readonly property var calendarChoices: Model.calendarOptions(tbCalendars, entryKind)
  readonly property var calendarColorMap: Model.calendarColors(visibleEventList)

  // ---- Phrase colours. Omarchy themes are near-monochrome, so the parts of
  //      a phrase get hues of their own rather than shades of the accent —
  //      the whole point is telling a date from a time from a calendar at a
  //      glance. Lightness follows the popup surface so both dark and light
  //      themes stay readable, and the calendar part borrows the colour
  //      the server already paints that calendar in.
  readonly property bool darkSurface: Color.popups.background.hslLightness < 0.5
  function roleTint(hue, saturation) {
    return Qt.hsla(hue, saturation === undefined ? 0.62 : saturation,
                   root.darkSurface ? 0.70 : 0.38, 1.0)
  }
  readonly property color roleDateColor: roleTint(0.78)
  readonly property color roleTimeColor: roleTint(0.86)
  readonly property color roleDurationColor: roleTint(0.92)
  readonly property color roleAlertColor: roleTint(0.12)
  readonly property color roleRepeatColor: roleTint(0.50)
  readonly property color rolePriorityColor: roleTint(0.99)
  readonly property color roleLocationColor: roleTint(0.42)
  readonly property color roleLinkColor: roleTint(0.60, 0.85)
  readonly property color roleCalendarColor: {
    var own = root.formCalendar ? root.calendarColorMap[root.formCalendar] : ""
    return own ? own : roleTint(0.97, 0.55)
  }

  // StyledText wants "#rrggbb"; a QML colour stringifies with its alpha.
  function hexColor(value) {
    function part(v) {
      var text = Math.round(Math.max(0, Math.min(1, v)) * 255).toString(16)
      return text.length < 2 ? "0" + text : text
    }
    return "#" + part(value.r) + part(value.g) + part(value.b)
  }

  readonly property var phraseColors: ({
    title: hexColor(root.contentForeground),
    date: hexColor(root.roleDateColor),
    time: hexColor(root.roleTimeColor),
    duration: hexColor(root.roleDurationColor),
    alert: hexColor(root.roleAlertColor),
    repeat: hexColor(root.roleRepeatColor),
    priority: hexColor(root.rolePriorityColor),
    location: hexColor(root.roleLocationColor),
    link: hexColor(root.roleLinkColor),
    calendar: hexColor(root.roleCalendarColor)
  })
  readonly property string phraseMarkup: Model.phraseHtml(root.nlText, root.nlSegments, root.phraseColors)

  // ---- Optional parts. A part is on screen when the phrase gave it a value
  //      or its pill was clicked; the pill and the row's × are the same
  //      switch seen from two places.
  function segmentValue(name) {
    if (name === "link") return root.formLink
    if (name === "location") return root.formLocation
    if (name === "notes") return root.formDescription
    if (name === "alert") return root.formAlertMinutes > 0 ? root.alertLabel() : ""
    if (name === "repeat") return root.formRecurrenceValue
    if (name === "priority") return root.formPriority
    return ""
  }

  function rowOpen(name) {
    return root.openRows.indexOf(name) !== -1 || root.segmentValue(name) !== ""
  }

  function openRow(name) {
    if (root.openRows.indexOf(name) !== -1) return
    var next = root.openRows.slice()
    next.push(name)
    root.openRows = next
  }

  function closeRow(name) {
    var next = []
    for (var i = 0; i < root.openRows.length; i++)
      if (root.openRows[i] !== name) next.push(root.openRows[i])
    root.openRows = next
  }

  function clearRow(name) {
    if (name === "link") root.formLink = ""
    else if (name === "location") root.formLocation = ""
    else if (name === "notes") root.formDescription = ""
    else if (name === "alert") root.formAlertMinutes = 0
    else if (name === "repeat") root.formRecurrence = null
    else if (name === "priority") root.formPriority = ""
    if (name === "location") root.closeAddressSuggestions()
    root.closeRow(name)
  }

  // ---- Address assist. Typing waits out a short pause before asking, so a
  //      word costs one request rather than one per keystroke, and only the
  //      location row ever asks — the phrase is never sent anywhere.
  function requestAddressSuggestions(text) {
    var query = String(text || "")
    if (!Model.shouldSuggestAddress(query)) { root.closeAddressSuggestions(); return }
    root.locationSuggestQuery = query.trim()
    suggestTimer.restart()
  }

  function fetchAddressSuggestions() {
    if (!root.locationSuggestQuery) return
    suggestProc.running = false
    suggestProc.command = [root.suggestAddressScript, root.locationSuggestQuery]
    suggestProc.running = true
  }

  // Only the answer to the word still standing in the field is shown: a
  // slower reply to something already typed over is thrown away.
  function applyAddressSuggestions(raw) {
    var reply = Model.parseAddressSuggestions(raw)
    if (!root.entryOpen || !root.rowOpen("location")) return
    if (reply.query !== root.locationSuggestQuery) return
    if (reply.items.length === 1 && reply.items[0].value === root.formLocation) {
      root.closeAddressSuggestions()
      return
    }
    root.locationSuggestions = reply.items
    root.locationSuggestIndex = reply.items.length ? 0 : -1
  }

  function closeAddressSuggestions() {
    suggestTimer.stop()
    root.locationSuggestions = []
    root.locationSuggestIndex = -1
    root.locationSuggestQuery = ""
  }

  function moveAddressSuggestion(delta) {
    if (!root.locationSuggestOpen) return
    var count = root.locationSuggestions.length
    root.locationSuggestIndex = (root.locationSuggestIndex + delta + count) % count
  }

  // A pick is the whole address as one line — street, number, postcode,
  // city — which is what the calendar stores and what the travel-time lookup
  // can geocode later.
  function takeAddressSuggestion(index) {
    var row = root.locationSuggestions[index === undefined ? root.locationSuggestIndex : index]
    if (!row) return false
    root.formLocation = row.value
    // Typing broke the field's binding to formLocation, as editing a
    // TextField always does — so the pick is written to the field itself,
    // with the caret left after it rather than mid-address.
    if (locationField) {
      locationField.text = row.value
      locationField.cursorPosition = row.value.length
    }
    root.closeAddressSuggestions()
    return true
  }

  // Keys in the location field. While the list is up it owns Up/Down, Enter
  // and Esc — Enter picks an address rather than creating the entry, and Esc
  // closes the list before it closes the pane.
  function handleLocationKey(event) {
    if (root.locationSuggestOpen) {
      if (event.key === Qt.Key_Down) { root.moveAddressSuggestion(1); event.accepted = true; return }
      if (event.key === Qt.Key_Up) { root.moveAddressSuggestion(-1); event.accepted = true; return }
      if (event.key === Qt.Key_Escape) { root.closeAddressSuggestions(); event.accepted = true; return }
      if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
        if (root.takeAddressSuggestion()) { event.accepted = true; return }
      }
    }
    root.handleEntryKey(event)
  }

  // Clicking a pill that already holds a value takes the part off again —
  // one switch, both directions.
  function toggleRow(name) {
    if (root.segmentValue(name) !== "") root.clearRow(name)
    else if (root.openRows.indexOf(name) !== -1) root.closeRow(name)
    else root.openRow(name)
  }

  // ---- Calendars. The event roster and the task roster are different lists
  //      even where the server lets one collection hold both, so every path
  //      that can set a calendar — the chooser, the phrase, a kind switch,
  //      reopening the pane — runs the choice past the current kind.
  function calendarValidForKind(name) {
    if (!root.calendarChoices.length) return true
    for (var i = 0; i < root.calendarChoices.length; i++)
      if (root.calendarChoices[i].value === name) return true
    return false
  }

  function ensureCalendarForKind() {
    if (root.formCalendar !== "" && root.calendarValidForKind(root.formCalendar)) return
    root.formCalendar = root.calendarChoices.length ? root.calendarChoices[0].value : ""
  }

  // ---- The when-card. End day is only stored when it differs from the
  //      start day, so the card works out what to show.

  // Qt's date formatting rather than JavaScript's: the QML engine ignores
  // toLocaleDateString's options object, so the month name and the weekday
  // come from Qt.formatDate and the locale.
  function dayLabel(dateKey) {
    var d = Model.dateFromKey(dateKey, null)
    if (!d) return String(dateKey || "")
    var body = Qt.formatDate(d, d.getFullYear() === root.today.getFullYear() ? "d MMM" : "d MMM yyyy")
    var kind = Model.relativeDayKind(dateKey, root.todayKey)
    if (kind === "today") return "Today, " + body
    if (kind === "tomorrow") return "Tomorrow, " + body
    if (kind === "yesterday") return "Yesterday, " + body
    if (kind === "weekday") return Qt.locale().dayName(d.getDay(), Locale.LongFormat) + ", " + body
    return body
  }

  function entryEndDateKey() {
    if (root.formEndDate) return root.formEndDate
    if (root.formEndNextDay && !root.formAllDay) {
      var d = Model.dateFromKey(root.formDate, null)
      if (d) {
        d.setDate(d.getDate() + 1)
        return Model.keyForDate(d)
      }
    }
    return root.formDate
  }

  function commitStartDate(text) {
    var key = Model.parseDateInput(text, root.selectedDayKey, Date.now())
    if (key) root.formDate = key
  }

  function commitEndDate(text) {
    var key = Model.parseDateInput(text, root.formDate || root.selectedDayKey, Date.now())
    if (!key) return
    root.formEndNextDay = false
    root.formEndDate = key === root.formDate ? "" : key
  }

  function commitStartTime(text) {
    var value = Model.parseTimeInput(text)
    if (!value) {
      // Emptying the start empties the span with it: an end alone would be
      // an event that finishes without starting.
      if (String(text || "").trim() === "") {
        root.formStart = ""
        root.formEnd = ""
        root.formEndNextDay = false
      }
      return
    }
    root.formStart = value
  }

  function commitEndTime(text) {
    // The first time typed into either chip is the start — an end on its own
    // has nothing to end.
    if (root.formStart === "") {
      root.commitStartTime(text)
      return
    }
    var value = Model.parseTimeInput(text)
    if (!value) {
      if (String(text || "").trim() === "") root.formEnd = ""
      return
    }
    root.formEnd = value
  }
  readonly property string formRecurrenceValue: {
    var r = formRecurrence
    return r && r.freq ? r.freq + ":" + (r.interval || 1) : ""
  }

  function alertLabel() {
    var v = root.formAlertMinutes
    if (!v) return ""
    if (v % 1440 === 0) return (v / 1440) + "d"
    if (v % 60 === 0) return (v / 60) + "h"
    return v + "m"
  }

  // Tick a task off, or put it back. The box flips under the cursor before
  // the server has heard about it: a write round trip is a second or two,
  // and a checkbox that waits that long feels broken. The sync a moment later
  // is what makes it true — and if the write failed, the task comes back
  // unticked, which is the honest outcome rather than a lie that sticks.
  // The + on the bucket header becomes the field itself. Enter files the
  // todo and clears the field without closing it, because things you add
  // this way come in threes; Esc puts the + back.
  function openQuickTodo() {
    root.quickTodoOpen = true
    // The field is still invisible this frame, and an invisible item cannot
    // take focus — same reason the life-expectancy fields defer it.
    Qt.callLater(function() {
      quickTodoField.text = ""
      quickTodoField.forceActiveFocus()
    })
  }

  function closeQuickTodo() {
    root.quickTodoOpen = false
    quickTodoField.text = ""
  }

  function commitQuickTodo() {
    var built = Model.buildQuickTodoRequest(quickTodoField.text, root.firstTaskCalendar)
    if (!built.ok) {
      // Enter on an empty field is how you back out without reaching for Esc.
      root.closeQuickTodo()
      return
    }
    var sent = Model.requestToArgs(built.request)
    if (sent) mailbox.call(sent.cmd, sent.args)

    // Into the list on Enter, not when the push comes back. The write is
    // the slow part; being told it happened should not be.
    var placeholder = Model.pendingTodo(
      built.request.title, root.firstTaskCalendar, Date.now(), root.visibleTaskList)
    if (placeholder) {
      root.pendingTodos = root.pendingTodos.concat([placeholder])
      root.eventRevision += 1
    }

    quickTodoField.text = ""
    quickTodoField.forceActiveFocus()
  }

  function toggleTask(task) {
    // A placeholder has no UID yet, so there is nothing to tick off on.
    if (!task || task.pending) return
    var request = Model.buildCompleteRequest(task, Date.now())
    if (!request) return
    var sent = Model.requestToArgs(request)
    if (!sent) return
    task.done = request.done
    task.completedAt = request.done ? new Date(request.completedMs).toISOString() : ""
    root.taskIndex = Model.indexTasksByDate(root.visibleTaskList, root.todayKey)
    root.eventRevision += 1
    // The daemon pushes todo.changed when the write lands; a failure comes
    // back on this very call, and the optimistic tick is taken off again.
    mailbox.call(sent.cmd, sent.args, function (error) {
      if (!error) return
      task.done = !request.done
      task.completedAt = ""
      root.taskIndex = Model.indexTasksByDate(root.visibleTaskList, root.todayKey)
      root.eventRevision += 1
    })
  }

  function selectDay(key) {
    root.selectedDayKey = String(key)
  }

  // The blank slate both a fresh + and an edit start from. Kept as one
  // function so an edit cannot inherit a stray field a previous add left
  // behind -- the two follow-up calls (applyDraft with a fallback draft,
  // or with the event being opened) are the only difference between them.
  function resetEntryState(dayKey) {
    if (dayKey) root.selectedDayKey = String(dayKey)
    root.entryKind = "event"
    root.editingEventId = null
    root.deleteConfirming = false
    root.nlText = ""
    root.nlSegments = []
    root.nlCalendarName = ""
    root.openRows = []
    root.editingSegment = ""
    root.entryStatus = ""
    // A calendar chosen last time must not outlive the pane: it may belong
    // to the other kind entirely.
    root.formCalendar = ""
    root.nlApplied = ({})
    // mergeEntryDraft falls back to whatever this already holds whenever
    // neither a typed phrase nor the draft names a date explicitly via a
    // segment (fallbackDraft and draftFromAgendaEvent never do) — left
    // unset, a pane opened on one day would keep showing whatever day the
    // last pane was on.
    root.formDate = ""
    root.formStart = ""
    root.formEnd = ""
    root.formEndDate = ""
    root.formLocation = ""
    root.closeAddressSuggestions()
    root.formDescription = ""
    root.formLink = ""
    root.formAlertMinutes = 0
    root.formRecurrence = null
    root.formPriority = ""
  }

  function openEntry(dayKey) {
    root.resetEntryState(dayKey)
    root.applyDraft(Model.fallbackDraft("", root.selectedDayKey, "event"))
    root.entryOpen = true
    Qt.callLater(function() {
      if (nlField) {
        nlField.setPhrase("")
        nlField.focusInput()
      }
    })
  }

  // Opens the entry pane on an event already on the server, filled with
  // what the day list already knew about it. The description, link,
  // alarms and repeat rule are not part of that agenda row, so they arrive
  // a moment later from `event view` and are patched in directly -- not
  // through another applyDraft, whose merge would read their absence from
  // the first draft as the phrase clearing them (see mergeEntryDraft).
  function openEventEdit(event) {
    if (!event || !event.objectId) return
    root.resetEntryState(event.dateKey)
    root.editingEventId = event.objectId
    root.applyDraft(Model.draftFromAgendaEvent(event))
    root.entryOpen = true
    Qt.callLater(function() {
      if (nlField) nlField.setPhrase("")
    })
    var editingId = event.objectId
    mailbox.call(["event", "view"], { positional: String(editingId) }, function (error, data) {
      // Superseded by a close, or by opening a different event, while the
      // read was in flight.
      if (root.editingEventId !== editingId) return
      if (error) { root.entryStatus = error; return }
      var detail = Model.draftFromEventDetail(data, root.formDate)
      if (detail.description) root.formDescription = detail.description
      if (detail.link) root.formLink = detail.link
      if (detail.alertMinutes) root.formAlertMinutes = detail.alertMinutes
      if (detail.recurrence) root.setRecurrenceFrom(detail.recurrence.freq + ":" + detail.recurrence.interval)
    })
  }

  function closeEntry() {
    root.closeAddressSuggestions()
    root.entryOpen = false
    root.entryStatus = ""
    root.editingEventId = null
    root.deleteConfirming = false
    Qt.callLater(function() { if (keyCatcher) keyCatcher.forceActiveFocus() })
  }

  // The parse merged into what is on screen. Model.mergeEntryDraft owns the
  // rules — the phrase wins where it speaks, hand edits stand where it is
  // silent, and everything derived counts as the phrase's so the next parse
  // can move it. The calendar is settled here rather than there, because only
  // the pane knows which collections the mirror offered.
  function applyDraft(d) {
    if (!d) return
    var merged = Model.mergeEntryDraft(d, {
      date: root.formDate,
      start: root.formStart,
      end: root.formEnd,
      endDate: root.formEndDate,
      location: root.formLocation,
      notes: root.formDescription,
      link: root.formLink,
      alert: root.formAlertMinutes,
      repeat: root.formRecurrenceValue,
      priority: root.formPriority
    }, root.nlApplied, root.entryKind)
    var v = merged.values

    root.formTitle = d.title || ""
    root.formDate = v.date || root.selectedDayKey
    root.formStart = v.start
    root.formEnd = v.end
    root.formEndDate = v.endDate
    root.formEndNextDay = v.endNextDay
    root.formLocation = v.location
    root.formDescription = v.notes
    root.formLink = v.link
    root.formDuration = d.durationMinutes || 0
    root.formAlertMinutes = v.alert
    root.setRecurrenceFrom(v.repeat)
    root.formPriority = v.priority
    root.nlApplied = merged.applied

    // A name out of the phrase still has to be a calendar this kind can be
    // written to: "/Aufgaben" on an event would land in a list that takes
    // tasks only, so it is ignored rather than silently mis-filed.
    if (d.calendarName && root.calendarValidForKind(d.calendarName)) {
      root.formCalendar = d.calendarName
    }
    root.ensureCalendarForKind()
  }

  function applyPhrase() {
    var known = []
    for (var i = 0; i < root.tbCalendars.length; i++)
      known.push(root.tbCalendars[i].name)
    var parsed = Model.parseEventPhrase(root.nlText, root.selectedDayKey, Date.now(), known)
    if (!parsed) parsed = Model.fallbackDraft(root.nlText, root.selectedDayKey, root.entryKind)
    parsed.kind = root.entryKind
    root.nlSegments = parsed.segments || []
    root.nlCalendarName = parsed.calendarName || ""
    root.applyDraft(parsed)
  }

  function assembleDraft() {
    var start = String(root.formStart || "").trim()
    var end = String(root.formEnd || "").trim()
    var wrap = root.formEndNextDay
    if (!wrap && start && end && end <= start) wrap = true
    return {
      kind: root.entryKind,
      title: root.formTitle,
      dateKey: root.formDate || root.selectedDayKey,
      endDateKey: root.formEndDate || null,
      startTime: start || null,
      endTime: end || null,
      endNextDay: wrap,
      durationMinutes: root.formDuration || null,
      allDay: root.formAllDay,
      location: root.formLocation || null,
      description: root.formDescription || null,
      calendarName: root.formCalendar || null,
      link: root.formLink || null,
      alertMinutes: root.formAlertMinutes || null,
      recurrence: root.formRecurrence,
      priority: root.formPriority || null,
      editingId: root.entryKind === "event" ? root.editingEventId : null
    }
  }

  // What Create would write, or why it cannot — one build for the error line
  // and the button, since two calls are two Date.now()s and twice the work.
  readonly property var entryCheck: Model.buildQuickAddRequest(root.assembleDraft(), Date.now())

  function commitEntry() {
    var built = Model.buildQuickAddRequest(root.assembleDraft(), Date.now())
    if (!built.ok) {
      root.entryStatus = built.error || "could not create"
      return
    }
    root.entryStatus = root.editingEventId ? "Saving…" : "Adding…"
    var sent = Model.requestToArgs(built.request)
    if (!sent) {
      root.entryStatus = "could not create"
      return
    }
    mailbox.call(sent.cmd, sent.args, function (error) {
      if (error) {
        root.entryStatus = error
        return
      }
      root.entryStatus = "✓  " + Model.formatEntrySummary(built.request)
      entryStatusTimer.restart()
      // The daemon pushes event.changed as the write lands; the re-ask
      // below is the read that shows it.
      root.askCalendar()
    })
  }

  // The delete button is a two-click confirm rather than a native dialog —
  // Escape or clicking anywhere else in the pane backs out of it the same
  // way it backs out of everything else here.
  function deleteEditingEvent() {
    if (!root.editingEventId) return
    if (!root.deleteConfirming) {
      root.deleteConfirming = true
      return
    }
    var sent = Model.requestToArgs({ kind: "event", action: "delete", id: root.editingEventId })
    if (!sent) return
    root.entryStatus = "Deleting…"
    mailbox.call(sent.cmd, sent.args, function (error) {
      if (error) {
        root.entryStatus = error
        root.deleteConfirming = false
        return
      }
      root.closeEntry()
      // The daemon pushes event.changed as the delete lands; the re-ask
      // below is the read that drops it from the day list.
      root.askCalendar()
    })
  }

  function handleEntryKey(event) {
    if (event.key === Qt.Key_Escape) {
      if (root.deleteConfirming) root.deleteConfirming = false
      else root.closeEntry()
      event.accepted = true
    } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
      // Enter anywhere in the entry pane means Create — the form always
      // mirrors exactly what would be written.
      root.commitEntry()
      event.accepted = true
    }
  }

  function setEntryKind(kind) {
    // Editing one specific event has nothing to switch to; the tabs are
    // hidden for it, but the shortcuts that drive them are still live.
    if (root.editingEventId) return
    root.entryKind = kind === "task" ? "task" : "event"
    if (root.nlText) root.applyPhrase()
    root.ensureCalendarForKind()
  }

  // ---- The mailbox daemon. Three reads build the document the panel
  //      draws: the roster of collections, the agenda window, the open
  //      todos. All three are Mirror reads, so they answer instantly and
  //      offline; the daemon pushes calendar.changed, event.changed and
  //      todo.changed as things move, and each push is one re-ask.
  property int readEpoch: 0
  property int readsWaiting: 0
  property var lastRead: ({})

  function askCalendar() {
    var epoch = root.readEpoch + 1
    root.readEpoch = epoch
    root.readsWaiting = 3
    var from = new Date()
    from.setDate(from.getDate() - 21)
    // A wire date, not a display date: the daemon parses 2026-08-29.
    var fromKey = Model.dateKey(from.getFullYear(), from.getMonth(), from.getDate())
    // A call with no arguments sends no args field at all: the daemon reads
    // args as an object, and an empty array is a malformed request it cannot
    // even answer by id.
    mailbox.call(["calendar", "list"], undefined, function (error, data) {
      root.readAnswered(epoch, "calendars", error, data)
    })
    mailbox.call(["agenda"], { from: fromKey, days: 112 }, function (error, data) {
      root.readAnswered(epoch, "agenda", error, data)
    })
    mailbox.call(["todo", "list"], { all: true }, function (error, data) {
      root.readAnswered(epoch, "todos", error, data)
    })
  }

  // One stale answer is dropped, not applied: a re-ask superseded it and a
  // newer round is already on its way.
  function readAnswered(epoch, key, error, data) {
    if (epoch !== root.readEpoch) return
    var answers = root.lastRead
    answers[key] = error ? null : data
    if (error) console.warn("mailbox.clock", key, error)
    root.lastRead = answers
    root.readsWaiting -= 1
    if (root.readsWaiting <= 0) root.applyMailbox(epoch)
  }

  function applyMailbox(epoch) {
    if (epoch !== root.readEpoch) return
    var roster = Model.mailboxRoster(root.lastRead.calendars)
    root.applyRoster(roster)
    root.applyDocument(Model.mailboxDocument(roster, root.lastRead.agenda, root.lastRead.todos, Date.now()))
  }

  // The roster arrives already shaped by Model.mailboxRoster -- one row per
  // calendar and task list, with the events/tasks flags the chooser filters
  // on. Nothing left to parse: it is a list of objects, not a document.
  function applyRoster(list) {
    root.tbCalendars = list || []
    root.ensureCalendarForKind()
  }

  function applyDocument(doc) {
    root.eventDoc = doc
    root.visibleEventList = doc && doc.events ? doc.events : []
    root.eventIndex = Model.indexEventsByDate(root.visibleEventList)
    root.visibleTaskList = Model.tasksFromDocument(doc)
    root.taskIndex = Model.indexTasksByDate(root.visibleTaskList, root.todayKey)
    root.pendingTodos = Model.prunePendingTodos(root.pendingTodos, root.visibleTaskList, Date.now())
    root.eventRevision += 1
  }

  function applyPalette(raw) {
    var text = String(raw || "")
    var named = text.match(/^[ \t]*green[ \t]*=[ \t]*["']?(#[0-9A-Fa-f]{6})/m)
    var indexed = text.match(/^[ \t]*color2[ \t]*=[ \t]*["']?(#[0-9A-Fa-f]{6})/m)
    var hit = (named && named[1]) || (indexed && indexed[1]) || ""
    root.todayAccent = hit ? hit : Color.accent
  }

  function setRecurrenceFrom(value) {
    var text = String(value || "")
    if (!text) { root.formRecurrence = null; return }
    var parts = text.split(":")
    root.formRecurrence = { freq: parts[0], interval: parseInt(parts[1] || "1", 10) || 1 }
  }

  // Qt.openUrlExternally rather than the shell helper on purpose. That helper
  // runs `bash -lc`, and a meeting link is supplied by whoever sent the
  // invitation, so putting it through a shell would be a command injection.
  // Model.safeUrl also refuses anything that is not plain https.
  function openExternally(url) {
    if (!url) return
    Qt.openUrlExternally(url)
    root.close()
  }

  function openMeeting(event) {
    var url = Model.meetingUrlFor(event)
    if (!url) return
    if (root.hostWidget && typeof root.hostWidget.dismissReminder === "function")
      root.hostWidget.dismissReminder(event)
    root.openExternally(url)
  }

  function formatSelectedHeroLabel() {
    return Qt.formatDate(root.selectedDate, "MMMM d")
  }


  // Guarded so the widget renders before the bar is injected (the bar-widget
  // contract instantiates it bare).
  readonly property color contentForeground: bar ? bar.foreground : Color.foreground
  readonly property string contentFontFamily: bar ? bar.fontFamily : Style.font.family

  readonly property int cellWidth: Style.space(52)
  readonly property int cellHeight: Style.space(34)
  readonly property int cellSpacing: Style.space(2)
  readonly property int weekColumnWidth: Style.space(32)
  readonly property int gutterWidth: Style.space(14)

  function open() {
    paletteFile.reload()
    refresh()
    root.askCalendar()
    root.controller.show()
    // Set after showing, not before: showing hands the popout coordinator
    // over, which closes whichever panel was open, and that close clears the
    // shared flag. Deferring means the panel taking over always wins, while
    // a handoff to a panel that does not manage the flag still leaves it
    // cleared rather than stuck on.
    Qt.callLater(function() {
      if (root.opened) setCenterHoverRevealSuppressed(true)
    })
  }

  function close() {
    root.closeQuickTodo()
    setCenterHoverRevealSuppressed(false)
    // Dismissing the panel mid-edit would otherwise leave the inputs up,
    // waiting behind a closed popup for the next time it opens.
    if (root.editingLife) root.cancelEditingLife()
    if (root.entryOpen) root.closeEntry()
    root.controller.hide()
  }

  function toggle() {
    if (root.opened) root.close()
    else root.open()
  }

  function switchPanel(direction) {
    if (root.bar && typeof root.bar.switchPanelFrom === "function")
      return root.bar.switchPanelFrom(root.barIdentity, direction)
    return false
  }

  // Summoning by hotkey moves no pointer, so a hover the bar was still
  // holding must not keep the center indicators revealed behind the panel.
  function setCenterHoverRevealSuppressed(value) {
    if (root.bar && "centerHoverRevealSuppressed" in root.bar)
      root.bar.centerHoverRevealSuppressed = value
  }

  function refresh() {
    root.today = new Date()
    root.goToToday()
  }

  function goToToday() {
    root.viewYear = today.getFullYear()
    root.viewMonth = today.getMonth()
    root.selectedDayKey = root.todayKey
  }

  function moveMonth(delta) {
    var next = Model.stepMonth(viewYear, viewMonth, delta)
    root.viewYear = next.year
    root.viewMonth = next.month
    if (next.year === today.getFullYear() && next.month === today.getMonth())
      root.selectedDayKey = root.todayKey
    else
      root.selectedDayKey = Model.dateKey(next.year, next.month, 1)
  }

  function moveYear(delta) {
    moveMonth(delta * 12)
  }

  // Applied locally first so the panel redraws on the click itself; the
  // shell.json write comes back through the bar as the same value. With no
  // writable entry (the widget is not in the layout) it stays a session-only
  // preference rather than doing nothing. The host widget builds its own
  // entry when the label format is cycled, so it has to be kept in step or
  // it would write this key straight back out from a stale copy.
  function persistSettings(values) {
    var entry = { id: root.moduleName }
    for (var existing in root.settings) if (existing !== "id") entry[existing] = root.settings[existing]
    for (var key in values) entry[key] = values[key]

    root.settings = entry
    if (root.hostWidget && "settings" in root.hostWidget) root.hostWidget.settings = entry
    if (root.bar && root.bar.shell && typeof root.bar.shell.updateEntryInline === "function")
      root.bar.shell.updateEntryInline(root.moduleName, entry)
  }

  function setWeekStart(day) {
    var next = Model.normalizedWeekStart(day, root.weekStart)
    if (next === root.weekStart) return
    persistSettings({ weekStartDay: Model.weekStartSettingName(next) })
  }

  function startEditingLife() {
    root.editingLife = true
    Qt.callLater(function() {
      bornField.text = root.birthYear > 0 ? String(root.birthYear) : ""
      expectancyField.text = String(root.lifeExpectancy)
      bornField.selectAll()
      bornField.forceActiveFocus()
    })
  }

  function cancelEditingLife() {
    root.editingLife = false
    Qt.callLater(function() { if (keyCatcher) keyCatcher.forceActiveFocus() })
  }

  // Shared by both fields: Tab hops to the other one, Enter commits the pair,
  // Escape drops the lot.
  function handleLifeKey(event, other) {
    if (event.key === Qt.Key_Escape) {
      root.cancelEditingLife()
      event.accepted = true
    } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
      root.commitLife()
      event.accepted = true
    } else if (event.key === Qt.Key_Tab || event.key === Qt.Key_Backtab) {
      other.selectAll()
      other.forceActiveFocus()
      event.accepted = true
    }
  }

  // Double-tapping the life bar puts it away again. The expectancy stays in
  // the config so setting a birth year again brings your own number back
  // rather than the default.
  function clearLife() {
    if (root.birthYear <= 0) return
    persistSettings({ birthYear: 0 })
  }

  function commitLife() {
    var born = Model.parseBirthYear(bornField.text, today.getFullYear())
    var span = Model.parseLifeExpectancy(expectancyField.text)
    if (born !== root.birthYear || span !== root.lifeExpectancy)
      persistSettings({ birthYear: born, lifeExpectancy: span })
    cancelEditingLife()
  }

  function toggleWeekStart() {
    setWeekStart(Model.toggledWeekStart(root.weekStart))
  }

  // Locale short day names, trimmed of the trailing period some locales
  // carry ("man." -> "MAN") so the header row stays a clean band of caps.
  function weekdayLabel(weekday) {
    return String(Qt.locale().dayName(weekday, Locale.ShortFormat)).replace(/\.$/, "").toUpperCase()
  }

  // The daemon, as one object. Reads answer from the Mirror — instantly,
  // offline — and every write is one request on the socket that already
  // holds the CalDAV session. The daemon pushes on change; the handler
  // below re-asks, so a calendar that moved anywhere is redrawn without
  // anybody clicking refresh.
  MailboxService {
    id: mailbox
    onConnected: root.askCalendar()
    onPushed: function (message) {
      var event = String(message.event || "")
      if (event === "calendar.changed" || event === "event.changed"
          || event === "todo.changed" || event === "habit.changed")
        root.askCalendar()
    }
  }

  // One request per pause in the typing, not per keystroke.
  Timer {
    id: suggestTimer
    interval: 280
    repeat: false
    onTriggered: root.fetchAddressSuggestions()
  }

  // Confirmation reads, then the pane returns to the overview on its own.
  Timer {
    id: entryStatusTimer
    interval: 900
    repeat: false
    onTriggered: {
      if (root.entryOpen && root.entryStatus.indexOf("✓") === 0) root.closeEntry()
    }
  }

  Process {
    id: suggestProc
    running: false
  }

  FileView {
    id: suggestFile
    path: (Quickshell.env("HOME") || "") + "/.cache/omarchy/mailbox.clock/location-suggest.json"
    watchChanges: true
    printErrors: false
    onLoaded: root.applyAddressSuggestions(text())
    onFileChanged: reload()
  }

  // The active theme's palette, for the one colour the shell's Color
  // singleton does not carry (ANSI green). Theme switches replace this
  // file wholesale; open() re-reads it and the watch catches changes made
  // while the panel is up.
  FileView {
    id: paletteFile
    path: (Quickshell.env("HOME") || "") + "/.local/state/omarchy/current/theme/colors.toml"
    watchChanges: true
    printErrors: false
    onLoaded: root.applyPalette(text())
    onLoadFailed: root.applyPalette("")
    onFileChanged: reload()
  }

  Component.onCompleted: Qt.callLater(function() { paletteFile.reload() })

  SystemClock {
    id: clock
    precision: SystemClock.Minutes
    onDateChanged: {
      if (Model.keyForDate(clock.date) === String(root.todayKey)) return
      var followToday = root.viewingCurrentMonth
      root.today = clock.date
      if (followToday) root.goToToday()
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: root.anchorItem
    owner: root.barIdentity
    bar: root.bar
    open: root.opened
    centerOnBar: true
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(560))
    contentHeight: panel.fittedContentHeight(root.entryOpen ? entryColumn.implicitHeight : calendarColumn.implicitHeight)

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      blocked: root.editingLife || root.entryOpen
      onMoveRequested: function(dx, dy) {
        if (dx !== 0) root.moveMonth(dx)
        if (dy !== 0) root.moveYear(dy)
      }
      onActivateRequested: root.goToToday()
      onCloseRequested: root.entryOpen ? root.closeEntry() : root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) {
        if (t === "[") root.moveMonth(-1)
        else if (t === "]") root.moveMonth(1)
        else if (t === "{") root.moveYear(-1)
        else if (t === "}") root.moveYear(1)
        else if (t === "t" || t === "T") root.goToToday()
        else if (t === "w" || t === "W") root.toggleWeekStart()
      }

      Item {
        id: paneViewport
        anchors.fill: parent
        clip: true

      Item {
        id: overviewPane
        width: parent.width
        height: parent.height
        x: root.entryOpen ? -width : 0
        Behavior on x { NumberAnimation { duration: 220; easing.type: Easing.OutCubic } }

      Flickable {
        id: calendarScroll
        anchors.fill: parent
        contentWidth: calendarColumn.width
        contentHeight: calendarColumn.implicitHeight
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        interactive: contentHeight > height || contentWidth > width

        Column {
          id: calendarColumn
          // Never narrower than the grid. The popup width is capped to what
          // the screen allows, and a fixed seven-column grid would otherwise
          // lose its last days off the edge instead of scrolling.
          width: Math.max(calendarScroll.width, gridColumn.width)
          spacing: Style.space(8)

          // ---- Hero: today, centered. Once the view has stepped back
          //      it is also the way home — clicking the date you are
          //      looking for beats hunting for a reset button.
          Item {
            width: parent.width
            height: heroRow.height

            Row {
              id: heroRow
              anchors.horizontalCenter: parent.horizontalCenter
              spacing: Style.space(22)

              Text {
                id: heroIcon
                // Baseline-aligned, not center-aligned: "July 26" carries a
                // descender, so centering the two boxes leaves the icon
                // sitting visibly low against the digits.
                anchors.baseline: heroDate.baseline
                text: "󰃭"
                color: root.contentForeground
                font.family: root.contentFontFamily
                // Decorative, and deliberately outside the Style.font.*
                // scale. Sized so the glyph reads at the cap height of the
                // date beside it rather than towering over it.
                font.pixelSize: 48
              }

              Text {
                id: heroDate
                anchors.verticalCenter: parent.verticalCenter
                text: root.formatSelectedHeroLabel()
                color: heroDateMouse.containsMouse
                  ? Style.hoverStateColor(root.contentForeground, Color.accent)
                  : root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: 52
                font.bold: true
              }
            }

            MouseArea {
              id: heroDateMouse
              x: heroRow.x + heroDate.x
              y: heroRow.y + heroDate.y
              width: heroDate.width
              height: heroDate.height
              enabled: root.selectedDayKey !== root.todayKey || !root.viewingCurrentMonth
              hoverEnabled: enabled
              cursorShape: Qt.PointingHandCursor
              onClicked: root.goToToday()

              PanelToolTip {
                visible: heroDateMouse.containsMouse
                text: "Back to today"
                fontFamily: root.contentFontFamily
              }
            }
          }

          // ---- Year progress, doubling as the rule under the hero:
          //      a plain hairline said nothing, and whole days done
          //      over days in the year says the same thing louder.
          Item {
            width: parent.width
            height: yearBlock.y + yearBlock.height

            Item {
              id: yearBlock
              y: Style.space(6)
              anchors.horizontalCenter: parent.horizontalCenter
              width: gridColumn.width
              height: yearRail.height

              TapHandler {
                enabled: !root.editingLife
                onDoubleTapped: root.startEditingLife()
              }

              Row {
                visible: root.editingLife
                anchors.horizontalCenter: parent.horizontalCenter
                anchors.verticalCenter: parent.verticalCenter
                spacing: Style.space(10)

                Text {
                  anchors.verticalCenter: parent.verticalCenter
                  text: "BORN"
                  color: Qt.darker(root.contentForeground, 1.5)
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.bodySmall
                  font.letterSpacing: 1
                }

                TextField {
                  id: bornField
                  width: Style.space(70)
                  anchors.verticalCenter: parent.verticalCenter
                  placeholderText: "year"
                  foreground: root.contentForeground
                  font.family: root.contentFontFamily
                  inputMethodHints: Qt.ImhDigitsOnly

                  Keys.onPressed: function(event) { root.handleLifeKey(event, expectancyField) }
                }

                Text {
                  anchors.verticalCenter: parent.verticalCenter
                  anchors.verticalCenterOffset: 0
                  leftPadding: Style.space(6)
                  text: "LIVE TO"
                  color: Qt.darker(root.contentForeground, 1.5)
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.bodySmall
                  font.letterSpacing: 1
                }

                TextField {
                  id: expectancyField
                  width: Style.space(60)
                  anchors.verticalCenter: parent.verticalCenter
                  placeholderText: "90"
                  foreground: root.contentForeground
                  font.family: root.contentFontFamily
                  inputMethodHints: Qt.ImhDigitsOnly

                  Keys.onPressed: function(event) { root.handleLifeKey(event, bornField) }
                }
              }

              ProgressRail {
                id: yearRail
                visible: !root.editingLife
                label: root.today.getFullYear()
                fraction: root.yearDone
                percent: root.yearDonePercent
              }
            }
          }

          // ---- Memento mori. Only here once someone has gone looking and
          //      given an age; the same rail as the year above it, measured
          //      against a nominal lifetime.
          Item {
            visible: root.birthYear > 0
            width: parent.width
            height: visible ? lifeBlock.height : 0

            Item {
              id: lifeBlock
              anchors.horizontalCenter: parent.horizontalCenter
              width: gridColumn.width
              height: lifeRail.height

              ProgressRail {
                id: lifeRail
                label: "LIFE"
                fraction: root.lifeDone
                percent: root.lifeDonePercent
              }

              TapHandler {
                onDoubleTapped: root.clearLife()
              }

              MouseArea {
                id: lifeMouse
                anchors.fill: parent
                hoverEnabled: true
                acceptedButtons: Qt.NoButton

                PanelToolTip {
                  visible: lifeMouse.containsMouse
                  text: "Memento Mori"
                  fontFamily: root.contentFontFamily
                }
              }
            }
          }

          // ---- Month grid: week numbers down a gutter on the left, then
          //      the seven day columns. Always six rows, so the popup is
          //      exactly as tall in February as it is in August.
          Item {
            width: parent.width
            height: gridColumn.y + gridColumn.height

            WheelHandler {
              onWheel: function(event) {
                // Horizontal wheels and touchpad side-scrolls report y === 0;
                // without this they would every one read as "next month".
                if (event.angleDelta.y === 0) return
                root.moveMonth(event.angleDelta.y > 0 ? -1 : 1)
              }
            }

            Column {
              id: gridColumn
              // The meter above is a solid rule; the grid needs room to
              // read as its own block rather than hanging off it.
              y: Style.space(18)
              anchors.horizontalCenter: parent.horizontalCenter
              spacing: Style.space(3)

              Row {
                id: headerRow
                spacing: root.cellSpacing

                // The week-number heading doubles as the week-start toggle.
                // It is the one control in the panel whose meaning is not
                // self-evident, so it carries a tooltip naming the day the
                // click will switch to.
                Rectangle {
                  width: root.weekColumnWidth
                  height: Style.space(16)
                  radius: Style.cornerRadius
                  color: weekStartMouse.containsMouse
                    ? Style.hoverFillFor(root.contentForeground, Color.accent)
                    : "transparent"

                  Text {
                    anchors.centerIn: parent
                    text: "W"
                    color: weekStartMouse.containsMouse
                      ? Style.hoverStateColor(root.contentForeground, Color.accent)
                      : Qt.darker(root.contentForeground, 1.9)
                    font.family: root.contentFontFamily
                    font.pixelSize: Style.font.caption
                    font.letterSpacing: 1
                    font.bold: true
                  }

                  MouseArea {
                    id: weekStartMouse
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: root.toggleWeekStart()
                  }

                  PanelToolTip {
                    visible: weekStartMouse.containsMouse
                    text: "Start weeks on " + root.nextWeekStartLabel
                    fontFamily: root.contentFontFamily
                  }
                }

                Item {
                  width: root.gutterWidth
                  height: Style.space(16)
                }

                Repeater {
                  model: root.weekdays

                  Text {
                    required property var modelData
                    width: root.cellWidth
                    height: Style.space(16)
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    text: root.weekdayLabel(modelData)
                    color: Qt.darker(root.contentForeground, 1.5)
                    font.family: root.contentFontFamily
                    font.pixelSize: Style.font.caption
                    font.letterSpacing: 1
                    font.bold: true
                  }
                }
              }

              Repeater {
                model: root.weeks

                Row {
                  required property var modelData
                  spacing: root.cellSpacing

                  Text {
                    width: root.weekColumnWidth
                    height: root.cellHeight
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    text: modelData.week
                    color: Qt.darker(root.contentForeground, 1.9)
                    font.family: root.contentFontFamily
                    font.pixelSize: Style.font.caption
                  }

                  Item {
                    width: root.gutterWidth
                    height: root.cellHeight
                  }

                  Repeater {
                    model: modelData.days

                    Item {
                      id: dayCell
                      required property var modelData
                      width: root.cellWidth
                      height: root.cellHeight

                      readonly property bool selected: modelData.key === root.selectedDayKey

                      // The server's own calendar colours, so work and personal
                      // days read apart here the way they do in the calendar app. The
                      // dots carry that and nothing else: a cell washed in the
                      // calendar's colour would make "has something on it" a
                      // different shade every day, and that band is what the eye
                      // reads first when scanning the month.
                      readonly property var dayColors: modelData.colors || []

                      // Events are filled dots, tasks hollow rings, in that
                      // order and capped at three between them: a ring is
                      // something still owed, a dot something already in the
                      // diary, and the difference has to survive being 5px
                      // wide. A day with an event but no calendar colour
                      // still gets its one dot, the way it always did.
                      readonly property bool marked: modelData.hasEvent || modelData.hasTask
                      readonly property var dayMarks: {
                        var marks = []
                        var events = modelData.colors || []
                        if (modelData.hasEvent && !events.length) marks.push({ tint: Color.accent, filled: true })
                        for (var i = 0; i < events.length && marks.length < 3; i++)
                          marks.push({ tint: events[i], filled: true })
                        var tasks = modelData.taskColors || []
                        if (modelData.hasTask && !tasks.length) marks.push({ tint: Color.accent, filled: false })
                        for (var j = 0; j < tasks.length && marks.length < 3; j++)
                          marks.push({ tint: tasks[j], filled: false })
                        return marks
                      }

                      Rectangle {
                        anchors.fill: parent
                        radius: Style.cornerRadius
                        // Today outranks the selection, which starts on today
                        // anyway: the one cell you look for first gets the
                        // theme's strongest fill and a solid ring at double
                        // width, so it carries further than the translucent
                        // wash every day holding an event already has.
                        color: modelData.today
                          ? Util.alpha(root.todayAccent, Style.selectionFillAlpha)
                          : parent.selected
                            ? Style.hoverFillFor(root.contentForeground, Color.accent)
                            : dayCell.marked
                              ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.15)
                              : "transparent"
                        border.width: modelData.today
                          ? Style.spacing.hairline * 2
                          : (parent.selected || dayCell.marked) ? Style.spacing.hairline : 0
                        border.color: modelData.today
                          ? Util.alpha(root.todayAccent, Style.selectedBorderAlpha)
                          : parent.selected
                            ? Style.normalBorderFor(root.contentForeground, Color.accent)
                            : dayCell.marked
                              ? Qt.rgba(Color.accent.r, Color.accent.g, Color.accent.b, 0.5)
                              : Style.normalBorderFor(root.contentForeground, Color.accent)

                        Text {
                          anchors.centerIn: parent
                          anchors.verticalCenterOffset: dayCell.marked ? -Style.space(2) : 0
                          text: modelData.day
                          // Today is never dimmed, weekend or not: it sits on
                          // the brightest fill in the grid, and the muted
                          // weekend grey would leave the one date you came for
                          // the hardest to read.
                          color: modelData.today
                            ? root.contentForeground
                            : modelData.inMonth
                              ? (modelData.weekend ? Qt.darker(root.contentForeground, 1.45) : root.contentForeground)
                              : Qt.darker(root.contentForeground, 2.2)
                          font.family: root.contentFontFamily
                          font.pixelSize: Style.font.body
                          font.bold: modelData.today
                        }

                        // One mark per calendar with something on that day.
                        // Three is what fits under a day number, so a fourth
                        // calendar goes without one rather than crowding the
                        // cell — the day list below still names everything.
                        Row {
                          anchors.horizontalCenter: parent.horizontalCenter
                          anchors.bottom: parent.bottom
                          anchors.bottomMargin: Style.space(3)
                          spacing: Style.space(2)
                          opacity: dayCell.marked ? 1 : 0

                          Repeater {
                            model: dayCell.dayMarks

                            Rectangle {
                              required property var modelData
                              anchors.verticalCenter: parent.verticalCenter
                              // The ring is a size up on the dot: at dot size
                              // its hole closes and it reads as a dimmer dot
                              // rather than as a different thing.
                              width: modelData.filled ? Style.space(4) : Style.space(6)
                              height: width
                              radius: width / 2
                              color: modelData.filled ? modelData.tint : "transparent"
                              border.width: modelData.filled ? 0 : Math.max(1, Style.spacing.hairline)
                              border.color: modelData.tint
                            }
                          }
                        }
                      }

                      MouseArea {
                        anchors.fill: parent
                        cursorShape: Qt.PointingHandCursor
                        acceptedButtons: Qt.LeftButton | Qt.RightButton
                        onClicked: function(mouse) {
                          root.selectDay(modelData.key)
                          if (mouse.button === Qt.RightButton) root.openEntry(modelData.key)
                        }
                      }
                    }
                  }
                }
              }
            }

            // Hairline down the week-number gutter, drawn only beside the
            // day rows so it does not cut through the header band.
            Rectangle {
              x: gridColumn.x + root.weekColumnWidth + root.cellSpacing + Math.round((root.gutterWidth - width) / 2)
              y: gridColumn.y + headerRow.height + gridColumn.spacing
              width: Style.spacing.hairline
              height: gridColumn.height - headerRow.height - gridColumn.spacing
              color: root.contentForeground
              opacity: 0.1
            }
          }

          // ---- Month stepping, spanning the grid it drives. The chevrons
          //      sit on the grid's outer bounds, the same edges the year
          //      rail above uses, so the row reads as the panel's other
          //      full-width rail instead of a cluster floating in space.
          //      The label is centered and fixed-width, so it holds still
          //      from "MAY" to "SEPTEMBER".
          Item {
            width: parent.width
            height: monthNav.height

            Item {
              id: monthNav
              anchors.horizontalCenter: parent.horizontalCenter
              width: gridColumn.width
              height: monthLabel.implicitHeight + Style.space(10)

              Text {
                id: monthLabel
                anchors.horizontalCenter: parent.horizontalCenter
                anchors.verticalCenter: parent.verticalCenter
                // Fixed width so the chevrons hold still between a
                // "MAY 2026" and a "SEPTEMBER 2026".
                width: Style.space(130)
                horizontalAlignment: Text.AlignHCenter
                text: Qt.formatDate(root.viewDate, "MMMM yyyy").toUpperCase()
                color: Qt.darker(root.contentForeground, 1.4)
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body
                font.letterSpacing: 1
              }

              PanelActionButton {
                // Pulled out by the button's own padding so the glyph, not
                // its hit box, lines up with the "2026" on the year rail.
                anchors.left: parent.left
                anchors.leftMargin: -Style.space(8)
                anchors.verticalCenter: parent.verticalCenter
                iconText: "󰅁"
                tooltipText: "Previous month"
                foreground: root.contentForeground
                fontFamily: root.contentFontFamily
                onClicked: root.moveMonth(-1)
              }

              PanelActionButton {
                anchors.right: parent.right
                anchors.rightMargin: -Style.space(8)
                anchors.verticalCenter: parent.verticalCenter
                iconText: "󰅂"
                tooltipText: "Next month"
                foreground: root.contentForeground
                fontFamily: root.contentFontFamily
                onClicked: root.moveMonth(1)
              }
            }
          }

          Column {
            visible: root.selectedEvents.length > 0 || root.selectedTasks.length > 0
              || root.bucketWeekSelected || !root.eventDoc
            width: gridColumn.width
            anchors.horizontalCenter: parent.horizontalCenter
            spacing: Style.space(6)

            Rectangle {
              width: parent.width
              height: Style.spacing.hairline
              color: root.contentForeground
              opacity: 0.12
            }

            Repeater {
              model: root.selectedEvents

              Item {
                id: eventRow
                required property var modelData
                readonly property string meetingUrl: Model.meetingUrlFor(modelData)
                readonly property bool joinable: meetingUrl !== ""
                readonly property int joinReserve: joinable ? joinButton.width + Style.space(8) : 0

                width: gridColumn.width
                height: Math.max(
                  eventTitle.implicitHeight + eventMeta.implicitHeight + Style.space(8),
                  joinable ? joinButton.height + Style.space(4) : 0
                )

                // Opens the event for editing. Stops short of the join
                // button's own area so the two clicks stay separate — one
                // joins the meeting, the other edits the appointment.
                MouseArea {
                  anchors.fill: parent
                  anchors.rightMargin: eventRow.joinReserve
                  hoverEnabled: true
                  cursorShape: Qt.PointingHandCursor
                  onClicked: root.openEventEdit(eventRow.modelData)
                }

                Rectangle {
                  width: Style.space(3)
                  height: parent.height - Style.space(4)
                  radius: 2
                  // The event's own calendar colour, matching the day dots above.
                  color: Model.eventColor(eventRow.modelData) || Color.accent
                  anchors.verticalCenter: parent.verticalCenter
                }

                Text {
                  id: eventTitle
                  x: Style.space(10)
                  width: parent.width - Style.space(12) - eventRow.joinReserve
                  text: modelData.title || ""
                  color: root.contentForeground
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.body
                  elide: Text.ElideRight
                }

                Text {
                  id: eventMeta
                  x: Style.space(10)
                  y: eventTitle.implicitHeight + Style.space(2)
                  width: parent.width - Style.space(12) - eventRow.joinReserve
                  text: {
                    var time = Model.eventDisplayTime(modelData)
                    var loc = modelData.location || ""
                    if (eventRow.meetingUrl && loc.indexOf(eventRow.meetingUrl) !== -1) loc = ""
                    if (time && loc) return time + "  ·  " + loc
                    return time || loc
                  }
                  color: Qt.darker(root.contentForeground, 1.5)
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.bodySmall
                  elide: Text.ElideRight
                }

                Rectangle {
                  id: joinButton
                  visible: eventRow.joinable
                  anchors.right: parent.right
                  anchors.verticalCenter: parent.verticalCenter
                  width: joinLabel.implicitWidth + Style.space(14)
                  height: joinLabel.implicitHeight + Style.space(7)
                  radius: height / 2
                  color: joinMouse.containsMouse
                    ? Style.selectedStateColor(root.contentForeground, Color.accent)
                    : "transparent"
                  border.width: Style.spacing.hairline
                  border.color: joinMouse.containsMouse
                    ? "transparent"
                    : Qt.darker(root.contentForeground, 2.0)

                  Text {
                    id: joinLabel
                    anchors.centerIn: parent
                    // Name the service: "Join Zoom" tells you what is about
                    // to open, and it is the same wording the bar uses for
                    // the next meeting.
                    text: Model.joinButtonLabel(eventRow.meetingUrl)
                    color: joinMouse.containsMouse
                      ? Color.background
                      : Qt.darker(root.contentForeground, 1.2)
                    font.family: root.contentFontFamily
                    font.pixelSize: Style.font.bodySmall
                  }

                  MouseArea {
                    id: joinMouse
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: root.openMeeting(eventRow.modelData)
                  }
                }
              }
            }

            // Tasks due on the selected day, under that day's events. Same
            // list, same rail: a Tuesday with a meeting and a deadline is one
            // day's worth of work, not two panes of it.
            Flow {
              visible: root.selectedTasks.length > 0
              width: parent.width
              spacing: Style.space(12)

              Repeater {
                model: root.selectedTasks

                TaskRow {
                  required property var modelData
                  task: modelData
                }
              }
            }

            // ---- "Sometime this week": everything with no day of its own —
            //      never given one, or given one that has gone by. Always the
            //      current week's, whatever month the grid is showing, because
            //      this is the list you opened the panel to see. It does not
            //      move when you click around the calendar.
            Column {
              visible: root.bucketWeekSelected
              width: parent.width
              spacing: Style.space(6)

              Item {
                width: 1
                height: Style.space(4)
              }

              Item {
                width: parent.width
                height: Math.max(bucketLabel.implicitHeight, quickTodoHolder.height)

                Text {
                  id: bucketLabel
                  anchors.left: parent.left
                  anchors.verticalCenter: parent.verticalCenter
                  text: "SOMETIME THIS WEEK"
                  color: Qt.darker(root.contentForeground, 1.7)
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.caption
                  font.letterSpacing: 1
                  font.bold: true
                }

                // The count is the point of the header: four things you have
                // not done reads differently from one.
                Text {
                  id: bucketCount
                  anchors.left: bucketLabel.right
                  anchors.leftMargin: Style.space(8)
                  anchors.verticalCenter: parent.verticalCenter
                  visible: root.bucketTasks.length > 0
                  text: root.bucketTasks.length
                  color: Qt.darker(root.contentForeground, 2.1)
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.caption
                }

                // The + does not open a form somewhere else; it turns into
                // the field, on the spot, where the todo is going to appear.
                Item {
                  id: quickTodoHolder
                  anchors.left: bucketCount.visible ? bucketCount.right : bucketLabel.right
                  anchors.leftMargin: Style.space(8)
                  anchors.right: parent.right
                  anchors.verticalCenter: parent.verticalCenter
                  height: root.quickTodoOpen ? quickTodoField.implicitHeight : quickTodoAdd.height

                  PanelActionButton {
                    id: quickTodoAdd
                    anchors.left: parent.left
                    anchors.verticalCenter: parent.verticalCenter
                    visible: !root.quickTodoOpen
                    iconText: "+"
                    tooltipText: "Add a todo"
                    foreground: root.contentForeground
                    fontFamily: root.contentFontFamily
                    onClicked: root.openQuickTodo()
                  }

                  TextField {
                    id: quickTodoField
                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    visible: root.quickTodoOpen
                    // Bare text on the panel, not a boxed input: the field
                    // stands where the chips do, and a filled control there
                    // would read as a different kind of thing from the list
                    // it is adding to. Padding goes with the background —
                    // the component derives it from the border it is no
                    // longer drawing.
                    background: null
                    verticalPadding: 0
                    leftPadding: 0
                    rightPadding: 0
                    topPadding: 0
                    bottomPadding: 0
                    foreground: root.contentForeground
                    font.family: root.contentFontFamily
                    font.pixelSize: Style.font.bodySmall
                    // The field owns its text, the way the other one-shot
                    // inputs in this panel do: a two-way binding here would
                    // be broken by the first keystroke anyway.
                    onAccepted: root.commitQuickTodo()
                    Keys.onPressed: function(event) {
                      // Esc belongs to the field while it is up, or it would
                      // close the whole panel out from under the typing.
                      if (event.key === Qt.Key_Escape) {
                        root.closeQuickTodo()
                        event.accepted = true
                      }
                    }
                  }
                }
              }

              Flow {
                width: parent.width
                spacing: Style.space(12)

                Repeater {
                  model: root.bucketTasks

                  TaskRow {
                    required property var modelData
                    task: modelData
                    bucket: true
                  }
                }
              }
            }

            Text {
              visible: !root.eventDoc
              text: "Waiting for the calendar to sync"
              color: Qt.darker(root.contentForeground, 1.8)
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.bodySmall
              font.italic: true
            }
          }
        }
      }
      }

      Item {
        id: entryPane
        width: parent.width
        height: parent.height
        x: root.entryOpen ? 0 : width
        Behavior on x { NumberAnimation { duration: 220; easing.type: Easing.OutCubic } }

        Keys.onPressed: function(event) { root.handleEntryKey(event) }

        // Keys are owned by the entry pane only when no dropdown popup is
        // open — inside those, j/k/Enter belong to the option list.
        readonly property bool keysIdle:
          root.entryOpen && !calDropdown.popupOpen && !alertDropdown.popupOpen && !repeatDropdown.popupOpen && !priorityDropdown.popupOpen

        Shortcut {
          enabled: entryPane.keysIdle
          sequence: "Escape"
          onActivated: root.deleteConfirming ? (root.deleteConfirming = false) : root.closeEntry()
        }

        Shortcut {
          enabled: entryPane.keysIdle
          sequence: "Ctrl+E"
          onActivated: root.setEntryKind("event")
        }

        Shortcut {
          enabled: entryPane.keysIdle
          sequence: "Ctrl+T"
          onActivated: root.setEntryKind("task")
        }

        Flickable {
          id: entryScroll
          anchors.fill: parent
          contentWidth: entryColumn.width
          contentHeight: entryColumn.implicitHeight
          clip: true
          boundsBehavior: Flickable.StopAtBounds
          interactive: contentHeight > height

          Column {
            id: entryColumn
            width: Math.max(entryScroll.width, gridColumn.width)
            spacing: Style.space(10)

            // Every row lines up on this. The entry pane runs wider than the
            // month grid it slid in over — the pills only sit on one line if
            // they get the pane's own width — so it takes the width back off
            // the pane rather than off the grid.
            readonly property real rowWidth: width - 2 * Style.space(40)

            // ---- Header: the way back on the left, what is being made in
            //      the middle, where it lands on the right.
            Item {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              height: Math.max(backButton.height, kindTabs.height, calDropdown.height)

              PanelActionButton {
                id: backButton
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                iconText: "󰅁"
                tooltipText: "Back to calendar"
                foreground: root.contentForeground
                fontFamily: root.contentFontFamily
                onClicked: root.closeEntry()
              }

              Row {
                id: kindTabs
                visible: !root.editingEventId
                anchors.left: backButton.right
                anchors.leftMargin: Style.space(10)
                anchors.verticalCenter: parent.verticalCenter
                spacing: Style.space(14)

                KindTab {
                  label: "EVENT"
                  active: root.entryKind === "event"
                  onActivated: root.setEntryKind("event")
                }

                KindTab {
                  label: "TODO"
                  active: root.entryKind === "task"
                  onActivated: root.setEntryKind("task")
                }
              }

              // Nothing to switch to while editing one specific event: the
              // tabs above give way to saying plainly what the pane is for.
              Text {
                visible: root.editingEventId !== null
                anchors.left: backButton.right
                anchors.leftMargin: Style.space(10)
                anchors.verticalCenter: parent.verticalCenter
                text: "EDIT EVENT"
                color: root.contentForeground
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.bodySmall
                font.bold: true
              }

              // The calendar the entry lands in, in the colour the server
              // gives it — the same dot the month grid paints on a day.
              Rectangle {
                visible: calDropdown.visible
                anchors.right: calDropdown.left
                anchors.rightMargin: Style.space(6)
                anchors.verticalCenter: parent.verticalCenter
                width: Style.space(8)
                height: width
                radius: Style.cornerRadius > 0 ? width / 2 : 0
                color: root.roleCalendarColor
              }

              Dropdown {
                id: calDropdown
                visible: root.calendarChoices.length > 0
                // An edit cannot move an event to another calendar — the
                // daemon has no verb for that — so the chooser goes
                // read-only rather than offering a change that would be
                // silently dropped.
                enabled: !root.editingEventId
                opacity: enabled ? 1 : 0.55
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                width: Math.min(Style.space(150), parent.width * 0.36)
                showLabel: false
                label: root.entryKind === "task" ? "List" : "Calendar"
                value: root.formCalendar
                options: root.calendarChoices
                foreground: root.contentForeground
                fontFamily: root.contentFontFamily
                onChanged: function(v) { root.formCalendar = v }
              }

              // No roster yet (add-on missing or old): a typed name still
              // gets matched at create time, so entry never blocks on it.
              TextField {
                visible: root.calendarChoices.length === 0
                enabled: !root.editingEventId
                opacity: enabled ? 1 : 0.55
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                width: Math.min(Style.space(150), parent.width * 0.36)
                placeholderText: root.entryKind === "task" ? "List" : "Calendar"
                foreground: root.contentForeground
                font.family: root.contentFontFamily
                text: root.formCalendar
                onTextEdited: root.formCalendar = text
                Keys.onPressed: function(event) { root.handleEntryKey(event) }
              }
            }

            // ---- The phrase, painted as it is understood.
            Item {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              height: nlField.height

              PhraseField {
                id: nlField
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                extraRightPadding: Style.space(18)
                phrase: root.nlText
                html: root.phraseMarkup
                placeholderText: root.entryKind === "task"
                  ? "buy groceries tomorrow !  /  einkaufen bis freitag"
                  : "lunch with Ana tomorrow 12:30 till 14:00  /  mittag 12:30 bis 14 Uhr"
                onEdited: function(text) {
                  root.nlText = text
                  root.applyPhrase()
                }
                onSubmitted: root.commitEntry()
                onCancelled: root.closeEntry()
              }

              // Hover examples: the whole grammar in one glance, since the
              // field itself only shows a single hint.
              Text {
                id: nlHelpIcon
                anchors.right: parent.right
                anchors.rightMargin: Style.space(6)
                anchors.top: parent.top
                anchors.topMargin: Style.space(8)
                text: "󰋗"
                color: Qt.darker(root.contentForeground, 1.4)
                font.family: root.contentFontFamily
                font.pixelSize: Style.font.body

                MouseArea {
                  id: nlHelpHover
                  anchors.fill: parent
                  anchors.margins: -Style.space(2)
                  hoverEnabled: true
                  cursorShape: Qt.PointingHandCursor
                }

                PanelToolTip {
                  visible: nlHelpHover.containsMouse
                  text: root.entryKind === "task"
                    ? "ToDos · Aufgaben\n" +
                      "buy groceries tomorrow !            (low)\n" +
                      "finish report friday /Work !!       (medium)\n" +
                      "call mom sunday 6pm !!!             (high)\n" +
                      "water plants nächsten Montag -r1w  (weekly)\n" +
                      "einkaufen bis freitag               (due date)\n" +
                      "in Aufgaben  or  /Name              (list)"
                    : "Events · Termine\n" +
                      "lunch with Ana tomorrow 12:30 till 14:00\n" +
                      "mittag mit Ana morgen 12 bis 13 Uhr /Arbeit\n" +
                      "standup next monday 9am -a15m   (alert)\n" +
                      "flight 15.3. 7:00-9:30          (date + range)\n" +
                      "party today 21:00 till 2:00     (past midnight)\n" +
                      "retreat till 25.8.              (multi-day)\n" +
                      "workshop for 90m  /  -120       (duration)\n" +
                      "call https://zoom.us/j/9421     (meeting link)\n" +
                      "in Work  or  /Name              (calendar)"
                  fontFamily: root.contentFontFamily
                }
              }
            }

            Text {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              visible: root.nlCalendarName !== "" && !root.calendarValidForKind(root.nlCalendarName)
              text: root.nlCalendarName
                + (root.entryKind === "task" ? " takes events only" : " takes todos only")
                + (root.formCalendar !== "" ? " — using " + root.formCalendar : "")
              color: Qt.darker(root.contentForeground, 1.3)
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.bodySmall
              wrapMode: Text.WordWrap
              horizontalAlignment: Text.AlignHCenter
            }

            // ---- What the phrase came to. Click any value to correct just
            //      that value; the phrase above is left as it was typed.
            SlotEdit {
              slotName: "title"
              hero: true
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              display: root.formTitle !== "" ? root.formTitle : "Untitled"
              dim: root.formTitle === ""
              editValue: root.formTitle
              onCommitText: function(text) { root.formTitle = text }
            }

            // ---- When: start on the left, end on the right, the way the
            //      phrase reads. All-day collapses both times.
            BorderSurface {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              radius: Style.cornerRadius
              color: Style.normalFillFor(root.contentForeground, Color.accent)
              borderSpec: Border.controlSpec("normal", root.contentForeground, Color.accent)
              implicitHeight: whenColumn.implicitHeight + 2 * Style.space(12)

              Column {
                id: whenColumn
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.verticalCenter: parent.verticalCenter
                anchors.margins: Style.space(12)
                spacing: Style.space(10)

                Item {
                  width: parent.width
                  height: Math.max(startSide.height, endSide.height)

                  Column {
                    id: startSide
                    anchors.left: parent.left
                    anchors.top: parent.top
                    width: (parent.width - Style.space(28)) / 2
                    spacing: Style.space(2)

                    SlotEdit {
                      slotName: "startDate"
                      width: parent.width
                      display: root.dayLabel(root.formDate)
                      editValue: root.formDate
                      tint: root.roleDateColor
                      onCommitText: function(text) { root.commitStartDate(text) }
                    }

                    SlotEdit {
                      slotName: "startTime"
                      width: parent.width
                      strong: true
                      display: root.formAllDay
                        ? "all day"
                        : (root.formStart !== "" ? root.formStart : "set time")
                      dim: root.formAllDay || root.formStart === ""
                      editValue: root.formStart
                      tint: root.roleTimeColor
                      onCommitText: function(text) { root.commitStartTime(text) }
                    }
                  }

                  Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    anchors.top: parent.top
                    anchors.topMargin: Style.space(6)
                    visible: endSide.visible
                    text: "→"
                    color: Qt.darker(root.contentForeground, 1.5)
                    font.family: root.contentFontFamily
                    font.pixelSize: Style.font.heading
                  }

                  Column {
                    id: endSide
                    anchors.right: parent.right
                    anchors.top: parent.top
                    width: (parent.width - Style.space(28)) / 2
                    spacing: Style.space(2)
                    visible: root.entryKind === "event"

                    SlotEdit {
                      slotName: "endDate"
                      width: parent.width
                      align: Text.AlignRight
                      display: root.dayLabel(root.entryEndDateKey())
                      editValue: root.entryEndDateKey()
                      tint: root.roleDateColor
                      onCommitText: function(text) { root.commitEndDate(text) }
                    }

                    SlotEdit {
                      slotName: "endTime"
                      width: parent.width
                      align: Text.AlignRight
                      strong: true
                      display: root.formAllDay
                        ? "all day"
                        : (root.formEnd !== "" ? root.formEnd : "set time")
                      dim: root.formAllDay || root.formEnd === ""
                      editValue: root.formEnd
                      tint: root.roleTimeColor
                      onCommitText: function(text) { root.commitEndTime(text) }
                    }
                  }
                }

                // A span that crosses midnight says so; nothing else needs
                // saying under the card now that all-day is just "no time".
                Text {
                  anchors.horizontalCenter: parent.horizontalCenter
                  visible: root.formEndNextDay && !root.formAllDay && !root.formEndDate
                  text: "ends next day"
                  color: Qt.darker(root.contentForeground, 1.5)
                  font.family: root.contentFontFamily
                  font.pixelSize: Style.font.bodySmall
                }
              }
            }

            // ---- Pills: the parts a phrase can carry but usually doesn't.
            //      A pill lights up when the phrase filled its part in, and
            //      opens the same row a click on it opens.
            Flow {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              spacing: Style.space(6)

              Pill {
                rowName: "link"
                iconText: "󰌹"
                // The pill names the service once a link is in, so the row
                // does not have to be open to see what it is.
                text: root.formLink !== "" ? Model.linkProviderLabel(root.formLink) : "Link"
                focusTarget: linkField
              }

              Pill {
                rowName: "location"
                iconText: "󰍎"
                text: "Location"
                focusTarget: locationField
              }

              Pill {
                rowName: "notes"
                iconText: "󰦨"
                text: "Notes"
                focusTarget: notesField
              }

              Pill {
                rowName: "alert"
                iconText: "󰂚"
                text: "Notify me"
                visible: root.entryKind === "event"
              }

              Pill {
                rowName: "repeat"
                iconText: "󰑐"
                text: "Repeat"
              }

              Pill {
                rowName: "priority"
                iconText: "󰈻"
                text: "Priority"
                visible: root.entryKind === "task"
              }
            }

            // ---- The opened parts themselves.
            Column {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              spacing: Style.space(6)

              EntryRow {
                rowName: "link"
                icon: "󰌹"

                TextField {
                  id: linkField
                  width: parent.width
                  placeholderText: "https://zoom.us/j/…"
                  foreground: root.contentForeground
                  font.family: root.contentFontFamily
                  text: root.formLink
                  onTextEdited: root.formLink = text
                  Keys.onPressed: function(event) { root.handleEntryKey(event) }
                }
              }

              EntryRow {
                rowName: "location"
                icon: "󰍎"

                Column {
                  width: parent.width
                  spacing: Style.space(4)

                  TextField {
                    id: locationField
                    width: parent.width
                    placeholderText: "Location"
                    foreground: root.contentForeground
                    font.family: root.contentFontFamily
                    text: root.formLocation
                    onTextEdited: {
                      root.formLocation = text
                      root.requestAddressSuggestions(text)
                    }
                    onActiveFocusChanged: if (!activeFocus) root.closeAddressSuggestions()
                    Keys.onPressed: function(event) { root.handleLocationKey(event) }
                  }

                  // The addresses OSM knows for what was typed. The line on
                  // top is the whole address the field would receive; the
                  // one under it is the rest of it, so a row can be told
                  // from its neighbour two streets over at a glance.
                  BorderSurface {
                    id: suggestList
                    visible: root.locationSuggestOpen
                    width: parent.width
                    height: visible ? suggestColumn.height + 2 * Style.space(4) : 0
                    color: Color.popups.background
                    borderSpec: Border.localOrSurfaceSpec("popups", "border", Color.popups.border, Color.popups.border, Style.normalBorderWidth)
                    radius: Style.cornerRadius

                    Column {
                      id: suggestColumn
                      x: Style.space(4)
                      y: Style.space(4)
                      width: parent.width - 2 * Style.space(4)

                      Repeater {
                        model: root.locationSuggestions

                        Rectangle {
                          required property int index
                          required property var modelData

                          width: suggestColumn.width
                          height: suggestText.height + 2 * Style.space(5)
                          radius: Style.cornerRadius
                          color: index === root.locationSuggestIndex
                            ? Style.controlFill(true, false, root.contentForeground, Color.accent)
                            : "transparent"

                          Column {
                            id: suggestText
                            anchors.left: parent.left
                            anchors.right: parent.right
                            anchors.leftMargin: Style.space(8)
                            anchors.rightMargin: Style.space(8)
                            anchors.verticalCenter: parent.verticalCenter
                            spacing: 0

                            Text {
                              width: parent.width
                              text: modelData.primary
                              color: root.contentForeground
                              font.family: root.contentFontFamily
                              font.pixelSize: Style.font.body
                              elide: Text.ElideRight
                            }

                            Text {
                              width: parent.width
                              visible: modelData.secondary !== ""
                              text: modelData.secondary
                              color: Qt.darker(root.contentForeground, 1.5)
                              font.family: root.contentFontFamily
                              font.pixelSize: Style.font.caption
                              elide: Text.ElideRight
                            }
                          }

                          MouseArea {
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onEntered: root.locationSuggestIndex = index
                            onClicked: {
                              root.takeAddressSuggestion(index)
                              locationField.forceActiveFocus()
                            }
                          }
                        }
                      }
                    }
                  }
                }
              }

              EntryRow {
                rowName: "notes"
                icon: "󰦨"

                NotesField {
                  id: notesField
                  width: parent.width
                  placeholderText: "Notes"
                  value: root.formDescription
                  onEdited: function(text) { root.formDescription = text }
                  onCancelled: root.closeEntry()
                  onSubmitted: root.commitEntry()
                }
              }

              EntryRow {
                rowName: "alert"
                icon: "󰂚"
                visible: root.entryKind === "event" && root.rowOpen("alert")

                Dropdown {
                  id: alertDropdown
                  width: parent.width
                  showLabel: false
                  label: "Alert"
                  value: String(root.formAlertMinutes || 0)
                  options: Model.alertOptions(root.formAlertMinutes)
                  foreground: root.contentForeground
                  fontFamily: root.contentFontFamily
                  onChanged: function(v) { root.formAlertMinutes = parseInt(v, 10) || 0 }
                }
              }

              EntryRow {
                rowName: "repeat"
                icon: "󰑐"

                Dropdown {
                  id: repeatDropdown
                  width: parent.width
                  showLabel: false
                  label: "Repeat"
                  value: root.formRecurrenceValue
                  options: Model.repeatOptions(root.formRecurrenceValue)
                  foreground: root.contentForeground
                  fontFamily: root.contentFontFamily
                  onChanged: function(v) { root.setRecurrenceFrom(v) }
                }
              }

              EntryRow {
                rowName: "priority"
                icon: "󰈻"
                visible: root.entryKind === "task" && root.rowOpen("priority")

                Dropdown {
                  id: priorityDropdown
                  width: parent.width
                  showLabel: false
                  label: "Priority"
                  value: root.formPriority
                  options: [
                    { value: "", label: "No priority" },
                    { value: "low", label: "Low !" },
                    { value: "medium", label: "Medium !!" },
                    { value: "high", label: "High !!!" }
                  ]
                  foreground: root.contentForeground
                  fontFamily: root.contentFontFamily
                  onChanged: function(v) { root.formPriority = v }
                }
              }
            }

            Text {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              visible: root.nlText !== "" && !root.entryCheck.ok
              text: root.entryCheck.error || ""
              color: Qt.darker(root.contentForeground, 1.3)
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.bodySmall
              wrapMode: Text.WordWrap
              horizontalAlignment: Text.AlignHCenter
            }

            Button {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              text: root.editingEventId ? "Save changes"
                : (root.entryKind === "task" ? "Add this todo" : "Add this event")
              selected: true
              foreground: root.contentForeground
              fontFamily: root.contentFontFamily
              onClicked: root.commitEntry()
              Keys.onPressed: function(event) { root.handleEntryKey(event) }
            }

            // Deleting is a click that arms it and a second click that
            // does it — no native dialog, same as everything else here,
            // but a mistaken single click must not be the whole story.
            Button {
              visible: root.editingEventId !== null
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              text: root.deleteConfirming ? "Click again to delete" : "Delete this event"
              bordered: true
              accent: "#d9534f"
              foreground: root.deleteConfirming ? "#d9534f" : root.contentForeground
              fontFamily: root.contentFontFamily
              onClicked: root.deleteEditingEvent()
            }

            Text {
              width: entryColumn.rowWidth
              anchors.horizontalCenter: parent.horizontalCenter
              visible: root.entryStatus !== ""
              text: root.entryStatus
              color: root.contentForeground
              font.family: root.contentFontFamily
              font.pixelSize: Style.font.bodySmall
              wrapMode: Text.WordWrap
              horizontalAlignment: Text.AlignHCenter
            }
          }
        }
      }
      }
    }
  }
}
