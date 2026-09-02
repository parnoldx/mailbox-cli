import QtQuick
import QtQuick.Controls.Basic
import "MailFormat.js" as Fmt

// The five-second grace period — but only when there is something worth
// pausing for: the body mentions an attachment and none was actually
// attached (warnAttachment). Every other send has nothing to catch, so it
// skips the toast entirely and goes out at once, with just a "Sent" flash.
// Send always closes the composer immediately. When the countdown runs out
// (or is never shown) this makes the real `send`/`reply` call; the result is
// reported through win.flash(), not here.
Item {
    id: root
    property var pending: null          // { cmd, args, label, warnAttachment, form }
    property string phase: ""           // "" | counting

    visible: phase !== ""

    function start(p) {
        root.pending = p
        if (!p.warnAttachment) { root.fire(); return }
        root.phase = "counting"
        bar.width = barTrack.width
        barAnim.restart()
        countdown.restart()
    }
    function fire() {
        if (!root.pending) return
        var p = root.pending
        countdown.stop(); barAnim.stop()
        root.phase = ""; root.pending = null
        Mailbox.call(p.cmd, p.args, function (r) {
            win.flash(r.ok ? "Sent" : Fmt.errText(r, "Send failed"))
        })
    }
    function undo() {
        countdown.stop(); barAnim.stop()
        var form = root.pending ? root.pending.form : null
        root.phase = ""; root.pending = null
        if (form) win.reopenComposer(form)
    }

    Timer { id: countdown; interval: 5000; onTriggered: root.fire() }

    Rectangle {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        width: Math.min(520, parent.width - 64)
        height: col.implicitHeight + 22
        // Drops down from the top, same edge as the "Draft saved" flash — the
        // bottom is the Reply Later / Set Aside stacks' turf on the Inbox.
        y: root.phase !== "" ? 28 : -height - 8
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.yellow
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        Column {
            id: col
            width: parent.width
            padding: 12
            spacing: 9

            Row {
                width: parent.width - 24
                spacing: 12

                Column {
                    width: parent.width - undoBtn.width - 12
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 3
                    Text {
                        width: parent.width
                        elide: Text.ElideRight
                        text: root.pending ? root.pending.label : ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        font.weight: Font.DemiBold
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        width: parent.width
                        wrapMode: Text.Wrap
                        text: "You mentioned an attachment, but none is attached."
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.yellow
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }

                AppButton {
                    id: undoBtn
                    anchors.verticalCenter: parent.verticalCenter
                    kind: "ghost"
                    glyph: ""
                    text: "Undo to attach"
                    onClicked: root.undo()
                }
            }

            // Draining progress bar.
            Rectangle {
                id: barTrack
                width: parent.width - 24
                height: 3
                radius: 2
                color: Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Rectangle {
                    id: bar
                    height: parent.height
                    radius: 2
                    color: Theme.yellow
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                NumberAnimation { id: barAnim; target: bar; property: "width"; to: 0; duration: 5000; easing.type: Easing.Linear }
            }
        }
    }
}
