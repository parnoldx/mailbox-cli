import QtQuick
import QtQuick.Dialogs

// An attachment card. The body opens an in-app Quick Look preview; the tray icon
// pops a "Save as…" dialog and lets the daemon write the file where you choose.
Rectangle {
    id: root
    property var att: ({})
    property string status: ""

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
    function localPath(url) { return decodeURIComponent(String(url).replace(/^file:\/\//, "")) }
    function fileUrl(p) {
        var parts = String(p || "").split("/")
        for (var i = 0; i < parts.length; i++) parts[i] = encodeURIComponent(parts[i])
        return "file://" + parts.join("/")
    }

    Timer { id: doneTimer; interval: 2600; onTriggered: root.status = "" }

    MouseArea {
        id: bodyArea
        anchors { left: parent.left; top: parent.top; bottom: parent.bottom; right: saveBtn.left }
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: win.openQuickLook(root.att)
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
                text: (root.status && root.status !== "busy") ? root.status : (root.att.filename || "attachment")
                elide: Text.ElideMiddle
                width: Math.min(210, implicitWidth)
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
            Text {
                text: root.status === "busy" ? "saving…" : root.humanSize(root.att.size)
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
            onClicked: saveDialog.open()
        }
    }

    FileDialog {
        id: saveDialog
        fileMode: FileDialog.SaveFile
        currentFolder: root.fileUrl(Mailbox.downloadDir())
        selectedFile: root.fileUrl(Mailbox.downloadDir() + "/" + (root.att.filename || "attachment"))
        onAccepted: {
            root.status = "busy"
            Mailbox.call(["attachment", "save"],
                         { positional: root.att.id, output: root.localPath(selectedFile), force: true },
                         function (r) {
                root.status = (r.ok && r.data) ? "Saved" : ((r.error && r.error.length) ? r.error : "Save failed")
                doneTimer.restart()
            })
        }
    }
}
