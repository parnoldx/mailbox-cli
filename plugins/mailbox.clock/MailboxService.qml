import QtQuick
import Quickshell
import Quickshell.Io

// The mailbox daemon, as a QML object.
//
// Every command the panel needs -- the calendar roster, the agenda, the
// open todos, and the writes that follow from clicking around in them --
// is one request on a unix socket the daemon already holds. The same
// command surface the CLI speaks, minus one process per call: a Mirror
// read answers in microseconds and never waits on a network.
//
// The daemon also pushes. Nothing here polls for a calendar that changed
// on the phone; the socket says calendar.changed, and the panel re-asks.
Item {
  id: root

  // Where the daemon listens. Empty means there is no user session to have a
  // socket in, which is a state and not an error: the panel simply has no
  // data rather than a spinner that never stops.
  readonly property string socketPath: {
    var explicit = Quickshell.env("MAILBOX_SOCKET")
    if (explicit) return String(explicit)
    var runtime = Quickshell.env("XDG_RUNTIME_DIR")
    return runtime ? String(runtime) + "/mailbox.sock" : ""
  }

  property bool available: false
  property string lastError: ""

  // Raised for every push the daemon sends: mail.changed, calendar.changed,
  // event.changed, todo.changed, habit.changed, contact.changed.
  signal pushed(var message)
  // Raised when the socket comes up, including after it came back. A listener
  // treats this as "ask again": whatever it was showing is from before the
  // gap, and the daemon does not replay what it pushed while nobody was
  // connected.
  signal connected()

  property int _seq: 0
  property var _pending: ({})

  // call runs one command. cmd is the path as an array -- ["event", "add"] --
  // and args are the flags without their dashes, with "positional" for what
  // comes after the command. args is omitted for the calls that take none:
  // the daemon reads args as an object, and an array -- even an empty one --
  // is a malformed request. done(error, data) is called exactly once.
  //
  // Sending while disconnected fails the call rather than queueing it: a
  // widget's request is about now, and a queue would answer a question nobody
  // is still asking.
  function call(cmd, args, done) {
    var sock = holder.item
    if (!sock || !sock.connected) {
      if (done) done("the mailbox daemon is not reachable", null)
      return
    }
    var id = "q" + (++_seq)
    var request = { id: id, cmd: cmd }
    if (args && !Array.isArray(args) && Object.keys(args).length)
      request.args = args
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
      root.lastError = "the daemon sent something that is not JSON"
      return
    }
    if (message.id === undefined) {
      root.pushed(message)
      return
    }
    if (message.ok === false) {
      _settle(message.id, String(message.error || "the command failed"), null)
      return
    }
    _settle(message.id, "", message.data)
  }

  // Everything queued on a socket that went away is answered, not forgotten.
  // A caller that never hears back leaves a spinner up for ever.
  function _abandon(reason) {
    var pending = _pending
    _pending = ({})
    for (var id in pending) {
      if (pending[id]) pending[id](reason, null)
    }
  }

  // The socket lives in a Loader so that reconnecting can throw it away and
  // build a new one. Quickshell's Socket does not come back: once a connect
  // has failed, setting connected again is a no-op -- no attempt, no error,
  // nothing -- so a service that only flipped the property would go quiet for
  // the rest of the session the first time the daemon restarted.
  Component {
    id: socketComponent
    Socket {
      path: root.socketPath
      // Under systemd the socket is there whether the daemon is running or
      // not, and connecting is what starts it. Nothing here spawns one: a
      // shell reload would then take the daemon down with it, and every IMAP
      // IDLE connection with that.
      connected: true
      parser: SplitParser {
        onRead: function (data) { root._line(data) }
      }
      onConnectionStateChanged: {
        if (connected) root.lastError = ""
        root.available = connected
        if (connected) {
          retry.running = false
          // Deferred, because a local socket to a path that exists connects
          // during the Loader's own construction -- before the Loader has
          // published the item that call() writes to. A listener that asked
          // straight away would be told there is no daemon.
          Qt.callLater(root.connected)
          return
        }
        root._abandon("the mailbox daemon went away")
        root._reconnectLater()
      }
      onError: function (error) {
        root.lastError = "cannot reach the mailbox daemon"
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

  // Backing off matters because the failure this retries is usually a daemon
  // that is not installed at all, and a shell that reconnects to nothing every
  // second for a whole session is a wakeup every second for a whole session.
  //
  // The timer repeats rather than being restarted from the failure handler.
  // A failed connect raises error synchronously inside the assignment below,
  // so restarting from there would be restarting a one-shot timer from inside
  // its own trigger -- which it then stops again on the way out, and the
  // retrying quietly ends after one attempt.
  property int _backoff: 1000
  Timer {
    id: retry
    interval: root._backoff
    repeat: true
    running: false
    onTriggered: {
      root._backoff = Math.min(root._backoff * 2, 60000)
      // Destroy and rebuild: see the Component above for why flipping the
      // property is not enough.
      holder.active = false
      holder.active = true
    }
  }
  onConnected: root._backoff = 1000
}
