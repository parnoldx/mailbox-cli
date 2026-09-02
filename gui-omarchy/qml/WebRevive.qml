import QtQuick
import QtQuick.Window

// The shared half of every WebEngine host's "QtWebEngine left a black GPU
// surface behind after a Wayland unmap" workaround. It listens to the two
// things that mean the surface went away and came back —
//
//   * the window being hidden or minimised (Window.visibility), and
//   * SurfaceWatcher's expose events, the only hint a bare Hyprland / Sway
//     workspace switch gives —
//
// and calls repaint() once, on the way back, if the surface was actually
// gone in between. The host owns repaint() and everything downstream of it
// (snapshot the page, rebuild the WebEngineView, restore state); it also owns
// its anti-flash cover, which it should raise from onObscured.
//
//   WebRevive {
//       window: Window.window
//       active: root.htmlMode          // gate to when a web view actually exists
//       onObscured: root.cover = true
//       onNeedsRepaint: root.repaintCycle()
//   }
Item {
    id: revive
    visible: false

    // The top-level window to watch. Usually Window.window.
    property var window: null
    // While false, unmap/remap events are ignored (the host has no live web
    // view to rescue).
    property bool active: true

    // The surface just went away — raise your cover now.
    signal obscured()
    // The surface is back after being gone — rebuild the web view.
    signal needsRepaint()

    property bool _dirty: false

    function _gone() {
        if (!revive.active) return
        revive._dirty = true
        revive.obscured()
    }
    function _backIfDirty() {
        if (!revive.active || !revive._dirty) return
        revive._dirty = false
        revive.needsRepaint()
    }

    Connections {
        target: revive.window
        enabled: revive.window !== null
        function onVisibilityChanged() {
            var v = revive.window.visibility
            if (v === Window.Hidden || v === Window.Minimized) revive._gone()
            else revive._backIfDirty()
        }
    }
    Connections {
        target: SurfaceWatcher
        function onObscured() { revive._gone() }
        function onRevealed() { revive._backIfDirty() }
    }
}
