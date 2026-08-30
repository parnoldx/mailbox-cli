#include "MailboxClient.hpp"
#include <QStandardPaths>
#include <QDir>
#include <QDateTime>
#include <QDebug>

MailboxClient::MailboxClient(QObject *parent)
    : QObject(parent),
      m_socket(new QLocalSocket(this)),
      m_reconnectTimer(new QTimer(this))
{
    connect(m_socket, &QLocalSocket::connected, this, &MailboxClient::onSocketConnected);
    connect(m_socket, &QLocalSocket::disconnected, this, &MailboxClient::onSocketDisconnected);
    connect(m_socket, &QLocalSocket::readyRead, this, &MailboxClient::onSocketReadyRead);
    connect(m_socket, &QLocalSocket::errorOccurred, this, &MailboxClient::onSocketError);

    m_reconnectTimer->setInterval(3000);
    connect(m_reconnectTimer, &QTimer::timeout, this, &MailboxClient::onReconnectTimer);

    reconnect();
}

MailboxClient::~MailboxClient() {
    if (m_socket->isOpen()) {
        m_socket->close();
    }
}

void MailboxClient::reconnect() {
    if (m_socket->state() == QLocalSocket::ConnectedState) return;

    QString runtimeDir = qEnvironmentVariable("XDG_RUNTIME_DIR");
    if (runtimeDir.isEmpty()) {
        runtimeDir = QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
    }
    if (runtimeDir.isEmpty()) {
        runtimeDir = "/tmp";
    }

    QString socketPath = qEnvironmentVariable("MAILBOX_SOCKET");
    if (socketPath.isEmpty()) {
        socketPath = runtimeDir + "/mailbox.sock";
    }

    m_socket->connectToServer(socketPath);
}

void MailboxClient::onReconnectTimer() {
    if (!m_connected) {
        reconnect();
    }
}

void MailboxClient::onSocketConnected() {
    m_connected = true;
    m_useMockFallback = false;
    m_reconnectTimer->stop();
    emit connectionStateChanged(true);
    refreshStatus();
}

void MailboxClient::onSocketDisconnected() {
    m_connected = false;
    emit connectionStateChanged(false);
    m_reconnectTimer->start();
}

void MailboxClient::onSocketError(QLocalSocket::LocalSocketError socketError) {
    Q_UNUSED(socketError);
    if (!m_connected) {
        m_useMockFallback = true;
        m_reconnectTimer->start();
    }
}

void MailboxClient::sendRawCommand(const QString &commandName, const QJsonObject &args, int requestId) {
    if (requestId <= 0) {
        requestId = m_nextReqId++;
    }

    m_pendingCommands[requestId] = commandName;

    QJsonObject req;
    req["id"] = QString::number(requestId);
    req["cmd"] = QJsonArray::fromStringList(commandName.split(' ', Qt::SkipEmptyParts));
    if (!args.isEmpty()) {
        req["args"] = args;
    }

    QByteArray data = QJsonDocument(req).toJson(QJsonDocument::Compact) + "\n";

    if (m_socket->isOpen() && m_connected) {
        m_socket->write(data);
        m_socket->flush();
    } else {
        // Fallback for UI prototype when daemon is not running
        QTimer::singleShot(20, this, [this, commandName, args, requestId]() {
            if (commandName == "status") {
                m_behind = false;
                m_lastSyncedAt = QDateTime::currentDateTime().toString(Qt::ISODate);
                emit mirrorStateChanged(m_behind, m_lastSyncedAt);
            } else if (commandName.startsWith("box view")) {
                QString box = args.value("positional").toString("inbox");
                loadBox(box);
            } else if (commandName == "screener") {
                loadScreener();
            }
        });
    }
}

void MailboxClient::refreshStatus() {
    sendRawCommand("status", QJsonObject{});
}

void MailboxClient::loadBox(const QString &boxName, int limit) {
    QJsonObject args;
    args["positional"] = boxName;
    args["limit"] = limit;
    
    if (m_connected) {
        sendRawCommand("box view", args);
    } else {
        // Mock data generator for interactive prototype
        populateDemoData();
    }
}

