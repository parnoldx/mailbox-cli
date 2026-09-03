import QtQuick
import QtQuick.Window
import QtWebEngine

// The compose body editor: Basecamp's Lexxy (github.com/basecamp/lexxy) running
// in a WebEngineView. The self-contained bundle and its stylesheet are inlined
// from C++ (LexxyJs / LexxyCss) the same way ReadingView inlines Dark Reader.
//
// Content is HTML. getHtml() is async — WebEngine round-trips through
// runJavaScript — so callers pass a callback.
Item {
    id: root

    property bool loaded: false
    signal ready()
    // A file dropped onto the editor. Lexxy's own uploader is disabled
    // (attachments='false'), so Chromium would otherwise navigate the view to
    // the file; onNavigationRequested catches that and hands the path here for
    // ComposerView's attachment tray.
    signal fileDropped(string url)

    function _js(s) { return JSON.stringify(s === undefined ? "" : s) }

    // Retint Lexxy to the live Omarchy palette. Its whole colour system derives
    // from an "ink" ramp (near-black → white) plus an accent ramp, so overriding
    // those roots on :root recolours the toolbar, borders, code blocks and text
    // in one go. Kept in its own <style id> so a theme switch rewrites it
    // without reloading the editor.
    function paletteCss() {
        return "html,body{margin:0;padding:0;height:100%;background:transparent;}" +
            "body{display:flex;flex-direction:column;}" +
            ":root{" +
            "--lexxy-color-ink:" + Theme.textPrimary + ";" +
            "--lexxy-color-ink-medium:" + Theme.textDim + ";" +
            "--lexxy-color-ink-light:" + Theme.textDim + ";" +
            "--lexxy-color-ink-lighter:" + Theme.hairline + ";" +
            "--lexxy-color-ink-lightest:" + Theme.selection + ";" +
            "--lexxy-color-ink-inverted:" + Theme.cardBg + ";" +
            "--lexxy-color-canvas:" + Theme.cardBg + ";" +
            "--lexxy-color-accent-dark:" + Theme.accent + ";" +
            "--lexxy-color-accent-medium:" + Theme.accent + ";" +
            "--lexxy-color-accent-light:" + Theme.selection + ";" +
            "--lexxy-color-accent-lightest:" + Theme.selection + ";" +
            "--lexxy-color-blue:" + Theme.blue + ";--lexxy-color-green:" + Theme.green + ";" +
            "--lexxy-color-red:" + Theme.red + ";--lexxy-color-purple:" + Theme.magenta + ";" +
            "--lexxy-color-code-bg:" + Theme.windowBg + ";" +
            "--lexxy-radius:" + Theme.radiusSmall + "px;" +
            "}" +
            // Lexxy sizes itself to ~8 rows and draws its own border; here it is
            // the whole pane, so stretch it (and its contenteditable) to fill
            // the WebEngine viewport and drop the frame — ComposerView already
            // wraps it in one.
            "lexxy-editor{background:transparent;color:" + Theme.textPrimary + ";" +
            "font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;" +
            "display:flex;flex-direction:column;flex:1 1 auto;min-height:0;border:0;border-radius:0;}" +
            "lexxy-editor .lexxy-editor__content{flex:1 1 auto;min-block-size:0;overflow-y:auto;}" +
            "lexxy-editor [contenteditable]{padding:14px 6px;font-size:14px;line-height:1.6;}" +
            "lexxy-editor ::selection{background:" + Theme.selection + ";}"
    }

    function document_() {
        var lib = (typeof LexxyJs === "string") ? LexxyJs : ""
        var base = (typeof LexxyCss === "string") ? LexxyCss : ""
        return "<!DOCTYPE html><html><head><meta charset='utf-8'>" +
            "<style>" + base + "</style>" +
            "<style id='omarchy'>" + root.paletteCss() + "</style>" +
            "<scr" + "ipt type='module'>" + lib + "</scr" + "ipt></head>" +
            // attachments='false' — this app has no direct-upload endpoint, so
            // Lexxy's inline uploader just spins forever. Dropped files go to
            // ComposerView's attachment tray instead (see fileDropped).
            "<body><lexxy-editor id='ed' attachments='false'></lexxy-editor></body></html>"
    }

    function reload() {
        root.loaded = false
        web.loadHtml(root.document_(), "about:blank")
    }

    function getHtml(cb) {
        if (!root.loaded) { cb(""); return }
        web.runJavaScript("(function(){var e=document.getElementById('ed');return e?(e.value||''):'';})()",
                          function (v) { cb(v || "") })
    }
    function setHtml(html, atStart) {
        pendingHtml = html || ""
        pendingFocusStart = !!atStart
        if (root.loaded) root._applyPending()
    }
    property string pendingHtml: ""
    property bool pendingFocusStart: false
    function _applyPending() {
        web.runJavaScript("(function(){var e=document.getElementById('ed');if(e)e.value=" +
                          root._js(root.pendingHtml) + ";})()")
        if (root.pendingFocusStart) {
            root.pendingFocusStart = false
            root.focusStart()
        }
    }
    function clear() { setHtml("") }
    function focusEditor() {
        web.runJavaScript("(function(){var e=document.getElementById('ed');if(e)e.focus();})()")
    }
    // Focus with the caret at the very start of the document rather than the
    // end — what a reply wants, so you type above the quoted parent. Lexxy is
    // Lexical underneath; its editor takes focus({defaultSelection:'rootStart'}).
    function focusStart() {
        web.runJavaScript(
            "(function(){var e=document.getElementById('ed');if(!e)return;" +
            "try{e.editor.focus(null,{defaultSelection:'rootStart'});return;}catch(x){}" +
            "if(e.focus)e.focus();})()")
    }

    onVisibleChanged: if (visible && !root.loaded) reload()
    Component.onCompleted: reload()

    Connections {
        target: Theme
        function onChanged() {
            if (!root.loaded) return
            web.runJavaScript("(function(){var s=document.getElementById('omarchy');if(s)s.textContent=" +
                              root._js(root.paletteCss()) + ";})()")
        }
    }

    Rectangle { anchors.fill: parent; color: Theme.cardBg; Behavior on color { ColorAnimation { duration: Theme.anim } } }

    WebEngineView {
        id: web
        anchors.fill: parent
        // Arm the tracking-pixel blocker on the default profile before this
        // view loads anything. main.cpp leaves it unarmed so the Inbox does not
        // pay Chromium start-up; the first web view to mount pays it here.
        Component.onCompleted: PixelBlock.arm()
        // The 1px width oscillation on _nudge is what actually clears a stale
        // black surface after a Wayland unmap; web.update() alone does not.
        anchors.rightMargin: root._nudge ? 1 : 0
        visible: root._webShown
        backgroundColor: "transparent"
        onNavigationRequested: function (req) {
            var u = req.url.toString()
            if (u.indexOf("file:") === 0) {
                // A file dropped onto the editor: Chromium wants to open it.
                // Keep the editor put and pass the path up to be attached.
                root.fileDropped(u)
                req.action = WebEngineNavigationRequest.IgnoreRequest
                return
            }
            if (req.navigationType === WebEngineNavigationRequest.LinkClickedNavigation) {
                Qt.openUrlExternally(req.url)
                req.action = WebEngineNavigationRequest.IgnoreRequest
            }
        }
        onLoadingChanged: function (req) {
            if (req.status === WebEngineView.LoadSucceededStatus) {
                root.loaded = true
                if (root.pendingHtml.length > 0) root._applyPending()
                root.ready()
            }
        }
        onJavaScriptConsoleMessage: function (level, message, line, source) {
            if (level >= WebEngineView.WarningMessageLevel)
                console.warn("lexxy:", message)
        }
    }

    // QtWebEngine paints a stale black surface after a Wayland unmap (a
    // workspace switch away and back). A plain focus change to another window
    // does NOT unmap the surface, so covering the editor on every !win.active
    // just blanks it whenever focus wanders — wrong when the mailbox window is
    // still on screen (tiled beside another, on a second monitor). So: cover
    // only when the window is genuinely hidden or minimised, and on the way
    // back force a few fresh frames by flipping the view's visibility once and
    // oscillating its width by a pixel. On a bare refocus, oscillate only —
    // no cover. (Same mechanism as FeedArticle / ThreadMessage.)
    property bool _nudge: false
    property bool _webShown: true
    property bool covered: false

    function _repaintCycle() {
        covered = true; _webShown = false
        pulse.count = 0; pulse.restart()
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
    // Window hidden/minimised, or a bare workspace switch (SurfaceWatcher) —
    // cover the editor, then force fresh frames on the way back.
    WebRevive {
        window: Window.window
        onObscured: root.covered = true
        onNeedsRepaint: root._repaintCycle()
    }

    Rectangle {
        anchors.fill: parent
        visible: root.covered
        color: Theme.cardBg
    }
}
