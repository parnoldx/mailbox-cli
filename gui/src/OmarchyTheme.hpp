#pragma once

#include <QObject>
#include <QColor>
#include <QString>
#include <QDateTime>

class QFileSystemWatcher;
class QTimer;

// OmarchyTheme is the whole point of this prototype: it mirrors the live Omarchy
// palette into QML properties and re-emits `changed()` the instant the user
// switches themes, so every bound colour in the UI retints in place.
//
// Omarchy publishes the active palette at
//   ~/.local/state/omarchy/current/theme/colors.toml
// and switches themes by `rm -rf`-ing the `theme/` directory and `mv`-ing a new
// one in (see omarchy-theme-set). That destroys any inode-level watch, so we
// watch the stable `current/` directory, re-arm the file watch after every
// reload, and keep a slow mtime poll as a backstop.
class OmarchyTheme : public QObject {
    Q_OBJECT

    // Raw palette, straight from colors.toml.
    Q_PROPERTY(QString name READ name NOTIFY changed)
    Q_PROPERTY(bool dark READ dark NOTIFY changed)
    Q_PROPERTY(QColor background READ background NOTIFY changed)
    Q_PROPERTY(QColor darkBackground READ darkBackground NOTIFY changed)
    Q_PROPERTY(QColor darkerBackground READ darkerBackground NOTIFY changed)
    Q_PROPERTY(QColor lighterBackground READ lighterBackground NOTIFY changed)
    Q_PROPERTY(QColor foreground READ foreground NOTIFY changed)
    Q_PROPERTY(QColor darkForeground READ darkForeground NOTIFY changed)
    Q_PROPERTY(QColor brightForeground READ brightForeground NOTIFY changed)
    Q_PROPERTY(QColor accent READ accent NOTIFY changed)
    Q_PROPERTY(QColor selection READ selection NOTIFY changed)
    Q_PROPERTY(QColor red READ red NOTIFY changed)
    Q_PROPERTY(QColor yellow READ yellow NOTIFY changed)
    Q_PROPERTY(QColor green READ green NOTIFY changed)
    Q_PROPERTY(QColor cyan READ cyan NOTIFY changed)
    Q_PROPERTY(QColor blue READ blue NOTIFY changed)
    Q_PROPERTY(QColor magenta READ magenta NOTIFY changed)
    Q_PROPERTY(QColor orange READ orange NOTIFY changed)
    Q_PROPERTY(QColor brown READ brown NOTIFY changed)

    // Derived design tokens so the QML never hand-mixes colours.
    Q_PROPERTY(QColor windowBg READ windowBg NOTIFY changed)
    Q_PROPERTY(QColor railBg READ railBg NOTIFY changed)
    Q_PROPERTY(QColor cardBg READ cardBg NOTIFY changed)
    Q_PROPERTY(QColor cardHover READ cardHover NOTIFY changed)
    Q_PROPERTY(QColor hairline READ hairline NOTIFY changed)
    Q_PROPERTY(QColor textPrimary READ textPrimary NOTIFY changed)
    Q_PROPERTY(QColor textDim READ textDim NOTIFY changed)
    Q_PROPERTY(QColor onAccent READ onAccent NOTIFY changed)
    Q_PROPERTY(QVariantList avatarPalette READ avatarPalette NOTIFY changed)

    // Constant style vocabulary shared with the rest of Omarchy.
    Q_PROPERTY(QString fontFamily READ fontFamily CONSTANT)
    Q_PROPERTY(int radius READ radius CONSTANT)
    Q_PROPERTY(int radiusSmall READ radiusSmall CONSTANT)
    Q_PROPERTY(int gap READ gap CONSTANT)
    Q_PROPERTY(int pad READ pad CONSTANT)
    Q_PROPERTY(int anim READ anim CONSTANT)

public:
    explicit OmarchyTheme(QObject *parent = nullptr);

    QString name() const { return m_name; }
    bool dark() const { return m_dark; }
    QColor background() const { return m_background; }
    QColor darkBackground() const { return m_darkBackground; }
    QColor darkerBackground() const { return m_darkerBackground; }
    QColor lighterBackground() const { return m_lighterBackground; }
    QColor foreground() const { return m_foreground; }
    QColor darkForeground() const { return m_darkForeground; }
    QColor brightForeground() const { return m_brightForeground; }
    QColor accent() const { return m_accent; }
    QColor selection() const { return m_selection; }
    QColor red() const { return m_red; }
    QColor yellow() const { return m_yellow; }
    QColor green() const { return m_green; }
    QColor cyan() const { return m_cyan; }
    QColor blue() const { return m_blue; }
    QColor magenta() const { return m_magenta; }
    QColor orange() const { return m_orange; }
    QColor brown() const { return m_brown; }

    QColor windowBg() const { return m_background; }
    QColor railBg() const { return m_darkBackground; }
    QColor cardBg() const { return m_lighterBackground; }
    QColor cardHover() const { return mix(m_lighterBackground, m_foreground, 0.10); }
    QColor hairline() const { return mix(m_background, m_foreground, 0.14); }
    QColor textPrimary() const { return m_foreground; }
    QColor textDim() const { return m_darkForeground; }
    QColor onAccent() const;
    QVariantList avatarPalette() const;

    QString fontFamily() const { return "JetBrainsMono Nerd Font"; }
    int radius() const { return 14; }
    int radiusSmall() const { return 8; }
    int gap() const { return 10; }
    int pad() const { return 16; }
    int anim() const { return 220; }

    Q_INVOKABLE void reload();
    // Deterministic avatar colour for a sender string.
    Q_INVOKABLE QColor avatarColor(const QString &key) const;

signals:
    void changed();

private:
    void armWatches();
    bool parse(const QString &path);
    static QColor mix(const QColor &a, const QColor &b, qreal t);

    QString m_colorsPath;
    QString m_namePath;
    QString m_currentDir;
    QFileSystemWatcher *m_watcher{nullptr};
    QTimer *m_poll{nullptr};
    QDateTime m_lastMtime;

    QString m_name{"—"};
    bool m_dark{true};
    QColor m_background{"#1a1b26"};
    QColor m_darkBackground{"#13141c"};
    QColor m_darkerBackground{"#0e0e14"};
    QColor m_lighterBackground{"#24283b"};
    QColor m_foreground{"#a9b1d6"};
    QColor m_darkForeground{"#565f89"};
    QColor m_brightForeground{"#c0caf5"};
    QColor m_accent{"#7aa2f7"};
    QColor m_selection{"#292e42"};
    QColor m_red{"#f7768e"};
    QColor m_yellow{"#e0af68"};
    QColor m_green{"#9ece6a"};
    QColor m_cyan{"#449dab"};
    QColor m_blue{"#7aa2f7"};
    QColor m_magenta{"#ad8ee6"};
    QColor m_orange{"#eb927b"};
    QColor m_brown{"#75493d"};
};
