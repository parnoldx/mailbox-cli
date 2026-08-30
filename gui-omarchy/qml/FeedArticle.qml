import QtQuick
import QtQuick.Window
import QtWebEngine

// The rendered newsletter for one Feed card. Renders body_html in a real
// WebEngine on a clean sheet (retinted by Dark Reader on a dark Omarchy theme,
// exactly like ReadingView). Collapsed it is clipped to `collapsedHeight` with a
// fade; expanded it grows to the page's own height. The web view is always laid
// out at full height so a wheel scroll falls through to the feed instead of
// scrolling the article.
Item {
    id: root

    property string html: ""
    property bool expanded: false
    property int collapsedHeight: 360
    property int maxHeight: 6000

    // Starts at the collapsed height and only moves once the page has actually
    // been measured, so a card does not visibly grow into place while its HTML
    // loads. _measure() breaks this binding with a concrete value.
    property real pageHeight: collapsedHeight
    readonly property bool overflowing: pageHeight > collapsedHeight + 24
    readonly property real shownHeight: expanded ? Math.min(pageHeight, maxHeight)
                                                 : Math.min(pageHeight, collapsedHeight)

    implicitHeight: shownHeight
    clip: true
    Behavior on implicitHeight { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }

    readonly property color sheet: Theme.dark ? Theme.background : "#ffffff"

    function _doc() {
        var dark = Theme.dark
        var bg = dark ? String(Theme.background) : "#ffffff"
        var fg = dark ? String(Theme.foreground) : "#1b1b1b"
        var css =
            "html,body{margin:0!important;padding:0!important;background:" + bg + "!important;color:" + fg + ";" +
              "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;" +
              "font-size:14px;line-height:1.6;overflow-x:hidden!important;" +
              "word-wrap:break-word;overflow-wrap:break-word;}" +
            "*{max-width:100%!important;box-sizing:border-box!important;}" +
            "img{max-width:100%!important;height:auto!important;}" +
            "table{table-layout:fixed!important;width:100%!important;max-width:100%!important;}" +
            "td,th{overflow-wrap:break-word;word-break:break-word;}" +
            "pre{white-space:pre-wrap!important;word-break:break-word;}" +
            "a{color:" + String(Theme.accent) + ";}" +
            "::-webkit-scrollbar{width:0;height:0;display:none;}"
        var tail = ""
        if (dark && typeof DarkReaderJs === "string" && DarkReaderJs.length > 0) {
            var opts = JSON.stringify({
                mode: 1, brightness: 100, contrast: 90, sepia: 0,
                darkSchemeBackgroundColor: String(Theme.background),
                darkSchemeTextColor: String(Theme.foreground),
                selectionColor: String(Theme.selection)
            })
            tail = "<scr" + "ipt>" + DarkReaderJs + "</scr" + "ipt>" +
                   "<scr" + "ipt>try{DarkReader.enable(" + opts + ");}catch(e){}</scr" + "ipt>"
        }
        return "<!DOCTYPE html><html><head><meta charset='utf-8'>" +
               "<meta name='viewport' content='width=device-width,initial-scale=1'>" +
               "<meta name='color-scheme' content='" + (dark ? "dark" : "light") + "'>" +
               "<style>" + css + "</style></head><body>" + html + tail + "</body></html>"
    }

    function _measure() {
        web.runJavaScript(
          "(function(){var b=document.body;if(!b)return 0;" +
          "return Math.ceil(Math.max(b.scrollHeight, b.getBoundingClientRect().height));})()",
          function (h) { if (h && h > 0) root.pageHeight = h })
    }

    function _render() { if (html && html.length > 0) web.loadHtml(_doc(), "about:blank") }
    onHtmlChanged: _render()
    Component.onCompleted: _render()
    Connections { target: Theme; function onChanged() { root._render() } }

    Rectangle { anchors.fill: parent; color: root.sheet
        Behavior on color { ColorAnimation { duration: Theme.anim } } }

    WebEngineView {
        id: web
        // The 1px oscillation on _nudge is what actually clears the stale black
        // surface after a Wayland unmap: a geometry change forces a fresh frame,
        // where web.update() / a JS scroll do not (the article has no internal
        // scroll to move).
        width: parent.width - (root._nudge ? 1 : 0)
        height: Math.min(root.pageHeight, root.maxHeight)
        visible: root._webShown
        backgroundColor: root.sheet
        onLoadingChanged: function (req) {
            if (req.status === WebEngineView.LoadSucceededStatus) { root._measure(); settle.restart() }
        }
        onContentsSizeChanged: root._measure()
        onNavigationRequested: function (req) {
            if (req.navigationType === WebEngineNavigationRequest.LinkClickedNavigation) {
                Qt.openUrlExternally(req.url)
                req.action = WebEngineNavigationRequest.IgnoreRequest
            }
        }
        onNewWindowRequested: function (req) { Qt.openUrlExternally(req.requestedUrl) }
    }

    // Hosted images land after load; keep re-measuring for a few seconds.
    Timer {
        id: settle
        interval: 320; repeat: true
        property int ticks: 0
        onTriggered: { root._measure(); if (++ticks > 10) { stop(); ticks = 0 } }
    }

    // Fade the cut-off edge while collapsed.
    Rectangle {
        anchors { left: parent.left; right: parent.right; bottom: parent.bottom }
        height: 96
        visible: !root.expanded && root.overflowing
        gradient: Gradient {
            GradientStop { position: 0.0; color: "transparent" }
            GradientStop { position: 1.0; color: root.sheet }
        }
    }

    // --- QtWebEngine paints a black surface after a Wayland unmap (a workspace
    // switch away and back). Nothing inside the page fixes it, so on the way
    // back we hold an opaque sheet-coloured cover over the whole article and
    // force a couple of fresh frames by flipping the view's visibility once and
    // oscillating its width by a pixel over ~4 frames. The user only ever sees
    // the sheet, and only for ~80ms.
    property bool _nudge: false
    property bool _webShown: true
    property bool covered: false
    property bool _dirty: false

    readonly property var _win: Window.window

    function _repaintCycle() {
        covered = true
        _webShown = false
        pulse.count = 0
        pulse.restart()
    }
    Timer {
        id: pulse
        interval: 16; repeat: true
        property int count: 0
        onTriggered: {
            root._webShown = true
            root._nudge = (count % 2 === 0)
            web.update()
            if (++count >= 4) { stop(); root.covered = false }
        }
    }

    // The same width oscillation as pulse but with no cover: used when the
    // window only regained focus (never unmapped), so at worst the user sees a
    // sub-frame repaint, not an ~80ms sheet flash.
    Timer {
        id: heal
        interval: 16; repeat: true
        property int count: 0
        onTriggered: {
            root._nudge = (count % 2 === 0)
            web.update()
            if (++count >= 4) { stop(); root._nudge = false }
        }
        onRunningChanged: if (running) count = 0
    }

    Connections {
        target: root._win
        enabled: root._win !== null
        function onVisibilityChanged() {
            var hidden = root._win.visibility === Window.Hidden
                      || root._win.visibility === Window.Minimized
            if (hidden) { root.covered = true; root._dirty = true }
            else if (root._dirty) { root._dirty = false; root._repaintCycle() }
        }
        function onActiveChanged() {
            // A focus change — the pointer moving to another window — does not
            // unmap the Wayland surface, so the article never actually goes
            // black here. Covering it anyway just blanks the whole feed every
            // time focus wanders. Do nothing on the way out; only force a few
            // fresh frames on the way back, in case the surface did go stale.
            if (root._win.active && !root._dirty) heal.restart()
        }
    }

    // Opaque cover: on top of the article and the fade, exactly the visible size.
    Rectangle {
        anchors.fill: parent
        z: 100
        visible: root.covered
        color: root.sheet
    }
}
