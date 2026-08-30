#pragma once

#include <QObject>
#include <QLocalSocket>
#include <QJSValue>
#include <QHash>
#include <QVariantMap>

class QTimer;
class QJSEngine;

// MailboxClient speaks the daemon's NDJSON line protocol over
// $XDG_RUNTIME_DIR/mailbox.sock. One request per line:
//   {"id":"7","cmd":["box","view"],"args":{"positional":"Inbox","limit":50}}
// One response per line, carrying a `mirror` freshness block on every reply.
//
// If the daemon is not reachable the client answers from a small canned set so
// the prototype still demos. `online` tells the UI which world it is in.
class MailboxClient : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool online READ online NOTIFY onlineChanged)
    Q_PROPERTY(QString syncedAt READ syncedAt NOTIFY mirrorChanged)
    Q_PROPERTY(bool behind READ behind NOTIFY mirrorChanged)

public:
    explicit MailboxClient(QObject *parent = nullptr);
    void setJsEngine(QJSEngine *e) { m_engine = e; }

    bool online() const { return m_online; }
    QString syncedAt() const { return m_syncedAt; }
    bool behind() const { return m_behind; }

    // call(["box","view"], {positional:"Inbox"}, function(reply){ reply.ok, reply.data, ... })
    Q_INVOKABLE void call(const QStringList &cmd, const QVariantMap &args, const QJSValue &callback);

    // Absolute directories the attachment code writes into.
    Q_INVOKABLE QString downloadDir() const;
    Q_INVOKABLE QString cacheDir() const;

signals:
    void onlineChanged();
    void mirrorChanged();
    void pushReceived(const QString &event, const QString &account, const QString &box);

private:
    void connectSocket();
    void flushBuffer();
    void dispatch(const QVariantMap &obj);
    void answerOffline(const QString &id, const QStringList &cmd, const QVariantMap &args);
    void deliver(const QString &id, const QVariantMap &reply);

    QJSEngine *m_engine{nullptr};
    QLocalSocket *m_sock{nullptr};
    QTimer *m_retry{nullptr};
    QByteArray m_buf;
    bool m_online{false};
    QString m_syncedAt;
    bool m_behind{false};
    int m_seq{0};
    QHash<QString, QJSValue> m_pending;
};
