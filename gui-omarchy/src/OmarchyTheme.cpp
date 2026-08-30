#include "OmarchyTheme.hpp"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QFileSystemWatcher>
#include <QRegularExpression>
#include <QTextStream>
#include <QTimer>

OmarchyTheme::OmarchyTheme(QObject *parent) : QObject(parent) {
    const QString home = QDir::homePath();
    m_currentDir = home + "/.local/state/omarchy/current";
    m_colorsPath = m_currentDir + "/theme/colors.toml";
    m_namePath = m_currentDir + "/theme.name";

    m_watcher = new QFileSystemWatcher(this);
    const auto onNoise = [this](const QString &) {
        // Debounce: a theme swap touches several paths in quick succession.
        QTimer::singleShot(60, this, [this] { reload(); });
    };
    connect(m_watcher, &QFileSystemWatcher::fileChanged, this, onNoise);
    connect(m_watcher, &QFileSystemWatcher::directoryChanged, this, onNoise);

    // Backstop: the watch can be lost when `theme/` is replaced wholesale.
    m_poll = new QTimer(this);
    m_poll->setInterval(2000);
    connect(m_poll, &QTimer::timeout, this, [this] {
        const QDateTime mt = QFileInfo(m_colorsPath).lastModified();
        if (mt.isValid() && mt != m_lastMtime)
            reload();
    });
    m_poll->start();

    reload();
}

void OmarchyTheme::armWatches() {
    const QStringList want{m_currentDir, m_currentDir + "/theme", m_colorsPath, m_namePath};
    for (const QString &p : want) {
        if (!QFile::exists(p))
            continue;
        if (!m_watcher->files().contains(p) && !m_watcher->directories().contains(p))
            m_watcher->addPath(p);
    }
}

void OmarchyTheme::reload() {
    armWatches();

    QFile nf(m_namePath);
    if (nf.open(QIODevice::ReadOnly | QIODevice::Text)) {
        QString n = QString::fromUtf8(nf.readAll()).trimmed();
        n.replace('-', ' ');
        QStringList parts = n.split(' ', Qt::SkipEmptyParts);
        for (QString &w : parts)
            if (!w.isEmpty()) w[0] = w[0].toUpper();
        m_name = parts.join(' ');
    }

    m_lastMtime = QFileInfo(m_colorsPath).lastModified();
    parse(m_colorsPath);
    emit changed();
}

bool OmarchyTheme::parse(const QString &path) {
    QFile f(path);
    if (!f.open(QIODevice::ReadOnly | QIODevice::Text))
        return false;

    static const QRegularExpression re(
        R"(^\s*([a-zA-Z_]+)\s*=\s*["']?([#0-9a-zA-Z]+)["']?\s*$)");

    QTextStream in(&f);
    while (!in.atEnd()) {
        const QString line = in.readLine();
        const auto m = re.match(line);
        if (!m.hasMatch())
            continue;
        const QString k = m.captured(1).toLower();
        const QString v = m.captured(2);

        if (k == "mode") m_dark = (v.toLower() != "light");
        else if (k == "background") m_background = QColor(v);
        else if (k == "dark_background") m_darkBackground = QColor(v);
        else if (k == "darker_background") m_darkerBackground = QColor(v);
        else if (k == "lighter_background") m_lighterBackground = QColor(v);
        else if (k == "foreground") m_foreground = QColor(v);
        else if (k == "dark_foreground") m_darkForeground = QColor(v);
        else if (k == "bright_foreground") m_brightForeground = QColor(v);
        else if (k == "accent") m_accent = QColor(v);
        else if (k == "selection") m_selection = QColor(v);
        else if (k == "red") m_red = QColor(v);
        else if (k == "yellow") m_yellow = QColor(v);
        else if (k == "green") m_green = QColor(v);
        else if (k == "cyan") m_cyan = QColor(v);
        else if (k == "blue") m_blue = QColor(v);
        else if (k == "magenta") m_magenta = QColor(v);
        else if (k == "orange") m_orange = QColor(v);
        else if (k == "brown") m_brown = QColor(v);
    }
    return true;
}

QColor OmarchyTheme::mix(const QColor &a, const QColor &b, qreal t) {
    return QColor::fromRgbF(a.redF() + (b.redF() - a.redF()) * t,
                            a.greenF() + (b.greenF() - a.greenF()) * t,
                            a.blueF() + (b.blueF() - a.blueF()) * t);
}

QColor OmarchyTheme::onAccent() const {
    // Pick whichever of the deep/bright ends of the palette reads on the accent.
    const double lum = 0.299 * m_accent.redF() + 0.587 * m_accent.greenF() + 0.114 * m_accent.blueF();
    return lum > 0.55 ? m_darkerBackground : m_brightForeground;
}

QVariantList OmarchyTheme::avatarPalette() const {
    return {m_red, m_yellow, m_green, m_cyan, m_blue, m_magenta, m_orange, m_brown};
}

QColor OmarchyTheme::avatarColor(const QString &key) const {
    const QVariantList p = avatarPalette();
    if (p.isEmpty())
        return m_accent;
    uint h = 2166136261u;
    for (const QChar c : key) {
        h ^= c.unicode();
        h *= 16777619u;
    }
    return p.at(h % p.size()).value<QColor>();
}
