#include "MailboxClient.hpp"

#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJSEngine>
#include <QDateTime>
#include <QProcessEnvironment>
#include <QDir>
#include <QStandardPaths>
#include <QTimer>

MailboxClient::MailboxClient(QObject *parent) : QObject(parent) {
    m_sock = new QLocalSocket(this);
    connect(m_sock, &QLocalSocket::connected, this, [this] {
        m_online = true;
        emit onlineChanged();
    });
    connect(m_sock, &QLocalSocket::readyRead, this, &MailboxClient::flushBuffer);
    connect(m_sock, &QLocalSocket::errorOccurred, this, [this](QLocalSocket::LocalSocketError) {
        if (m_online) {
            m_online = false;
            emit onlineChanged();
        }
        m_retry->start();
    });
    connect(m_sock, &QLocalSocket::disconnected, this, [this] {
        m_online = false;
        emit onlineChanged();
        m_retry->start();
    });

    m_retry = new QTimer(this);
    m_retry->setInterval(2500);
    m_retry->setSingleShot(true);
    connect(m_retry, &QTimer::timeout, this, &MailboxClient::connectSocket);

    connectSocket();
}

void MailboxClient::connectSocket() {
    if (m_sock->state() != QLocalSocket::UnconnectedState)
        return;
    QString dir = QProcessEnvironment::systemEnvironment().value("XDG_RUNTIME_DIR", "/tmp");
    m_sock->connectToServer(dir + "/mailbox.sock");
}

void MailboxClient::call(const QStringList &cmd, const QVariantMap &args, const QJSValue &callback) {
    const QString id = QString::number(++m_seq);
    if (callback.isCallable())
        m_pending.insert(id, callback);

    if (!m_online) {
        answerOffline(id, cmd, args);
        return;
    }

    QJsonObject req;
    req["id"] = id;
    req["cmd"] = QJsonArray::fromStringList(cmd);
    if (!args.isEmpty())
        req["args"] = QJsonObject::fromVariantMap(args);
    m_sock->write(QJsonDocument(req).toJson(QJsonDocument::Compact) + '\n');
}

void MailboxClient::flushBuffer() {
    m_buf += m_sock->readAll();
    int nl;
    while ((nl = m_buf.indexOf('\n')) >= 0) {
        const QByteArray line = m_buf.left(nl);
        m_buf.remove(0, nl + 1);
        if (line.trimmed().isEmpty())
            continue;
        const QJsonDocument doc = QJsonDocument::fromJson(line);
        if (doc.isObject())
            dispatch(doc.object().toVariantMap());
    }
}

void MailboxClient::dispatch(const QVariantMap &obj) {
    if (obj.contains("event")) {
        emit pushReceived(obj.value("event").toString(),
                          obj.value("account").toString(),
                          obj.value("box").toString());
        return;
    }

    const QVariantMap mirror = obj.value("mirror").toMap();
    if (!mirror.isEmpty()) {
        m_syncedAt = mirror.value("synced_at").toString();
        m_behind = mirror.value("behind").toBool();
        emit mirrorChanged();
    }

    QVariantMap reply;
    reply["ok"] = obj.value("ok").toBool();
    reply["data"] = obj.value("data");
    reply["error"] = obj.value("error").toString();
    reply["mirror"] = mirror;
    deliver(obj.value("id").toString(), reply);
}

void MailboxClient::deliver(const QString &id, const QVariantMap &reply) {
    auto it = m_pending.find(id);
    if (it == m_pending.end())
        return;
    QJSValue cb = it.value();
    m_pending.erase(it);
    if (cb.isCallable() && m_engine)
        cb.call({m_engine->toScriptValue(reply)});
}

QString MailboxClient::downloadDir() const {
    QString d = QStandardPaths::writableLocation(QStandardPaths::DownloadLocation);
    if (d.isEmpty())
        d = QDir::homePath() + "/Downloads";
    QDir().mkpath(d);
    return d;
}

QString MailboxClient::cacheDir() const {
    QString d = QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (d.isEmpty())
        d = QDir::homePath() + "/.cache/mailbox-omarchy";
    else
        d += "/attachments";
    QDir().mkpath(d);
    return d;
}

// ---- offline demo answers -------------------------------------------------

