#pragma once

#include <QObject>
#include <QColor>
#include <QFileSystemWatcher>

class ThemeBridge : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool darkMode READ isDarkMode WRITE setDarkMode NOTIFY darkModeChanged)
    Q_PROPERTY(QColor background READ background NOTIFY colorsChanged)
    Q_PROPERTY(QColor foreground READ foreground NOTIFY colorsChanged)
    Q_PROPERTY(QColor dim READ dim NOTIFY colorsChanged)
    Q_PROPERTY(QColor accent READ accent NOTIFY colorsChanged)
    Q_PROPERTY(QColor accentHover READ accentHover NOTIFY colorsChanged)
    Q_PROPERTY(QColor surface READ surface NOTIFY colorsChanged)
    Q_PROPERTY(QColor surfaceAlt READ surfaceAlt NOTIFY colorsChanged)
    Q_PROPERTY(QColor surfaceHover READ surfaceHover NOTIFY colorsChanged)
    Q_PROPERTY(QColor border READ border NOTIFY colorsChanged)
    Q_PROPERTY(QColor urgent READ urgent NOTIFY colorsChanged)
    Q_PROPERTY(QColor success READ success NOTIFY colorsChanged)
    Q_PROPERTY(QColor warning READ warning NOTIFY colorsChanged)

    Q_PROPERTY(QString fontFamily READ fontFamily CONSTANT)
    Q_PROPERTY(QString monospaceFont READ monospaceFont CONSTANT)
    Q_PROPERTY(int radiusSmall READ radiusSmall CONSTANT)
    Q_PROPERTY(int radiusMedium READ radiusMedium CONSTANT)
    Q_PROPERTY(int radiusLarge READ radiusLarge CONSTANT)
    Q_PROPERTY(int animFast READ animFast CONSTANT)
    Q_PROPERTY(int animNormal READ animNormal CONSTANT)
    Q_PROPERTY(int animSlow READ animSlow CONSTANT)

public:
    explicit ThemeBridge(QObject *parent = nullptr);

    bool isDarkMode() const { return m_darkMode; }
    void setDarkMode(bool dark);

    QColor background() const { return m_background; }
    QColor foreground() const { return m_foreground; }
    QColor dim() const { return m_dim; }
    QColor accent() const { return m_accent; }
    QColor accentHover() const { return m_accentHover; }
    QColor surface() const { return m_surface; }
    QColor surfaceAlt() const { return m_surfaceAlt; }
    QColor surfaceHover() const { return m_surfaceHover; }
    QColor border() const { return m_border; }
    QColor urgent() const { return m_urgent; }
    QColor success() const { return m_success; }
    QColor warning() const { return m_warning; }

    QString fontFamily() const { return "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif"; }
    QString monospaceFont() const { return "'JetBrains Mono', 'Fira Code', monospace"; }

    int radiusSmall() const { return 6; }
    int radiusMedium() const { return 10; }
    int radiusLarge() const { return 16; }

    int animFast() const { return 120; }
    int animNormal() const { return 220; }
    int animSlow() const { return 350; }

    Q_INVOKABLE void reloadTheme();
    Q_INVOKABLE QString generateEmailCss(bool originalStyle = false) const;

signals:
    void darkModeChanged(bool dark);
    void colorsChanged();

private:
    void loadOmarchyTheme();

    bool m_darkMode{true};
    QFileSystemWatcher *m_watcher{nullptr};

    QColor m_background{"#0f1f28"};
    QColor m_foreground{"#FEFDD8"};
    QColor m_dim{"#bfbea2"};
    QColor m_accent{"#667ac0"};
    QColor m_accentHover{"#889cf5"};
    QColor m_surface{"#0b171e"};
    QColor m_surfaceAlt{"#27353e"};
    QColor m_surfaceHover{"#364753"};
    QColor m_border{"#364753"};
    QColor m_urgent{"#f3b691"};
    QColor m_success{"#82dacd"};
    QColor m_warning{"#fff4b0"};
};
