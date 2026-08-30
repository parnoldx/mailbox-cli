import QtQuick

// A clickable attachment card. The body opens the file with the desktop default
// app; the tray icon saves a copy to ~/Downloads.
Rectangle {
    id: root
    property var att: ({})
    property string status: ""     // "", "busy", or a short done message

    implicitWidth: Math.min(360, row.implicitWidth + 68)
    implicitHeight: 52
    radius: Theme.radiusSmall
    color: bodyArea.containsMouse ? Theme.cardHover : Theme.cardBg
    border.width: 1
    border.color: Theme.hairline
    Behavior on color { ColorAnimation { duration: Theme.anim } }
    Behavior on border.color { ColorAnimation { duration: Theme.anim } }

    function glyphFor(mime) {
        mime = mime || ""
        if (mime.indexOf("pdf") >= 0) return "\uf1c1"
        if (mime.indexOf("image") === 0) return "\uf1c5"
        if (mime.indexOf("zip") >= 0 || mime.indexOf("compress") >= 0 || mime.indexOf("tar") >= 0) return "\uf1c6"
        if (mime.indexOf("audio") === 0) return "\uf1c7"
        if (mime.indexOf("video") === 0) return "\uf1c8"
        if (mime.indexOf("text") === 0 || mime.indexOf("word") >= 0 || mime.indexOf("document") >= 0) return "\uf15c"
        return "\uf016"
    }
    function humanSize(n) {
        n = n || 0
        if (n < 1024) return n + " B"
        if (n < 1048576) return (n / 1024).toFixed(0) + " KB"
        return (n / 1048576).toFixed(1) + " MB"
    }

    function fetch(dir, thenOpen) {
        if (root.status === "busy") return
        root.status = "busy"
        Mailbox.call(["attachment", "save"], { positional: root.att.id, output: dir, force: true }, function (r) {
            if (r.ok && r.data && r.data.path) {
                if (thenOpen) Qt.openUrlExternally("file://" + r.data.path)
                root.status = thenOpen ? "" : "Saved to Downloads"
            } else {
                root.status = (r.error && r.error.length) ? r.error : "Could not fetch"
            }
            doneTimer.restart()
        })
    }
    Timer { id: doneTimer; interval: 2600; onTriggered: if (root.status !== "busy") root.status = "" }

    MouseArea {
        id: bodyArea
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom; right: saveBtn.left }
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: root.fetch(Mailbox.cacheDir(), true)
    }

    Row {
        id: row
        anchors.verticalCenter: parent.verticalCenter
        anchors.left: parent.left
        anchors.leftMargin: 12
        spacing: 10

        Text {
            anchors.verticalCenter: parent.verticalCenter
            text: root.glyphFor(root.att.mime_type)
            font.family: Theme.fontFamily
            font.pixelSize: 16
            color: Theme.accent
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Column {
            anchors.verticalCenter: parent.verticalCenter
            spacing: 2
            Text {
                text: (root.status && root.status !== "busy") ? root.status
                    : (root.att.filename || "attachment")
                elide: Text.ElideMiddle
                width: Math.min(210, implicitWidth)
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Text {
                text: root.status === "busy" ? "fetching…" : root.humanSize(root.att.size)
                font.family: Theme.fontFamily
                font.pixelSize: 10
                color: Theme.textDim
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
        }
    }

    Rectangle {
        id: saveBtn
        width: 30; height: 30; radius: 7
        anchors { right: parent.right; rightMargin: 8; verticalCenter: parent.verticalCenter }
        color: saveArea.containsMouse ? Theme.selection : "transparent"
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Text {
            anchors.centerIn: parent
            text: "\uf0c7"
            font.family: Theme.fontFamily
            font.pixelSize: 12
            color: saveArea.containsMouse ? Theme.accent : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        MouseArea {
            id: saveArea
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: root.fetch(Mailbox.downloadDir(), false)
        }
    }
}