void MailboxClient::loadScreener() {
    if (m_connected) {
        sendRawCommand("screener", QJsonObject{});
    } else {
        populateDemoData();
    }
}

void MailboxClient::routeSender(const QString &sender, const QString &destination) {
    QJsonObject args;
    args["sender"] = sender;
    args["to"] = destination;
    sendRawCommand("route set", args);

    if (m_screenerCount > 0) {
        m_screenerCount--;
        emit screenerCountChanged(m_screenerCount);
    }
}

void MailboxClient::markSeen(const QString &messageId, bool seen) {
    QJsonObject args;
    args["positional"] = messageId;
    sendRawCommand(seen ? "seen" : "unseen", args);
}

void MailboxClient::setAside(const QString &messageId) {
    QJsonObject args;
    args["positional"] = messageId;
    sendRawCommand("aside add", args);
}

void MailboxClient::moveToTrash(const QString &messageId) {
    QJsonObject args;
    args["positional"] = messageId;
    sendRawCommand("trash", args);
}

void MailboxClient::sendMessage(const QString &to, const QString &cc, const QString &bcc, const QString &subject, const QString &bodyHtml, const QStringList &attachments) {
    QJsonObject args;
    args["to"] = to;
    if (!cc.isEmpty()) args["cc"] = cc;
    if (!bcc.isEmpty()) args["bcc"] = bcc;
    args["subject"] = subject;
    args["body"] = bodyHtml;
    if (!attachments.isEmpty()) {
        args["attachments"] = QJsonArray::fromStringList(attachments);
    }

    if (m_connected) {
        sendRawCommand("compose", args);
    } else {
        emit messageSentResult(true, "");
    }
}

void MailboxClient::saveDraft(const QString &to, const QString &subject, const QString &bodyHtml) {
    QJsonObject args;
    args["to"] = to;
    args["subject"] = subject;
    args["body"] = bodyHtml;
    sendRawCommand("draft save", args);
}

void MailboxClient::onSocketReadyRead() {
    m_readBuffer.append(m_socket->readAll());
    while (true) {
        int idx = m_readBuffer.indexOf('\n');
        if (idx < 0) break;
        QByteArray line = m_readBuffer.left(idx).trimmed();
        m_readBuffer.remove(0, idx + 1);
        if (!line.isEmpty()) {
            processLine(line);
        }
    }
}

void MailboxClient::processLine(const QByteArray &line) {
    QJsonParseError err;
    QJsonDocument doc = QJsonDocument::fromJson(line, &err);
    if (err.error != QJsonParseError::NoError || !doc.isObject()) return;

    QJsonObject obj = doc.object();

    if (obj.contains("event")) {
        QString evt = obj.value("event").toString();
        if (evt == "mail.changed") {
            emit mailChanged(obj.value("account").toString(), obj.value("box").toString());
        }
        return;
    }

    int id = obj.value("id").toString().toInt();
    bool ok = obj.value("ok").toBool(true);

    if (obj.contains("mirror")) {
        QJsonObject mirror = obj.value("mirror").toObject();
        m_behind = mirror.value("behind").toBool(false);
        m_lastSyncedAt = mirror.value("synced_at").toString();
        emit mirrorStateChanged(m_behind, m_lastSyncedAt);
    }

    QString cmd = m_pendingCommands.take(id);

    if (!ok) {
        qWarning() << "Command" << cmd << "failed:" << obj.value("error").toString();
        return;
    }

    QJsonValue data = obj.value("data");

    if (cmd == "status") {
        if (data.isObject()) {
            QJsonObject st = data.toObject();
            m_unreadCount = st.value("unread").toInt(m_unreadCount);
            m_screenerCount = st.value("screener").toInt(m_screenerCount);
            emit unreadCountChanged(m_unreadCount);
            emit screenerCountChanged(m_screenerCount);
        }
    } else if (cmd.startsWith("box view")) {
        if (data.isArray()) {
            emit boxLoaded("inbox", data.toArray());
        }
    } else if (cmd == "screener") {
        if (data.isArray()) {
            emit screenerLoaded(data.toArray());
        }
    }
}

