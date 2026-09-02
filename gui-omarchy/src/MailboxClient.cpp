#include "MailboxClient.hpp"

#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QJSEngine>
#include <QDateTime>
#include <QProcessEnvironment>
#include <QDir>
#include <QFile>
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

// ---- tiny persisted key/value store -------------------------------------

static QString statePath() {
    QString d = QStandardPaths::writableLocation(QStandardPaths::AppConfigLocation);
    if (d.isEmpty())
        d = QDir::homePath() + "/.config/Mailbox";
    QDir().mkpath(d);
    return d + "/state.json";
}

void MailboxClient::loadState() {
    if (m_stateLoaded)
        return;
    m_stateLoaded = true;
    QFile f(statePath());
    if (!f.open(QIODevice::ReadOnly))
        return;
    const QJsonDocument doc = QJsonDocument::fromJson(f.readAll());
    if (doc.isObject())
        m_state = doc.object().toVariantMap();
}

void MailboxClient::saveState() const {
    QFile f(statePath());
    if (!f.open(QIODevice::WriteOnly | QIODevice::Truncate))
        return;
    f.write(QJsonDocument(QJsonObject::fromVariantMap(m_state)).toJson(QJsonDocument::Indented));
}

QString MailboxClient::stateGet(const QString &key, const QString &fallback) {
    loadState();
    return m_state.value(key, fallback).toString();
}

void MailboxClient::stateSet(const QString &key, const QString &value) {
    loadState();
    if (m_state.value(key).toString() == value)
        return;
    m_state.insert(key, value);
    saveState();
}
