import QtQuick
import Quickshell
import Quickshell.Io
import "Model.js" as Model

// MailboxService.qml — Direct unix socket client to the mailbox daemon.
//
// Maintains the live mirror connection over $XDG_RUNTIME_DIR/mailbox.sock,
// handling real-time push events (`mail.changed`) and exposing clean QML
// models for unread mail, accounts, and the Screener.
Item {
  id: root

  property var settings: ({})
  readonly property bool notify: settings && settings.notify !== undefined ? settings.notify : false
  readonly property bool sound: settings && settings.sound !== undefined ? settings.sound : true

  readonly property string socketPath: {
    var explicit = Quickshell.env("MAILBOX_SOCKET")
    if (explicit) return String(explicit)
    var runtime = Quickshell.env("XDG_RUNTIME_DIR")
    return runtime ? String(runtime) + "/mailbox.sock" : ""
  }

  property bool available: false
  property bool refreshing: false
  property string lastError: ""
  property string actionStatus: ""

  property var accounts: []
  property int accountCount: accounts.length
  property var messages: []
  property var screenerList: []

  property int unreadCount: 0
  property int screenerCount: 0

  signal connected()

  property int _seq: 0
  property var _pending: ({})
  property var _knownUnreadIds: ({})
  property bool _initialized: false

  function call(cmd, args, done) {
    var sock = holder.item
    if (!sock || !sock.connected) {
      if (done) done("the mailbox daemon is not reachable", null)
      return
    }
    var id = "m" + (++_seq)
    var request = { id: id, cmd: cmd }
    if (args && !Array.isArray(args) && Object.keys(args).length) {
      request.args = args
    }
    var pending = _pending
    pending[id] = done || null
    _pending = pending
    sock.write(JSON.stringify(request) + "\n")
    sock.flush()
  }

  function _settle(id, error, data) {
    var done = _pending[id]
    if (done === undefined) return
    var pending = _pending
    delete pending[id]
    _pending = pending
    if (done) done(error, data)
  }

  function _line(raw) {
    var text = String(raw || "").replace(/^\s+|\s+$/g, "")
    if (text === "") return
    var message
    try {
      message = JSON.parse(text)
    } catch (error) {
      root.lastError = "the daemon sent non-JSON output"
      return
    }
    if (message.id === undefined) {
      if (message.event === "mail.changed") refreshDebounce.restart()
      return
    }
    if (message.ok === false) {
      _settle(message.id, String(message.error || "command failed"), null)
      return
    }
    _settle(message.id, "", message.data)
  }

  function _abandon(reason) {
    var pending = _pending
    _pending = ({})
    for (var id in pending) {
      if (pending[id]) pending[id](reason, null)
    }
  }

  // Refresh all state from the daemon: box counts, screener senders, and recent inbox messages
  function refresh(callback) {
    if (!available) {
      if (callback) callback("daemon not available")
      return
    }
    root.refreshing = true

    // 1. Box list
    root.call(["box", "list"], {}, function(errBoxes, boxes) {
      if (errBoxes) {
        root.lastError = String(errBoxes)
        root.refreshing = false
        if (callback) callback(errBoxes)
        return
      }
      var boxList = Array.isArray(boxes) ? boxes : []

      // Extract distinct accounts
      var accMap = {}
      var totalUnread = 0
      for (var i = 0; i < boxList.length; i++) {
        var b = boxList[i]
        if (b && b.account) accMap[b.account] = true
        // Count unseen in inbox and watched boxes
        if (b && (b.box === "inbox" || b.box === "INBOX" || b.folder === "INBOX" || b.watched)) {
          totalUnread += (b.unseen || 0)
        }
      }
      var accList = []
      for (var a in accMap) accList.push(a)
      root.accounts = accList
      root.unreadCount = totalUnread

      // 2. Screener list
      root.call(["screener"], { limit: 50 }, function(errScreener, screenerData) {
        if (!errScreener && Array.isArray(screenerData)) {
          root.screenerList = screenerData
          var sCount = 0
          for (var j = 0; j < screenerData.length; j++) {
            sCount += (screenerData[j].count || 1)
          }
          root.screenerCount = sCount
        } else {
          root.screenerList = []
          root.screenerCount = 0
        }

        // 3. Inbox messages
        root.call(["box", "view"], { positional: "inbox", limit: 50 }, function(errMsgs, msgs) {
          root.refreshing = false
          if (!errMsgs && Array.isArray(msgs)) {
            var formatted = []
            var newUnreadList = []
            var known = root._knownUnreadIds
            var nextKnown = {}

            for (var k = 0; k < msgs.length; k++) {
              var m = msgs[k]
              var item = {
                id: m.id,
                uid: m.uid,
                date: m.date,
                from: m.from,
                name: Model.cleanSenderName(m.from),
                address: Model.cleanAddress(m.from),
                subject: m.subject || "(No Subject)",
                seen: m.seen === true,
                body: m.body_state || "",
                account: "primary",
                initials: Model.extractInitials(m.from),
                colorIndex: Model.avatarColorIndex(m.from, 8)
              }
              formatted.push(item)
              if (!item.seen) {
                nextKnown[item.id] = true
                if (!known[item.id]) {
                  newUnreadList.push(item)
                }
              }
            }
            var isInitial = !root._initialized
            root.messages = formatted
            root._knownUnreadIds = nextKnown
            root._initialized = true

            // If new unread mail arrived while running (suppressed on initial startup sync)
            if (!isInitial && newUnreadList.length > 0) {
              if (root.sound) root._playNewMailSound()
              if (root.notify) root._notifyNewMail(newUnreadList)
            }
          }
          if (callback) callback(null)
        })
      })
    })
  }

  function _playNewMailSound() {
    soundProcess.running = false
    soundProcess.command = [
      "bash", "-c",
      "canberra-gtk-play -i message-new-email -d 'New Email' 2>/dev/null || pw-play /usr/share/sounds/freedesktop/stereo/message.oga 2>/dev/null || paplay /usr/share/sounds/freedesktop/stereo/message.oga 2>/dev/null || true"
    ]
    soundProcess.running = true
  }

  Process { id: soundProcess }

  function _notifyNewMail(newMsgs) {
    if (!newMsgs || newMsgs.length === 0) return
    var title = newMsgs.length === 1
      ? "New email from " + newMsgs[0].name
      : newMsgs.length + " new emails"
    var body = newMsgs.length === 1
      ? newMsgs[0].subject
      : newMsgs.slice(0, 3).map(function(m) { return m.name + ": " + m.subject }).join("\n")

    toastProcess.command = [
      "notify-send",
      "-a", "Mailbox",
      "-i", "mail-unread",
      "-h", "string:x-canonical-private-synchronous:mailbox",
      title,
      body
    ]
    toastProcess.running = true
  }

  Process { id: toastProcess }

  Timer {
    id: refreshDebounce
    interval: 200
    repeat: false
    onTriggered: root.refresh()
  }

  // Every panel action is the same shape: fire one daemon command, report the
  // outcome in actionStatus (which actionTimer clears), resync on success.
  function _action(cmd, args, okStatus, done) {
    root.call(cmd, args, function(err, result) {
      if (err) {
        root.actionStatus = "Error: " + err
      } else {
        root.actionStatus = okStatus
        root.refresh()
      }
      if (err || okStatus !== "") actionTimer.restart()
      if (done) done(err, result)
    })
  }

  function routeSender(target, destination, done) {
    root.actionStatus = "Filing sender to " + destination + "…"
    _action(["route"], { positional: target, to: destination }, "Routed to " + destination, done)
  }

  function setSeen(id, seen, done) {
    _action([seen === false ? "unseen" : "seen"], { positional: id }, "", done)
  }

  function setAside(id, done) {
    _action(["aside"], { positional: id }, "Set aside", done)
  }

  function trashMessage(id, done) {
    _action(["trash"], { positional: id }, "Moved to Trash", done)
  }

  Timer {
    id: actionTimer
    interval: 3000
    repeat: false
    onTriggered: root.actionStatus = ""
  }

  Component {
    id: socketComponent
    Socket {
      path: root.socketPath
      connected: true
      parser: SplitParser {
        onRead: function(data) { root._line(data) }
      }
      onConnectionStateChanged: {
        if (connected) root.lastError = ""
        root.available = connected
        if (connected) {
          retry.running = false
          Qt.callLater(function() {
            root.connected()
            root.refresh()
          })
          return
        }
        root._abandon("mailbox daemon disconnected")
        root._reconnectLater()
      }
      onError: function(error) {
        root.lastError = "cannot reach mailbox daemon"
        root._reconnectLater()
      }
    }
  }

  Loader {
    id: holder
    active: root.socketPath !== ""
    sourceComponent: socketComponent
  }

  function _reconnectLater() {
    if (root.socketPath === "") return
    retry.running = true
  }

  property int _backoff: 1000
  Timer {
    id: retry
    interval: root._backoff
    repeat: true
    running: false
    onTriggered: {
      root._backoff = Math.min(root._backoff * 2, 60000)
      holder.active = false
      holder.active = true
    }
  }
  onConnected: root._backoff = 1000
}
