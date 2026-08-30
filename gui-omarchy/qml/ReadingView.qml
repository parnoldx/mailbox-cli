import QtQuick
import QtQuick.Controls.Basic

Item {
    id: root

    function fromName(s) {
        if (!s) return ""
        var m = s.match(/^\s*"?(.*?)"?\s*<([^>]+)>\s*$/)
        return m ? (m[1].trim() || m[2]) : s
    }
    function fromAddr(s) {
        if (!s) return ""
        var m = s.match(/<([^>]+)>/)
        return m ? m[1] : s
    }
    function niceDate(s) {
        var d = new Date(s)
        return isNaN(d.getTime()) ? (s || "") : Qt.formatDateTime(d, "dddd d MMMM yyyy  ·  HH:mm")
    }

    // Back affordance — the only chrome HEY's reading view keeps.
    Row {
        id: backBar
        anchors { left: parent.left; top: parent.top; leftMargin: 28; topMargin: 22 }
        spacing: 10
        z: 2

        Rectangle {
            width: 30; height: 30; radius: 15
            color: backHover.hovered ? Theme.cardHover : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }
            Text {
                anchors.centerIn: parent
                text: ""
                font.family: Theme.fontFamily
                font.pixelSize: 14
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            HoverHandler { id: backHover }
            TapHandler { onTapped: win.back() }
        }
        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: win.buckets[win.bucketIndex].label
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Kbd { anchors.verticalCenter: parent.verticalCenter; text: "Esc" }
    }

    Text {
        anchors.centerIn: parent
        visible: win.openLoading
        text: "opening…"
        font.family: Theme.fontFamily
        font.pixelSize: 12
        color: Theme.textDim
    }

    Flickable {
        anchors { fill: parent; topMargin: 64 }
        visible: win.openMsg && !win.openLoading
        contentWidth: width
        contentHeight: body.y + body.implicitHeight + 80
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        Column {
            id: head
            x: Math.max(40, (parent.width - 780) / 2)
            width: Math.min(780, parent.width - 80)
            spacing: 22
            topPadding: 8

            // No "Subject:" label — the subject is the headline.
            Text {
                width: parent.width
                text: win.openMsg ? (win.openMsg.subject || "(no subject)") : ""
                wrapMode: Text.WordWrap
                font.family: Theme.fontFamily
                font.pixelSize: 27
                font.weight: Font.Bold
                lineHeight: 1.18
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }

            // No "From:" / "Date:" labels — just the person and the time.
            Row {
                spacing: 13
                Avatar {
                    width: 42; height: 42; radius: 21
                    name: win.openMsg ? root.fromName(win.openMsg.from) : ""
                    seed: win.openMsg ? root.fromAddr(win.openMsg.from) : ""
                }
                Column {
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 3
                    Text {
                        text: win.openMsg ? root.fromName(win.openMsg.from) : ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        font.weight: Font.DemiBold
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: win.openMsg
                              ? root.fromAddr(win.openMsg.from) + "   ·   " + root.niceDate(win.openMsg.date)
                              : ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
            }

            Rectangle {
                width: parent.width; height: 1
                color: Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
        }

        Text {
            id: body
            x: head.x
            y: head.y + head.implicitHeight + 26
            width: head.width
            text: win.openMsg ? (win.openMsg.body || "(this message has no text part yet)") : ""
            wrapMode: Text.WordWrap
            textFormat: Text.PlainText
            font.family: Theme.fontFamily
            font.pixelSize: 13
            lineHeight: 1.6
            color: Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }
}
