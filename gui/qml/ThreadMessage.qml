import QtQuick
import QtQuick.Controls.Basic
import QtQuick.Window
import QtWebEngine
import "MailFormat.js" as Fmt

// One Message in an open Thread's accordion: a collapsed one-line row by
// default (avatar, name, date, a snippet), expanding in place to the same
// rendering a single message used to get — real HTML via WebEngine with Dark
// Reader and inline images, or plain/markdown text otherwise. Every property
// that used to read the window's one open message now reads this instance's
// own msg, so several of these can be expanded at once without fighting over
// shared state (ADR: HEY's own thread view is exactly this — an accordion of
// equals, not one enlarged message plus summaries of the rest).
Item {
    id: root
    property var msg: ({})
    property var attachments: []
    property bool expanded: false
    readonly property bool unseen: root.msg.seen === false

    // Set by ReadingView when this is the only Message in the Thread: the
    // HTML sheet then stretches to fill `viewportHeight` (the reader's
    // Flickable height) minus the sender row and attachments, rather than
    // sitting at the fixed accordion size. The WebEngineView scrolls its
    // own content inside that.
    property bool sole: false
    property real viewportHeight: 0
    // The reader's Flickable, so a wheel over a fully-visible mail can still
    // scroll the accordion (QtWebEngine otherwise swallows the event).
    property var scroller: null
    readonly property real fillHeight: Math.max(320,
        root.viewportHeight
            - senderRow.height
            - rule.height
            - body.spacing * 2
            - (attachBlock.visible ? attachBlock.height + body.spacing : 0))

    // One of several expanded Messages sharing the accordion: the HTML sheet
    // is sized to its own rendered content (measured out of the page after it
    // loads) instead of a fixed 520, so a two-line reply is two lines tall and
    // "Expand all" doesn't bury the rest under screens of blank sheet. A long
    // mail is still capped just under the viewport and scrolls inside itself.
    property real htmlContentHeight: 0
    readonly property real maxSheet: Math.max(280, root.viewportHeight - 140)
    readonly property real sheetHeight: {
        if (!root.htmlMode) return 0
        if (root.sole) return root.fillHeight
        var h = root.htmlContentHeight > 0 ? root.htmlContentHeight + 4 : 260
        return Math.min(root.maxSheet, Math.max(96, h))
    }

    function measureHtml() {
        if (!root.htmlMode || !webLoader.item) return
        // Measure the wrapper div, not document.scrollHeight: the latter is
        // never smaller than the WebEngineView's own height, so once the sheet
        // shrinks it reports the sheet height back and the box can't get any
        // smaller. The in-flow div reports its true content extent. +44 for
        // the body's 22px padding either side.
        webLoader.item.runJavaScript(
            "(function(){var e=document.getElementById('__mb');" +
            "return e?Math.ceil(e.getBoundingClientRect().height)+44:0;})()",
            function (h) { if (h && h > 0) root.htmlContentHeight = h })
    }
    onWidthChanged: if (root.htmlMode) root.measureHtml()

    width: parent ? parent.width : 0
    height: col.implicitHeight

    // The WebEngineView is the expensive part, so it only exists at all while
    // this Message is both expanded and HTML — collapse it and the Loader
    // tears the whole render down.
    readonly property bool htmlMode: root.expanded
        && !!(root.msg.body_html && root.msg.body_html.length > 0)

    // Dark mail. -1 = follow the Omarchy theme, 0 = the mail's own colours,
    // 1 = forced dark. Reset whenever a different Message expands into here.
    property int darkOverride: -1
    readonly property bool applyDark: root.htmlMode
        && (darkOverride === -1 ? Theme.dark : darkOverride === 1)
    function drJs() { return (typeof DarkReaderJs === "string") ? DarkReaderJs : "" }

    // Inline images: an HTML body refers to them as <img src="cid:ID">; we
    // fetch each with `attachment bytes` and patch the live DOM with a data:
    // URI once the page is up (see internal/daemon for why not all at once).
    property var cidMap: ({})
    property bool webLoaded: false

    function fromName(s) { return Fmt.displayName(s) }
    function fromAddr(s) { return Fmt.address(s) }
    function niceDate(s) {
        var d = new Date(s)
        return isNaN(d.getTime()) ? (s || "") : Qt.formatDateTime(d, "d MMM yyyy · HH:mm")
    }
    // One line of taste for the collapsed row — not a rendering of the body,
    // just enough to remember what this Message said without opening it.
    function snippet() {
        var t = String(root.msg.body || "").replace(/\s+/g, " ").trim()
        return t.length > 140 ? t.slice(0, 140) + "…" : t
    }
    function bodyText() {
        var b = root.msg.body || ""
        if (root.msg.body_format === "markdown")
            b = b.replace(/!\[[^\]]*\]\([^)]*\)/g, "")
        return b.trim() || "(this message has no text part yet)"
    }

    function inlineNeeds() {
        var out = []
        if (!root.htmlMode) return out
        var html = root.msg.body_html
        var atts = root.attachments || []
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

    function patchCid(cid, uri) {
        if (!root.webLoaded || !webLoader.item) return
        var js =
          "(function(){var c=" + JSON.stringify("cid:" + cid) + ",u=" + JSON.stringify(uri) + ";" +
          "var n=document.getElementsByTagName('img');" +
          "for(var i=0;i<n.length;i++){if(n[i].getAttribute('src')===c)n[i].src=u;}})();"
        webLoader.item.runJavaScript(js)
    }
    function flushCids() {
        for (var cid in root.cidMap) root.patchCid(cid, root.cidMap[cid])
    }

    function themedHtml() {
        if (!root.htmlMode) return ""
        var dark = root.applyDark
        var pageBg = dark ? String(Theme.background) : "#ffffff"
        var pageFg = dark ? String(Theme.foreground) : "#1b1b1b"
        var sbThumb = dark ? String(Theme.selection) : "#c9c9c9"
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
                    mode: 1, brightness: 100, contrast: 90, sepia: 0,
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
        // Strip any <meta> the mail carried in its own <head>: this wrapper
        // sets charset / viewport / color-scheme already, and newsletter HTML
        // often ships `content="width=device-width; initial-scale=1"` whose
        // ';' makes Chromium log a parse warning. Also drops http-equiv
        // refresh redirects and stray CSP from the body.
        var mail = String(root.msg.body_html).replace(/<meta\b[^>]*>/gi, "")
        // DarkReader overrides colours through a stylesheet rule, which the
        // cascade ranks below an inline `style="…!important"` on the element —
        // so a mail that hard-codes a light `background:#fff !important` inline
        // (e.g. the unbox.at catch-all banner) stays light. Drop !important
        // from inline background/colour decls only so the invert can land.
        if (dark)
            mail = mail.replace(/\sstyle=(["'])([\s\S]*?)\1/gi, function (m, q, decls) {
                return " style=" + q + decls.replace(
                    /((?:^|;)\s*(?:background-color|background|color)\s*:[^;]*?)\s*!\s*important/gi,
                    "$1") + q
            })
        return head + "<body><div id='__mb'>" + mail + "</div>" + tail + "</body></html>"
    }

    function renderHtml() {
        if (!root.htmlMode || !webLoader.item) return
        root.webLoaded = false
        // A dark mail's own stylesheet paints white for a frame before the
        // DarkReader invert lands. Drop the opaque (already-dark) cover over
        // the sheet now; the load handler lifts it once the page is up and
        // re-tinted, so the mail only ever appears dark.
        if (root.applyDark) root.webCovered = true
        webLoader.item.loadHtml(root.themedHtml(), "about:blank")
    }
    function reloadHtml() {
        if (!root.htmlMode) return
        root.cidMap = ({})
        root.renderHtml()
        root.inlineNeeds().forEach(function (need) {
            Mailbox.call(["attachment", "bytes"], { positional: need.id }, function (r) {
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
    onAttachmentsChanged: reloadHtml()
    Connections { target: Theme; function onChanged() { root.renderHtml() } }

    // --- QtWebEngine leaves a black GPU surface behind once its Wayland surface
    // has been unmapped and remapped (a Hyprland workspace switch away and
    // back). Nudging geometry / flipping visibility does not reliably bring it
    // back; tearing the WebEngineView down and building a fresh one does. So on
    // the way back from anything that may have unmapped us, save the reading
    // position, recreate the view, and restore the scroll under an opaque cover.
    property bool webCovered: false
    property bool reviving: false
    property real savedScroll: 0

    function repaintCycle() {
        if (!root.htmlMode) return
        webCovered = true
        if (webLoader.item) {
            webLoader.item.runJavaScript("window.scrollY || 0", function (y) {
                root.savedScroll = y || 0
                root._recreate()
            })
        } else {
            root._recreate()
        }
    }
    function _recreate() { root.reviving = true; reviveTimer.restart() }
    Timer {
        id: reviveTimer
        interval: 60
        onTriggered: {
            root.reviving = false
            if (!root.htmlMode) root.webCovered = false
        }
    }

    function restoreScroll() {
        var y = root.savedScroll
        root.savedScroll = 0
        if (y > 1 && webLoader.item)
            webLoader.item.runJavaScript(
                "(function(){var n=0,iv=setInterval(function(){window.scrollTo(0," + y +
                ");if(++n>16)clearInterval(iv);},60);})()")
        uncoverTimer.restart()
    }
    Timer { id: uncoverTimer; interval: 220; onTriggered: root.webCovered = false }

    // Window hidden/minimised, or a bare workspace switch (SurfaceWatcher) —
    // cover up, and rebuild the view on the way back.
    WebRevive {
        window: Window.window
        active: root.htmlMode
        onObscured: root.webCovered = true
        onNeedsRepaint: root.repaintCycle()
    }

    Column {
        id: col
        width: parent.width
        spacing: 10

        // ---- Collapsed / header row — always visible, click toggles. -----
        Rectangle {
            width: parent.width
            height: 52
            radius: Theme.radiusSmall
            visible: !root.expanded
            color: rowHover.hovered ? Theme.cardHover : "transparent"
            Behavior on color { ColorAnimation { duration: Theme.anim } }

            Row {
                anchors.fill: parent
                anchors.margins: 8
                spacing: 12
                Avatar {
                    width: 28; height: 28; radius: 14
                    anchors.verticalCenter: parent.verticalCenter
                    name: root.fromName(root.msg.from)
                    seed: root.fromAddr(root.msg.from)
                }
                Column {
                    width: parent.width - 28 - 12 - 76
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 2
                    Text {
                        width: parent.width
                        text: root.fromName(root.msg.from)
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        font.weight: root.unseen ? Font.DemiBold : Font.Normal
                        color: root.unseen ? Theme.textPrimary : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        width: parent.width
                        text: root.snippet()
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                Text {
                    anchors.verticalCenter: parent.verticalCenter
                    text: root.niceDate(root.msg.date)
                    font.family: Theme.fontFamily
                    font.pixelSize: 10
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
            }
            HoverHandler { id: rowHover; cursorShape: Qt.PointingHandCursor }
            TapHandler { onTapped: win.toggleExpanded(root.msg.id) }
        }

        // ---- Expanded — the full message. ---------------------------------
        Column {
            id: body
            width: parent.width
            spacing: 14
            visible: root.expanded

            Row {
                id: senderRow
                width: parent.width
                spacing: 13
                Avatar {
                    width: 36; height: 36; radius: 18
                    anchors.verticalCenter: parent.verticalCenter
                    name: root.fromName(root.msg.from)
                    seed: root.fromAddr(root.msg.from)
                }
                Column {
                    width: parent.width - 36 - 13 - 90
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 3
                    Text {
                        text: root.fromName(root.msg.from)
                        font.family: Theme.fontFamily
                        font.pixelSize: 13
                        font.weight: Font.DemiBold
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        text: root.fromAddr(root.msg.from)
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                Column {
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: 6
                    Text {
                        anchors.right: parent.right
                        text: root.niceDate(root.msg.date)
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    // Dark-mail toggle, this Message's own — HTML mail only.
                    Rectangle {
                        anchors.right: parent.right
                        visible: root.htmlMode
                        width: dmRow.implicitWidth + 16
                        height: 18
                        radius: 9
                        color: dmHover.hovered ? Theme.cardHover : Theme.selection
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                        Row {
                            id: dmRow
                            anchors.centerIn: parent
                            spacing: 4
                            Text {
                                text: root.applyDark ? "dark" : "original"
                                font.family: Theme.fontFamily
                                font.pixelSize: 9
                                color: Theme.textDim
                                Behavior on color { ColorAnimation { duration: Theme.anim } }
                            }
                        }
                        HoverHandler { id: dmHover; cursorShape: Qt.PointingHandCursor }
                        TapHandler { onTapped: root.darkOverride = root.applyDark ? 0 : 1 }
                    }
                }
            }

            // Attachments — collapsed behind a toggle when every part is an
            // inline image the body already shows, same as before.
            Column {
                id: attachBlock
                width: parent.width
                spacing: 10
                visible: root.attachments.length > 0

                readonly property bool allInline: {
                    if (root.attachments.length === 0) return false
                    for (var i = 0; i < root.attachments.length; i++) {
                        var a = root.attachments[i]
                        if (a.disposition !== "inline") return false
                        if (String(a.mime_type || "").indexOf("image") !== 0) return false
                    }
                    return true
                }
                property bool showInline: false
                onAllInlineChanged: showInline = false

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
                            text: root.attachments.length
                                  + (root.attachments.length === 1 ? " inline image" : " inline images")
                            font.family: Theme.fontFamily
                            font.pixelSize: 10
                            color: Theme.textDim
                            Behavior on color { ColorAnimation { duration: Theme.anim } }
                        }
                    }
                    HoverHandler { id: exHover; cursorShape: Qt.PointingHandCursor }
                    TapHandler { onTapped: attachBlock.showInline = !attachBlock.showInline }
                }

                Flow {
                    width: parent.width
                    spacing: 10
                    visible: !attachBlock.allInline || attachBlock.showInline
                    Repeater {
                        model: root.attachments
                        AttachmentChip { att: modelData }
                    }
                }
            }

            // HTML mail: real WebEngine render, only instantiated while this
            // Message is both expanded and HTML — that's the whole point of
            // the Loader, versus a WebEngineView permanently alive per row.
            Rectangle {
                id: sheet
                width: parent.width
                height: root.sheetHeight
                visible: root.htmlMode
                Behavior on height { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }
                radius: Theme.radiusSmall
                color: root.applyDark ? Theme.background : "#ffffff"
                border.width: 1
                border.color: Theme.hairline
                clip: true
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on border.color { ColorAnimation { duration: Theme.anim } }

                Loader {
                    id: webLoader
                    anchors.fill: parent
                    anchors.margins: 1
                    // `reviving` blips false to tear the view down and rebuild a
                    // fresh one after a Wayland remap.
                    active: root.htmlMode && !root.reviving
                    onLoaded: root.renderHtml()
                    sourceComponent: WebEngineView {
                        backgroundColor: root.applyDark ? Theme.background : "#ffffff"
                        // The page's rendered height moves after first paint —
                        // inline images decode, DarkReader re-tints, a width
                        // change reflows. This fires on each of those, so the
                        // sheet resizes to real content without a guessed delay.
                        onContentsSizeChanged: if (root.htmlMode) root.measureHtml()
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
                                root.measureHtml()
                                // Also lifts the dark-mail anti-flash cover set
                                // in renderHtml() (savedScroll 0 → just uncovers).
                                if (root.webCovered) root.restoreScroll()
                            } else if (req.status === WebEngineView.LoadFailedStatus) {
                                // Never leave the mail stuck behind the cover.
                                if (root.webCovered && !root.reviving && !root.webDirty)
                                    root.webCovered = false
                            }
                        }
                    }
                }
                Rectangle {
                    anchors.fill: parent
                    anchors.margins: 1
                    z: 1
                    visible: root.webCovered
                    color: sheet.color
                }

                // When the mail fits its sheet there is nothing to scroll
                // inside the web view, yet QtWebEngine still eats the wheel.
                // A transparent lid on top catches the wheel and drives the
                // reader instead, so the accordion keeps scrolling under the
                // pointer. `acceptedButtons: NoButton` lets clicks, link hits
                // and text selection fall straight through to the web view.
                // Disabled the moment the mail is tall enough to scroll itself.
                MouseArea {
                    anchors.fill: parent
                    z: 2
                    acceptedButtons: Qt.NoButton
                    propagateComposedEvents: true
                    enabled: !!root.scroller && root.htmlContentHeight > 0
                             && root.htmlContentHeight <= root.sheetHeight + 1
                    onWheel: function (wheel) {
                        var f = root.scroller
                        if (!f) { wheel.accepted = false; return }
                        var max = Math.max(0, f.contentHeight - f.height)
                        var dy = wheel.angleDelta.y !== 0 ? wheel.angleDelta.y
                                                          : wheel.pixelDelta.y
                        f.contentY = Math.max(0, Math.min(max, f.contentY - dy))
                        wheel.accepted = true
                    }
                }
            }

            // Plain / Markdown mail: native rich text, no Loader needed — it
            // is cheap enough to just toggle visible with the rest.
            TextEdit {
                width: parent.width
                visible: root.expanded && !root.htmlMode
                readOnly: true
                selectByMouse: true
                persistentSelection: true
                wrapMode: TextEdit.Wrap
                textFormat: root.msg.body_format === "markdown" ? TextEdit.MarkdownText : TextEdit.PlainText
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

            Rectangle {
                id: rule
                width: parent.width; height: 1
                color: Theme.hairline
                opacity: 0.5
                Behavior on color { ColorAnimation { duration: Theme.anim } }
            }
        }
    }
}
