import QtQuick
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
            "<body><lexxy-editor id='ed'></lexxy-editor></body></html>"
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
    function setHtml(html) {
        pendingHtml = html || ""
        if (root.loaded) root._applyPending()
    }
    property string pendingHtml: ""
    function _applyPending() {
        web.runJavaScript("(function(){var e=document.getElementById('ed');if(e)e.value=" +
                          root._js(root.pendingHtml) + ";})()")
    }
    function clear() { setHtml("") }
    function focusEditor() {
        web.runJavaScript("(function(){var e=document.getElementById('ed');if(e)e.focus();})()")
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
        backgroundColor: "transparent"
        onNavigationRequested: function (req) {
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

    // Cover the web view while the window is unmapped so a stale GPU surface
    // never flashes black (the trick ReadingView uses for the mail view).
    property bool covered: false
    Rectangle {
        anchors.fill: parent
        visible: root.covered
        color: Theme.cardBg
    }
    Timer { id: uncover; interval: 60; onTriggered: { web.update(); root.covered = false } }
    Connections {
        target: win
        function onActiveChanged() { if (win.active) uncover.restart(); else root.covered = true }
    }
}
