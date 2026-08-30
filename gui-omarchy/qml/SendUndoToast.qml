import QtQuick
import QtQuick.Controls.Basic

// The five-second grace period. Send closes the composer at once; this banner
// counts down with a draining bar and an Undo. If the body mentioned an
// attachment and none was attached it says so. On timeout it makes the real
// `send`/`reply` call and reports the result briefly.
Item {
    id: root
    property var pending: null          // { cmd, args, label, warnAttachment, form }
    property string phase: ""           // "" | counting | result
    property string resultText: ""
    property bool resultOk: true

    visible: phase !== ""

    function start(p) {
        root.pending = p
        root.resultText = ""
        root.phase = "counting"
        bar.width = barTrack.width
        barAnim.restart()
        countdown.restart()
    }
    function fire() {
        if (!root.pending) return
        var p = root.pending
        root.phase = "result"
        Mailbox.call(p.cmd, p.args, function (r) {
            root.resultOk = !!r.ok
            root.resultText = r.ok ? "Sent" : ((r.error && r.error.length) ? r.error : "Send failed")
            hideTimer.restart()
        })
    }
    function undo() {
        countdown.stop(); barAnim.stop()
        var form = root.pending ? root.pending.form : null
        root.phase = ""; root.pending = null
        if (form) win.reopenComposer(form)
    }

    Timer { id: countdown; interval: 5000; onTriggered: root.fire() }
    Timer { id: hideTimer; interval: 2200; onTriggered: { root.phase = ""; root.pending = null } }

    Rectangle {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        width: Math.min(520, parent.width - 64)
        height: col.implicitHeight + 22
        y: root.phase !== "" ? parent.height - height - 28 : parent.height + 8
        Behavior on y { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: warn ? Theme.yellow : Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        readonly property bool warn: root.phase === "counting" && root.pending && root.pending.warnAttachment

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
                        text: root.phase === "result"
                              ? root.resultText
                              : (root.pending ? root.pending.label : "")
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        font.weight: Font.DemiBold
                        color: root.phase === "result"
                               ? (root.resultOk ? Theme.green : Theme.red)
                               : Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        visible: card.warn
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
                    visible: root.phase === "counting"
                    kind: "ghost"
                    glyph: "\uf0e2"
                    text: card.warn ? "Undo to attach" : "Undo"
                    onClicked: root.undo()
                }
            }

            // Draining progress bar.
            Rectangle {
                id: barTrack
                visible: root.phase === "counting"
                width: parent.width - 24
                height: 3
                radius: 2
                color: Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Rectangle {
                    id: bar
                    height: parent.height
                    radius: 2
                    color: card.warn ? Theme.yellow : Theme.accent
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                NumberAnimation { id: barAnim; target: bar; property: "width"; to: 0; duration: 5000; easing.type: Easing.Linear }
            }
        }
    }
}
