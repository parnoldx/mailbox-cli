#pragma once

#include <QObject>

class QWindow;
class QEvent;

// QtWebEngine leaves a stale black GPU surface behind after its Wayland surface
// is unmapped and remapped. A Hyprland / Sway workspace switch does exactly that
// — but without ever changing QWindow::visibility(), so the QML views that host
// a WebEngineView can't see it from `Window.visibility` / `Window.active` alone.
//
// The one reliable signal is the platform expose event. SurfaceWatcher filters
// it off the top-level window and re-emits the exposed <-> obscured transitions
// as plain signals; the web-hosting views listen and run their repaint cycle on
// `revealed()`.
class SurfaceWatcher : public QObject {
    Q_OBJECT

public:
    explicit SurfaceWatcher(QObject *parent = nullptr) : QObject(parent) {}

    // Pass the QML Window (an ApplicationWindow is a QWindow).
    Q_INVOKABLE void attach(QObject *window);

signals:
    void obscured();   // the surface stopped being exposed
    void revealed();   // the surface became exposed again

protected:
    bool eventFilter(QObject *obj, QEvent *ev) override;

private:
    QWindow *m_win{nullptr};
    bool m_exposed{true};
};
