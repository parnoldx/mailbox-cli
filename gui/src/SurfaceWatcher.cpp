#include "SurfaceWatcher.hpp"

#include <QEvent>
#include <QWindow>

void SurfaceWatcher::attach(QObject *window)
{
    auto *w = qobject_cast<QWindow *>(window);
    if (!w || w == m_win)
        return;
    if (m_win)
        m_win->removeEventFilter(this);
    m_win = w;
    m_win->installEventFilter(this);
    m_exposed = m_win->isExposed();
}

bool SurfaceWatcher::eventFilter(QObject *obj, QEvent *ev)
{
    if (obj == m_win && ev->type() == QEvent::Expose) {
        const bool now = m_win->isExposed();
        if (now != m_exposed) {
            m_exposed = now;
            if (now)
                emit revealed();
            else
                emit obscured();
        }
    }
    return QObject::eventFilter(obj, ev);
}