void MailboxClient::populateDemoData() {
    // Generate realistic demo data tailored to HEY buckets (Imbox new/seen, Feed, Paper Trail, Set Aside)
    QJsonArray inboxMessages;

    // Unread (New for you)
    QJsonObject m1;
    m1["id"] = "101";
    m1["subject"] = "Project Architecture & QtQuick Roadmap";
    m1["from_name"] = "Max Mustermann";
    m1["from_email"] = "max@example.org";
    m1["to_name"] = "Max";
    m1["to_email"] = "you@example.com";
    m1["date"] = "Today, 14:32";
    m1["seen"] = false;
    m1["snippet"] = "Preview text.";
    m1["body_html"] = "<p>Example body.</p>";
    m1["has_attachments"] = true;
    m1["attachments_count"] = 2;
    m1["bucket"] = "inbox";
    inboxMessages.append(m1);

    QJsonObject m2;
    m2["id"] = "102";
    m2["subject"] = "Meeting Notes: Omarchy Theme Sync & Palette";
    m2["from_name"] = "Sam Rivers";
    m2["from_email"] = "sarah.connor@example.com";
    m2["to_name"] = "Max";
    m2["to_email"] = "you@example.com";
    m2["date"] = "Today, 11:15";
    m2["seen"] = false;
    m2["snippet"] = "Preview text.";
    m2["body_html"] = "<p>Example body.</p>";
    m2["has_attachments"] = false;
    m2["attachments_count"] = 0;
    m2["bucket"] = "inbox";
    inboxMessages.append(m2);

    // Read (Previously seen)
    QJsonObject m3;
    m3["id"] = "103";
    m3["subject"] = "Weekly Engineering Sync & Milestones";
    m3["from_name"] = "Lee Turner";
    m3["from_email"] = "linus@example.org";
    m3["to_name"] = "Max";
    m3["to_email"] = "you@example.com";
    m3["date"] = "Yesterday";
    m3["seen"] = true;
    m3["snippet"] = "Preview text.";
    m3["body_html"] = "<p>Example body.</p>";
    m3["has_attachments"] = false;
    m3["bucket"] = "inbox";
    inboxMessages.append(m3);

    QJsonObject m4;
    m4["id"] = "104";
    m4["subject"] = "Release candidate tag v2.4.0";
    m4["from_name"] = "Alex Morgan";
    m4["from_email"] = "alex@example.org";
    m4["to_name"] = "Max";
    m4["to_email"] = "you@example.com";
    m4["date"] = "Aug 27";
    m4["seen"] = true;
    m4["snippet"] = "Preview text.";
    m4["body_html"] = "<p>Example body.</p>";
    m4["has_attachments"] = false;
    m4["bucket"] = "inbox";
    inboxMessages.append(m4);

    emit boxLoaded("inbox", inboxMessages);

    // Screener demo senders
    QJsonArray screenerList;
    QJsonObject s1;
    s1["sender"] = "newsletter@example.com";
    s1["name"] = "Tech Daily";
    s1["subject"] = "Industry news";
    s1["date"] = "Today, 13:00";
    s1["snippet"] = "Preview text.";
    screenerList.append(s1);

    QJsonObject s2;
    s2["sender"] = "billing@example.com";
    s2["name"] = "Acme Hosting Inc";
    s2["subject"] = "Your Invoice #INV-1000 is ready";
    s2["date"] = "Today, 09:30";
    s2["snippet"] = "Preview text.";
    screenerList.append(s2);

    QJsonObject s3;
    s3["sender"] = "promotions@example.net";
    s3["name"] = "Promo Mailer";
    s3["subject"] = "Claim your exclusive voucher now!";
    s3["date"] = "Aug 29";
    s3["snippet"] = "Preview text.";
    screenerList.append(s3);

    emit screenerLoaded(screenerList);
}
