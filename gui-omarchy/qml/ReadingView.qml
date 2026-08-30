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
        var bg = Theme.background, fg = Theme.foreground, acc = Theme.accent
        var card = Theme.cardBg, hair = Theme.hairline, dim = Theme.textDim
        var css =
          "html,body{background:" + bg + " !important;color:" + fg + " !important;" +
          "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;" +
          "margin:0;padding:28px;font-size:14px;line-height:1.62;}" +
          "p,div,span,td,th,li,h1,h2,h3,h4,h5,h6,strong,b,em{color:" + fg + " !important;}" +
          "a{color:" + acc + " !important;}" +
          "img{max-width:100% !important;height:auto !important;border-radius:6px;}" +
          "table{max-width:100% !important;border-color:" + hair + " !important;}" +
          "hr{border:0;border-top:1px solid " + hair + ";}" +
          "blockquote{border-left:3px solid " + acc + ";margin-left:0;padding-left:12px;color:" + dim + " !important;}" +
          "pre,code{background:" + card + " !important;color:" + fg + " !important;border-radius:6px;padding:2px 6px;}" +
          "::-webkit-scrollbar{width:10px;height:10px;}::-webkit-scrollbar-thumb{background:" + hair + ";border-radius:5px;}"
        return "<!DOCTYPE html><html><head><meta charset='utf-8'>" +
               "<meta name='color-scheme' content='" + (Theme.dark ? "dark" : "light") + "'>" +
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
    // HTML mail: real WebEngine render, themed and de-tracked.
    WebEngineView {
        id: web
        anchors { top: header.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }
        anchors.topMargin: 8
        visible: root.htmlMode && !win.openLoading
        backgroundColor: Theme.background
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
