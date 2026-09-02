import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Pdf
import QtQuick.Dialogs
import "MailFormat.js" as Fmt

// In-app file preview in the spirit of macOS Quick Look / omarchy-quick-look:
// the attachment opens here, rendered, not handed off. Esc or Q dismisses; the
// toolbar still offers "Open externally" and "Save as…".
Item {
    id: root
    property bool opened: false
    property var att: ({})
    property string path: ""       // local cache path once fetched
    property string status: ""

    readonly property string mime: att.mime_type || ""
    readonly property string name: att.filename || "attachment"
    readonly property bool isPdf: mime.indexOf("pdf") >= 0 || name.toLowerCase().endsWith(".pdf")
    readonly property bool isImage: mime.indexOf("image/") === 0
    readonly property bool isText: mime.indexOf("text/") === 0
        || mime.indexOf("json") >= 0 || mime.indexOf("xml") >= 0
        || mime.indexOf("javascript") >= 0 || mime.indexOf("csv") >= 0

    function fileUrl(p) { return Fmt.fileUrl(p) }
    function localPath(url) { return Fmt.localPath(url) }

    // Called from an attachment chip. Fetch to the cache dir, then show.
    function openFor(attachment) {
        root.att = attachment
        root.path = ""
        root.status = "fetching…"
        root.opened = true
        Qt.callLater(function () { keyGrab.forceActiveFocus() })
        Mailbox.call(["attachment", "save"],
                     { positional: attachment.id, output: Mailbox.cacheDir(), force: true },
                     function (r) {
            if (!root.opened) return
            if (r.ok && r.data && r.data.path) {
                root.status = ""
                root.path = r.data.path
                textLoad.restart()
            } else {
                root.status = Fmt.errText(r, "Could not fetch this file")
            }
        })
    }
    function close() { opened = false; path = ""; textArea.text = "" }

    visible: opened || scrim.opacity > 0.01

    Rectangle {
        id: scrim
        anchors.fill: parent
        color: Qt.rgba(0, 0, 0, 0.78)
        opacity: root.opened ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        TapHandler { onTapped: root.close() }
    }

    Item {
        id: keyGrab
        anchors.fill: parent
        focus: root.opened
        Keys.onEscapePressed: root.close()
        Keys.onPressed: function (e) {
            if (e.key === Qt.Key_Q) { root.close(); e.accepted = true }
        }
    }

    Rectangle {
        id: panel
        anchors.centerIn: parent
        width: Math.round(parent.width * 0.86)
        height: Math.round(parent.height * 0.88)
        radius: Theme.radius
        color: Theme.railBg
        border.width: 1
        border.color: Theme.hairline
        opacity: root.opened ? 1 : 0
        scale: root.opened ? 1 : 0.98
        Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        Behavior on scale { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        clip: true

        Item {
            id: bar
            anchors { top: parent.top; left: parent.left; right: parent.right }
            height: 48

            Text {
                anchors { left: parent.left; leftMargin: 18; verticalCenter: parent.verticalCenter }
                width: parent.width - actions.width - 40
                text: root.name
                elide: Text.ElideMiddle
                font.family: Theme.fontFamily
                font.pixelSize: 12
                font.weight: Font.DemiBold
                color: Theme.textPrimary
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }

            Row {
                id: actions
                anchors { right: parent.right; rightMargin: 12; verticalCenter: parent.verticalCenter }
                spacing: 4

                Repeater {
                    model: [
                        { g: "", act: "ext" },
                        { g: "", act: "save" },
                        { g: "", act: "close" }
                    ]
                    Rectangle {
                        width: 30; height: 30; radius: 7
                        enabled: modelData.act === "close" || root.path !== ""
                        opacity: enabled ? 1 : 0.4
                        color: mh.hovered && enabled ? Theme.cardHover : "transparent"
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                        Text {
                            anchors.centerIn: parent
                            text: modelData.g
                            font.family: Theme.fontFamily
                            font.pixelSize: 12
                            color: mh.hovered && parent.enabled ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        HoverHandler { id: mh }
                        TapHandler {
                            onTapped: {
                                if (!parent.enabled) return
                                if (modelData.act === "ext") Qt.openUrlExternally(root.fileUrl(root.path))
                                else if (modelData.act === "save") saveDialog.open()
                                else root.close()
                            }
                        }
                    }
                }
            }

            Rectangle {
                anchors { bottom: parent.bottom; left: parent.left; right: parent.right }
                height: 1; color: Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
        }

        Item {
            anchors { top: bar.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }

            Text {
                anchors.centerIn: parent
                visible: root.status !== ""
                text: root.status
                font.family: Theme.fontFamily
                font.pixelSize: 12
                color: Theme.textDim
            }

            Loader {
                anchors.fill: parent
                active: root.opened && root.isPdf && root.path !== ""
                sourceComponent: Component {
                    PdfMultiPageView {
                        anchors.fill: parent
                        document: PdfDocument { source: root.fileUrl(root.path) }
                    }
                }
            }

            Flickable {
                id: imgFlick
                anchors.fill: parent
                visible: root.isImage && root.path !== ""
                contentWidth: Math.max(width, img.width)
                contentHeight: Math.max(height, img.height)
                clip: true
                Image {
                    id: img
                    anchors.centerIn: parent
                    source: (root.isImage && root.path !== "") ? root.fileUrl(root.path) : ""
                    fillMode: Image.PreserveAspectFit
                    width: Math.min(implicitWidth, imgFlick.width - 40)
                    height: implicitHeight * (width / Math.max(1, implicitWidth))
                    smooth: true
                    asynchronous: true
                }
            }

            Flickable {
                anchors.fill: parent
                visible: root.isText && root.path !== ""
                clip: true
                contentWidth: width
                contentHeight: textArea.implicitHeight + 32
                ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }
                TextArea {
                    id: textArea
                    width: parent.width
                    padding: 20
                    readOnly: true
                    selectByMouse: true
                    wrapMode: TextArea.NoWrap
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textPrimary
                    background: null
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }

            Column {
                anchors.centerIn: parent
                spacing: 12
                visible: root.opened && root.path !== "" && !root.isPdf && !root.isImage && !root.isText
                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: ""
                    font.family: Theme.fontFamily
                    font.pixelSize: 40
                    color: Theme.hairline
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                Text {
                    anchors.horizontalCenter: parent.horizontalCenter
                    text: "No inline preview for " + (root.mime || "this file")
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
        }
    }

    FileDialog {
        id: saveDialog
        fileMode: FileDialog.SaveFile
        currentFolder: root.fileUrl(Mailbox.downloadDir())
        selectedFile: root.fileUrl(Mailbox.downloadDir() + "/" + root.name)
        onAccepted: {
            Mailbox.call(["attachment", "save"],
                         { positional: root.att.id, output: root.localPath(selectedFile), force: true },
                         function (r) {
                if (!(r.ok && r.data)) root.status = Fmt.errText(r, "Save failed")
            })
        }
    }

    Timer {
        id: textLoad
        interval: 30
        onTriggered: {
            if (!root.isText || root.path === "") return
            var xhr = new XMLHttpRequest()
            xhr.onreadystatechange = function () {
                if (xhr.readyState === XMLHttpRequest.DONE)
                    textArea.text = xhr.responseText || "(empty)"
            }
            xhr.open("GET", root.fileUrl(root.path))
            xhr.send()
        }
    }
}