static QVariantMap msg(const QString &id, const QString &date, const QString &from,
                       const QString &subject, bool seen) {
    return {{"id", id}, {"date", date}, {"from", from}, {"subject", subject}, {"seen", seen}};
}

void MailboxClient::answerOffline(const QString &id, const QStringList &cmd, const QVariantMap &args) {
    const QString verb = cmd.join(' ');
    QVariantMap reply{{"ok", true}, {"error", ""}};
    QVariantMap mirror{{"synced_at", QDateTime::currentDateTimeUtc().toString(Qt::ISODate)},
                       {"behind", false}, {"connected", false}};
    reply["mirror"] = mirror;
    m_syncedAt = mirror.value("synced_at").toString();
    emit mirrorChanged();

    if (verb == "box list") {
        reply["data"] = QVariantList{
            QVariantMap{{"box", "INBOX"}, {"count", 3}, {"unseen", 2}, {"watched", true}},
            QVariantMap{{"box", "Feed"}, {"count", 2}, {"unseen", 0}, {"watched", false}},
            QVariantMap{{"box", "Paper Trail"}, {"count", 7}, {"unseen", 0}, {"watched", false}},
            QVariantMap{{"box", "Screener"}, {"count", 1}, {"unseen", 1}, {"watched", true}},
        };
    } else if (verb == "box view") {
        const QString box = args.value("positional").toString();
        QVariantList d;
        if (box == "Feed") {
            d << msg("Feed:2455", "2026-08-28 21:37", "News Digest <no-reply@example.com>", "NEWSLETTER", true)
              << msg("Feed:2347", "2026-08-12 11:15", "Wellness Letter <newsletter@example.net>", "This week's letter", true);
        } else if (box == "Paper Trail") {
            d << msg("Paper Trail:3250", "2026-02-27 05:21", "Acme Support <no-reply@example.com>", "Registration confirmation", true)
              << msg("Paper Trail:2506", "2026-01-04 20:50", "Online Shop <dsar-request@example.com>", "Your data request", true)
              << msg("Paper Trail:2957", "2025-05-06 07:05", "Widgets Support <support@example.com>", "Re: Question about the manual", true);
        } else if (box == "Screener") {
            d << msg("Screener:1", "2026-08-30 09:12", "Acme Hosting <billing@example.com>", "Your invoice INV-1000 is ready", false);
        } else {
            d << msg("36731", "2026-08-25 06:29", "Max Mustermann <max@example.org>", "Plan", false)
              << msg("36732", "2026-08-24 06:31", "Clinic <no-reply@example.com>", "Your appointment is confirmed", false)
              << msg("36635", "2026-07-22 17:04", "Max Mustermann <max@example.org>", "Recipe card", true);
        }
        reply["data"] = d;
    } else if (verb == "message view") {
        reply["data"] = QVariantMap{
            {"id", args.value("positional")},
            {"from", "Max Mustermann <max@example.org>"},
            {"to", "Max Mustermann <max@example.org>"},
            {"subject", "Plan"},
            {"date", "2026-08-25T06:29:35Z"},
            {"seen", true},
            {"body", "Luck Is Preparation Meeting Opportunity\n\n"
                     "“Luck is what happens when preparation meets opportunity.”\n\n"
                     "The daemon is offline, so this is a canned message. Start it with:\n"
                     "  ./bin/mailbox daemon\n\n"
                     "and the real Mirror shows up here — headers, bodies and all."}};
    } else if (verb == "attachment list") {
        QString mid = args.value("positional").toString();
        if (mid.contains("36635"))
            reply["data"] = QVariantList{QVariantMap{
                {"id", mid + ":1"}, {"index", 1},
                {"filename", "recipe-card.pdf"},
                {"mime_type", "application/pdf"}, {"disposition", "attachment"}, {"size", 38444}}};
        else
            reply["data"] = QVariantList{};
    } else if (verb == "attachment save") {
        reply["ok"] = false;
        reply["error"] = "offline: start the daemon to save attachments";
    } else if (verb == "status") {
        reply["data"] = QVariantList{QVariantMap{{"account", "primary"}, {"count", 3}}};
    } else {
        reply["data"] = QVariantList{};
    }

    QTimer::singleShot(0, this, [this, id, reply] { deliver(id, reply); });
}
