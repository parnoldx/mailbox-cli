import QtQuick
import QtQuick.Controls.Basic
import QtWebEngine

Item {
    id: root

    readonly property bool htmlMode: !!(win.openMsg && win.openMsg.body_html && win.openMsg.body_html.length > 0)

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
    function bodyText() {
        if (!win.openMsg) return ""
        var b = win.openMsg.body || ""
        if (win.openMsg.body_format === "markdown")
            b = b.replace(/!\[[^\]]*\]\([^)]*\)/g, "")
        return b.trim() || "(this message has no text part yet)"
    }

    // Wrap the message HTML in a stylesheet built from the live Omarchy palette,
    // so a newsletter lands on the same background as the rest of the app and
    // retints when the theme changes.
    function themedHtml() {
        if (!root.htmlMode) return ""
        // Render the mail as its author designed it, on a clean sheet — the way
        // every desktop mail client does. We only stop it scrolling sideways:
        // clamp every box to the viewport and let fixed-width tables collapse.
        var css =
          "html{overflow-x:hidden !important;max-width:100% !important;}" +
          "*{max-width:100% !important;box-sizing:border-box !important;}" +
          "body{margin:0;padding:22px;background:#ffffff;color:#1b1b1b;" +
          "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;" +
          "font-size:14px;line-height:1.6;overflow-x:hidden !important;" +
          "word-wrap:break-word;overflow-wrap:break-word;}" +
          "table{table-layout:fixed !important;width:100% !important;max-width:100% !important;}" +
          "td,th{overflow-wrap:break-word;word-break:break-word;}" +
          "pre{white-space:pre-wrap !important;word-break:break-word;}" +
          "img{max-width:100% !important;height:auto !important;}" +
          "a{word-break:break-word;}" +
          "::-webkit-scrollbar{width:9px;}" +
          "::-webkit-scrollbar:horizontal{height:0 !important;display:none !important;}" +
          "::-webkit-scrollbar-thumb{background:#c9c9c9;border-radius:5px;}"
        return "<!DOCTYPE html><html><head><meta charset='utf-8'>" +
               "<meta name='viewport' content='width=device-width, initial-scale=1'>" +
               "<meta name='color-scheme' content='light'>" +
               "<style>" + css + "</style></head><body>" + win.openMsg.body_html + "</body></html>"
    }

    function reloadHtml() {
        if (root.htmlMode) web.loadHtml(root.themedHtml(), "about:blank")
    }

    onHtmlModeChanged: reloadHtml()
    Connections { target: win; function onOpenMsgChanged() { root.reloadHtml() } }
    Connections { target: Theme; function onChanged() { root.reloadHtml() } }

    // ---- Header (does not scroll) -------------------------------------------
    Column {
        id: header
        anchors { top: parent.top; left: parent.left; right: parent.right }
        anchors.leftMargin: Math.max(28, (parent.width - 820) / 2)
        anchors.rightMargin: anchors.leftMargin
        anchors.topMargin: 22
        spacing: 16
        visible: win.openMsg && !win.openLoading

        Row {
            spacing: 10
            Rectangle {
                width: 28; height: 28; radius: 14
                color: backHover.hovered ? Theme.cardHover : "transparent"
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Text {
                    anchors.centerIn: parent
                    text: ""
                    font.family: Theme.fontFamily
                    font.pixelSize: 13
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

            // PixelBlock badge
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                visible: PixelBlock.blocked > 0
                width: badge.implicitWidth + 18
                height: 20
                radius: 10
                color: Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: badge
                    anchors.centerIn: parent
                    spacing: 5
                    Text {
                        text: ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.green
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: PixelBlock.blocked + (PixelBlock.blocked === 1 ? " tracker blocked" : " trackers blocked")
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
            }
        }

        Text {
            width: parent.width
            text: win.openMsg ? (win.openMsg.subject || "(no subject)") : ""
            wrapMode: Text.Wrap
            maximumLineCount: 3
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 25
            font.weight: Font.Bold
            lineHeight: 1.18
            color: Theme.textPrimary
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }

        Row {
            spacing: 13
            Avatar {
                width: 40; height: 40; radius: 20
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

        Flow {
            width: parent.width
            spacing: 10
            visible: win.openAttachments.length > 0
            Repeater {
                model: win.openAttachments
                AttachmentChip { att: modelData }
            }
        }

        Rectangle {
            width: parent.width; height: 1
            color: Theme.hairline
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Text {
        anchors.centerIn: parent
        visible: win.openLoading
        text: "opening…"
        font.family: Theme.fontFamily
        font.pixelSize: 12
        color: Theme.textDim
    }

    // ---- Body ------------------------------------------------------------
    // HTML mail: a real WebEngine render of the mail as authored, on a white
    // sheet inset from the themed chrome. De-tracked by PixelBlock.
    Rectangle {
        id: sheet
        anchors { top: header.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }
        anchors.topMargin: 14
        anchors.leftMargin: Math.max(28, (root.width - 900) / 2)
        anchors.rightMargin: anchors.leftMargin
        anchors.bottomMargin: 22
        visible: root.htmlMode && !win.openLoading
        radius: Theme.radiusSmall
        color: "#ffffff"
        border.width: 1
        border.color: Theme.hairline
        clip: true
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
    }

    WebEngineView {
        id: web
        parent: sheet
        anchors.fill: parent
        anchors.margins: 1
        visible: root.htmlMode && !win.openLoading
        backgroundColor: "#ffffff"
        onNavigationRequested: function (req) {
            if (req.navigationType === WebEngineNavigationRequest.LinkClickedNavigation) {
                Qt.openUrlExternally(req.url)
                req.action = WebEngineNavigationRequest.IgnoreRequest
            }
        }
        onNewWindowRequested: function (req) { Qt.openUrlExternally(req.requestedUrl) }
    }

    // Plain / Markdown mail: native rich text.
    Flickable {
        anchors { top: header.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }
        anchors.topMargin: 8
        visible: win.openMsg && !win.openLoading && !root.htmlMode
        contentWidth: width
        contentHeight: body.implicitHeight + 64
        clip: true
        boundsBehavior: Flickable.StopAtBounds
        ScrollBar.vertical: ScrollBar { policy: ScrollBar.AsNeeded }

        TextEdit {
            id: body
            x: header.anchors.leftMargin
            y: 12
            width: parent.width - 2 * header.anchors.leftMargin
            readOnly: true
            selectByMouse: true
            persistentSelection: true
            wrapMode: TextEdit.Wrap
            textFormat: (win.openMsg && win.openMsg.body_format === "markdown")
                        ? TextEdit.MarkdownText : TextEdit.PlainText
            text: root.bodyText()
            font.family: Theme.fontFamily
            font.pixelSize: 13
            color: Theme.textPrimary
            selectionColor: Theme.selection
            selectedTextColor: Theme.textPrimary
            palette.link: Theme.accent
            Behavior on color { ColorAnimation { duration: Theme.anim } }
            onLinkActivated: function (link) { Qt.openUrlExternally(link) }
        }
    }
}
