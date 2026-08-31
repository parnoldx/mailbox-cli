import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Window
import QtWebEngine

Item {
    id: root

    readonly property bool htmlMode: !!(win.openMsg && win.openMsg.body_html && win.openMsg.body_html.length > 0)

    // Dark mail. -1 = follow the Omarchy theme, 0 = force the mail's own colours,
    // 1 = force the dark treatment. Reset to -1 whenever a new message opens.
    property int darkOverride: -1
    readonly property bool applyDark: root.htmlMode
        && (darkOverride === -1 ? Theme.dark : darkOverride === 1)
    // Vendored Dark Reader engine, injected from C++ as a plain string.
    function drJs() { return (typeof DarkReaderJs === "string") ? DarkReaderJs : "" }

    // Inline images. An HTML body refers to them as <img src="cid:ID">; the
    // bytes live in an `inline` part the daemon only fetches when named. We pull
    // each one with `attachment bytes` and, once the page is up, point the <img>
    // at a data: URI from JavaScript — a data: URI per image is fine in the live
    // DOM, but the whole set at once would blow loadHtml's 2 MB argument cap.
    // cidMap is keyed by Content-ID; cidMsg is the message it was built for.
    property var cidMap: ({})
    property string cidMsg: ""
    property bool webLoaded: false

    function msgId() { return win.openMsg ? String(win.openMsg.id || "") : "" }

    // The inline parts this body actually references and we have not fetched.
    function inlineNeeds() {
        var out = []
        if (!root.htmlMode) return out
        var html = win.openMsg.body_html
        var atts = win.openAttachments || []
        for (var i = 0; i < atts.length; i++) {
            var a = atts[i]
            var cid = String(a.content_id || "")
            if (!cid || a.disposition !== "inline") continue
            if (String(a.mime_type || "").indexOf("image") !== 0) continue
            if (root.cidMap[cid] !== undefined) continue
            if (html.indexOf("cid:" + cid) < 0) continue
            out.push({ id: a.id, cid: cid })
        }
        return out
    }

    // Point every <img src="cid:CID"> at its data: URI, in the live DOM.
    function patchCid(cid, uri) {
        if (!root.webLoaded) return
        var js =
          "(function(){var c=" + JSON.stringify("cid:" + cid) + ",u=" + JSON.stringify(uri) + ";" +
          "var n=document.getElementsByTagName('img');" +
          "for(var i=0;i<n.length;i++){if(n[i].getAttribute('src')===c)n[i].src=u;}})();"
        web.runJavaScript(js)
    }

    function flushCids() {
        for (var cid in root.cidMap)
            root.patchCid(cid, root.cidMap[cid])
    }

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

    // Wrap the message HTML in a stylesheet built from the live Omarchy palette.
    // By default we render the mail as its author designed it on a clean white
    // sheet; on a dark Omarchy theme (or when the reader forces it) we also
    // inline the Dark Reader engine, which analyses the mail's real styles and
    // images and re-tints them onto the app's own background.
    function themedHtml() {
        if (!root.htmlMode) return ""
        var dark = root.applyDark
        var pageBg = dark ? String(Theme.background) : "#ffffff"
        var pageFg = dark ? String(Theme.foreground) : "#1b1b1b"
        var sbThumb = dark ? String(Theme.selection) : "#c9c9c9"
        // Clamp every box to the viewport and let fixed-width tables collapse so
        // the mail never scrolls sideways.
        var css =
          "html{overflow-x:hidden !important;max-width:100% !important;}" +
          "*{max-width:100% !important;box-sizing:border-box !important;}" +
          "body{margin:0;padding:22px;background:" + pageBg + ";color:" + pageFg + ";" +
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
          "::-webkit-scrollbar-thumb{background:" + sbThumb + ";border-radius:5px;}"

        var head =
          "<!DOCTYPE html><html><head><meta charset='utf-8'>" +
          "<meta name='viewport' content='width=device-width, initial-scale=1'>" +
          "<meta name='color-scheme' content='" + (dark ? "dark" : "light") + "'>" +
          "<style>" + css + "</style></head>"

        var tail = ""
        if (dark) {
            var lib = root.drJs()
            if (lib.length > 0) {
                var opts = JSON.stringify({
                    mode: 1,
                    brightness: 100,
                    contrast: 90,
                    sepia: 0,
                    darkSchemeBackgroundColor: String(Theme.background),
                    darkSchemeTextColor: String(Theme.foreground),
                    selectionColor: String(Theme.selection)
                })
                tail =
                  "<scr" + "ipt>" + lib + "</scr" + "ipt>" +
                  "<scr" + "ipt>try{DarkReader.enable(" + opts + ");}" +
                  "catch(e){console.warn('DarkReader:',e);}</scr" + "ipt>"
            }
        }

        return head + "<body>" + win.openMsg.body_html + tail + "</body></html>"
    }

    function renderHtml() {
        if (!root.htmlMode) return
        root.webLoaded = false
        web.loadHtml(root.themedHtml(), "about:blank")
    }

    // Render the mail now, then fetch every inline image it references and drop
    // each one into the DOM as it lands.
    function reloadHtml() {
        if (!root.htmlMode) return
        if (root.cidMsg !== root.msgId()) {
            root.cidMap = ({})
            root.cidMsg = root.msgId()
        }
        root.renderHtml()
        var token = root.msgId()
        root.inlineNeeds().forEach(function (need) {
            Mailbox.call(["attachment", "bytes"], { positional: need.id }, function (r) {
                if (root.msgId() !== token) return
                if (r && r.ok && r.data && r.data.base64) {
                    var uri = "data:" + (r.data.mime_type || "image/png") + ";base64," + r.data.base64
                    root.cidMap[need.cid] = uri
                    root.patchCid(need.cid, uri)
                }
            })
        })
    }

    onHtmlModeChanged: reloadHtml()
    onApplyDarkChanged: renderHtml()
    Connections {
        target: win
        function onOpenMsgChanged() {
            root.darkOverride = -1
            root.reloadHtml()
        }
        function onOpenAttachmentsChanged() { root.reloadHtml() }
    }
    Connections { target: Theme; function onChanged() { root.renderHtml() } }

    // QtWebEngine keeps a stale GPU surface across a Wayland unmap/map (switch
    // workspace away and back) and paints it black until something forces a
    // fresh frame. web.update() and a JS scroll do not; a 1px geometry change
    // does. On the way out we raise an opaque sheet-coloured cover; on the way
    // back we flip the view's visibility once and oscillate its width by a
    // pixel over a few frames under the cover, then drop it. Same fix as
    // FeedArticle — the user only ever sees the sheet, for ~80ms.
    property bool webCovered: false
    property bool webShown: true
    property bool webNudge: false
    property bool webDirty: false

    function repaintCycle() {
        webCovered = true
        webShown = false
        webPulse.count = 0
        webPulse.restart()
    }
    Timer {
        id: webPulse
        interval: 16; repeat: true
        property int count: 0
        onTriggered: {
            root.webShown = true
            root.webNudge = (count % 2 === 0)
            web.update()
            if (++count >= 4) { stop(); root.webCovered = false }
        }
    }
    // Width oscillation with no cover: used when the window only regained focus
    // (never unmapped), so at worst a sub-frame repaint, not a sheet flash.
    Timer {
        id: webHeal
        interval: 16; repeat: true
        property int count: 0
        onRunningChanged: if (running) count = 0
        onTriggered: {
            root.webNudge = (count % 2 === 0)
            web.update()
            if (++count >= 4) { stop(); root.webNudge = false }
        }
    }
    Connections {
        target: win
        enabled: root.htmlMode
        function onVisibilityChanged() {
            var hidden = win.visibility === Window.Hidden || win.visibility === Window.Minimized
            if (hidden) { root.webCovered = true; root.webDirty = true }
            else if (root.webDirty) { root.webDirty = false; root.repaintCycle() }
        }
        function onActiveChanged() {
            // A focus change — the pointer moving to another window — does not
            // unmap the Wayland surface, so don't cover: that would blank the
            // reader every time focus wanders. Only force a few fresh frames on
            // the way back, in case the surface went stale anyway.
            if (win.active && !root.webDirty) webHeal.restart()
        }
    }

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

            // Dark-mail toggle: flips this message between the Dark Reader
            // treatment and its original colours.
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                visible: root.htmlMode
                width: dmRow.implicitWidth + 18
                height: 20
                radius: 10
                color: dmHover.hovered ? Theme.cardHover : Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: dmRow
                    anchors.centerIn: parent
                    spacing: 5
                    Text {
                        text: root.applyDark ? "" : ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: root.applyDark ? "dark mail" : "original colours"
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: dmHover }
                TapHandler { onTapped: root.darkOverride = root.applyDark ? 0 : 1 }
            }

            // Reply / Reply all — hand off to the compose view in reply mode.
            Repeater {
                model: [ { label: "Reply", all: false }, { label: "Reply all", all: true } ]
                Rectangle {
                    anchors.verticalCenter: parent.verticalCenter
                    visible: !!win.openMsg
                    width: rpRow.implicitWidth + 18
                    height: 20
                    radius: 10
                    color: rpHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Row {
                        id: rpRow
                        anchors.centerIn: parent
                        spacing: 5
                        Text {
                            text: modelData.all ? "" : ""
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: rpHover.hovered ? Theme.accent : Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                        Text {
                            text: modelData.label
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: rpHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: win.startReply(modelData.all) }
                }
            }

            // Trash — the reading view's one destructive action. Drops back to
            // the list, then fires the daemon call. (Key: T.)
            Rectangle {
                anchors.verticalCenter: parent.verticalCenter
                visible: !!win.openMsg
                width: trRow.implicitWidth + 18
                height: 20
                radius: 10
                color: trHover.hovered ? Theme.red : Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: trRow
                    anchors.centerIn: parent
                    spacing: 5
                    Text {
                        text: ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: trHover.hovered ? "#ffffff" : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: "Trash"
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: trHover.hovered ? "#ffffff" : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: trHover; cursorShape: Qt.PointingHandCursor }
                TapHandler { onTapped: win.trashCurrent() }
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

        // Attachments. When every part is an inline image — something the body
        // refers to rather than a real enclosure — collapse the cards behind a
        // small toggle so a picture-heavy newsletter doesn't open under a wall
        // of chips.
        Column {
            id: attachBlock
            width: parent.width
            spacing: 10
            visible: win.openAttachments.length > 0

            readonly property bool allInline: {
                if (win.openAttachments.length === 0) return false
                for (var i = 0; i < win.openAttachments.length; i++) {
                    var a = win.openAttachments[i]
                    if (a.disposition !== "inline") return false
                    if (String(a.mime_type || "").indexOf("image") !== 0) return false
                }
                return true
            }
            property bool expanded: false
            onAllInlineChanged: expanded = false

            Rectangle {
                visible: attachBlock.allInline
                width: exRow.implicitWidth + 20
                height: 24
                radius: 12
                color: exHover.hovered ? Theme.cardHover : Theme.selection
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Row {
                    id: exRow
                    anchors.centerIn: parent
                    spacing: 6
                    Text {
                        text: attachBlock.expanded ? "" : ""
                        font.family: Theme.fontFamily
                        font.pixelSize: 9
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: win.openAttachments.length
                              + (win.openAttachments.length === 1 ? " inline image" : " inline images")
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                HoverHandler { id: exHover }
                TapHandler { onTapped: attachBlock.expanded = !attachBlock.expanded }
            }

            Flow {
                width: parent.width
                spacing: 10
                visible: !attachBlock.allInline || attachBlock.expanded
                Repeater {
                    model: win.openAttachments
                    AttachmentChip { att: modelData }
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
        anchors.centerIn: parent
        visible: win.openLoading
        text: "opening…"
        font.family: Theme.fontFamily
        font.pixelSize: 12
        color: Theme.textDim
    }

    // ---- Body ------------------------------------------------------------
    // HTML mail: a real WebEngine render inset from the themed chrome. Rendered
    // on a white sheet as authored, or re-tinted by Dark Reader onto the app's
    // own background when applyDark. De-tracked by PixelBlock.
    Rectangle {
        id: sheet
        anchors { top: header.bottom; left: parent.left; right: parent.right; bottom: parent.bottom }
        anchors.topMargin: 14
        anchors.leftMargin: Math.max(28, (root.width - 900) / 2)
        anchors.rightMargin: anchors.leftMargin
        anchors.bottomMargin: 22
        visible: root.htmlMode && !win.openLoading && !win.composeOpen
        radius: Theme.radiusSmall
        color: root.applyDark ? Theme.background : "#ffffff"
        border.width: 1
        border.color: Theme.hairline
        clip: true
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
    }

    WebEngineView {
        id: web
        parent: sheet
        anchors.fill: parent
        anchors.margins: 1
        // The 1px right-margin oscillation on webNudge is what actually clears a
        // stale black surface after a Wayland unmap — a geometry change forces a
        // fresh frame where web.update() and a JS scroll do not.
        anchors.rightMargin: 1 + (root.webNudge ? 1 : 0)
        visible: root.webShown && root.htmlMode && !win.openLoading && !win.composeOpen
        backgroundColor: root.applyDark ? Theme.background : "#ffffff"
        onNavigationRequested: function (req) {
            if (req.navigationType === WebEngineNavigationRequest.LinkClickedNavigation) {
                Qt.openUrlExternally(req.url)
                req.action = WebEngineNavigationRequest.IgnoreRequest
            }
        }
        onNewWindowRequested: function (req) { Qt.openUrlExternally(req.requestedUrl) }
        onLoadingChanged: function (req) {
            if (req.status === WebEngineView.LoadSucceededStatus) {
                root.webLoaded = true
                root.flushCids()
            }
        }
    }

    // Opaque cover over the web view for the brief window between a Wayland
    // remap and the page repainting, so that gap reads as the sheet, not black.
    Rectangle {
        parent: sheet
        anchors.fill: parent
        anchors.margins: 1
        z: 1
        visible: root.webCovered && root.htmlMode && !win.openLoading && !win.composeOpen
        color: sheet.color
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
