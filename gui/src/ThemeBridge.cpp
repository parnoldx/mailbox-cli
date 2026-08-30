#include "ThemeBridge.hpp"
#include <QFile>
#include <QTextStream>
#include <QDir>
#include <QStandardPaths>
#include <QRegularExpression>
#include <QDebug>

ThemeBridge::ThemeBridge(QObject *parent)
    : QObject(parent),
      m_watcher(new QFileSystemWatcher(this))
{
    loadOmarchyTheme();

    connect(m_watcher, &QFileSystemWatcher::fileChanged, this, [this](const QString &path) {
        Q_UNUSED(path);
        loadOmarchyTheme();
    });
    connect(m_watcher, &QFileSystemWatcher::directoryChanged, this, [this](const QString &path) {
        Q_UNUSED(path);
        loadOmarchyTheme();
    });
}

void ThemeBridge::setDarkMode(bool dark) {
    if (m_darkMode == dark) return;
    m_darkMode = dark;
    emit darkModeChanged(dark);
    emit colorsChanged();
}

void ThemeBridge::reloadTheme() {
    loadOmarchyTheme();
}

void ThemeBridge::loadOmarchyTheme() {
    QString home = QDir::homePath();
    QString themeDir = home + "/.config/omarchy/themes/om";
    QString colorsPath = themeDir + "/colors.toml";

    if (!QFile::exists(colorsPath)) {
        // Fallback check in themes directory
        QDir td(home + "/.config/omarchy/themes");
        QStringList entries = td.entryList(QDir::Dirs | QDir::NoDotAndDotDot);
        if (!entries.isEmpty()) {
            colorsPath = td.filePath(entries.first() + "/colors.toml");
        }
    }

    if (QFile::exists(colorsPath)) {
        if (!m_watcher->files().contains(colorsPath)) {
            m_watcher->addPath(colorsPath);
        }

        QFile f(colorsPath);
        if (f.open(QIODevice::ReadOnly | QIODevice::Text)) {
            QTextStream in(&f);
            static const QRegularExpression kvRe(R"(^([a-zA-Z0-9_]+)\s*=\s*["']?([^"'\r\n]+)["']?)");

            while (!in.atEnd()) {
                QString line = in.readLine().trimmed();
                if (line.startsWith('#') || line.isEmpty()) continue;

                QRegularExpressionMatch match = kvRe.match(line);
                if (match.hasMatch()) {
                    QString key = match.captured(1).trimmed().toLower();
                    QString val = match.captured(2).trimmed();

                    if (key == "mode") {
                        m_darkMode = (val == "dark");
                    } else if (key == "background") {
                        m_background = QColor(val);
                    } else if (key == "dark_background" || key == "darker_background") {
                        m_surface = QColor(val);
                    } else if (key == "lighter_background") {
                        m_surfaceAlt = QColor(val);
                        m_border = QColor(val).lighter(115);
                        m_surfaceHover = QColor(val).lighter(125);
                    } else if (key == "foreground") {
                        m_foreground = QColor(val);
                    } else if (key == "dark_foreground" || key == "muted") {
                        m_dim = QColor(val);
                    } else if (key == "accent" || key == "blue") {
                        m_accent = QColor(val);
                    } else if (key == "bright_blue") {
                        m_accentHover = QColor(val);
                    } else if (key == "red") {
                        m_urgent = QColor(val);
                    } else if (key == "green" || key == "bright_green") {
                        m_success = QColor(val);
                    } else if (key == "yellow" || key == "orange") {
                        m_warning = QColor(val);
                    }
                }
            }
            f.close();
            emit colorsChanged();
            qDebug() << "[ThemeBridge] Loaded Omarchy theme colors from:" << colorsPath;
            return;
        }
    }

    // Default fallback Catppuccin-like colors if file is not found
    m_background = QColor("#0f1f28");
    m_surface = QColor("#0b171e");
    m_surfaceAlt = QColor("#27353e");
    m_surfaceHover = QColor("#364753");
    m_border = QColor("#364753");
    m_foreground = QColor("#FEFDD8");
    m_dim = QColor("#bfbea2");
    m_accent = QColor("#667ac0");
    m_accentHover = QColor("#889cf5");
    m_urgent = QColor("#f3b691");
    m_success = QColor("#82dacd");
    m_warning = QColor("#fff4b0");
    emit colorsChanged();
}

QString ThemeBridge::generateEmailCss(bool originalStyle) const {
    if (originalStyle) {
        return QString(
            "body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; "
            "margin: 0; padding: 24px; font-size: 15px; line-height: 1.6; }\n"
        );
    }

    QString bg = m_background.name();
    QString fg = m_foreground.name();
    QString acc = m_accent.name();
    QString surf = m_surface.name();
    QString bord = m_border.name();
    QString dimCol = m_dim.name();

    return QString(
        "body { background-color: %1 !important; color: %2 !important; "
        "font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; "
        "margin: 0; padding: 24px; font-size: 15px; line-height: 1.6; }\n"
        "p, div, span, td, th, li, h1, h2, h3, h4, h5, h6 { color: %2 !important; }\n"
        "a { color: %3 !important; text-decoration: underline; }\n"
        "table { border-color: %5 !important; }\n"
        "hr { border: 0; border-top: 1px solid %5; }\n"
        "blockquote { border-left: 3px solid %3; padding-left: 12px; margin-left: 0; color: %6 !important; }\n"
        "pre, code { background: %4 !important; color: %2 !important; padding: 2px 6px; border-radius: 4px; }\n"
        "img { max-width: 100%; height: auto; border-radius: 4px; }\n"
    ).arg(bg, fg, acc, surf, bord, dimCol);
}
