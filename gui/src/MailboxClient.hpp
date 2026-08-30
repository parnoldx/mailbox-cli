#pragma once

#include <QObject>
#include <QLocalSocket>
#include <QJsonObject>
#include <QJsonArray>
#include <QJsonDocument>
#include <QMap>
#include <QTimer>
#include <functional>

class MailboxClient : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool connected READ isConnected NOTIFY connectionStateChanged)
    Q_PROPERTY(bool behind READ isBehind NOTIFY mirrorStateChanged)
    Q_PROPERTY(QString lastSyncedAt READ lastSyncedAt NOTIFY mirrorStateChanged)
    Q_PROPERTY(int screenerCount READ screenerCount NOTIFY screenerCountChanged)
    Q_PROPERTY(int unreadCount READ unreadCount NOTIFY unreadCountChanged)

public:
    explicit MailboxClient(QObject *parent = nullptr);
    ~MailboxClient() override;

    bool isConnected() const { return m_connected; }
    bool isBehind() const { return m_behind; }
    QString lastSyncedAt() const { return m_lastSyncedAt; }
    int screenerCount() const { return m_screenerCount; }
    int unreadCount() const { return m_unreadCount; }

    Q_INVOKABLE void reconnect();
    Q_INVOKABLE void sendRawCommand(const QString &commandName, const QJsonObject &args, int requestId = 0);
    Q_INVOKABLE void refreshStatus();
    Q_INVOKABLE void loadBox(const QString &boxName, int limit = 50);
    Q_INVOKABLE void loadScreener();
    Q_INVOKABLE void routeSender(const QString &sender, const QString &destination); // "inbox", "feed", "paper", "block"
    Q_INVOKABLE void markSeen(const QString &messageId, bool seen);
    Q_INVOKABLE void setAside(const QString &messageId);
    Q_INVOKABLE void moveToTrash(const QString &messageId);
    Q_INVOKABLE void sendMessage(const QString &to, const QString &cc, const QString &bcc, const QString &subject, const QString &bodyHtml, const QStringList &attachments);
    Q_INVOKABLE void saveDraft(const QString &to, const QString &subject, const QString &bodyHtml);

signals:
    void connectionStateChanged(bool connected);
    void mirrorStateChanged(bool behind, const QString &syncedAt);
    void screenerCountChanged(int count);
    void unreadCountChanged(int count);
    void boxLoaded(const QString &boxName, const QJsonArray &messages);
    void screenerLoaded(const QJsonArray &senders);
    void searchResultsReady(const QString &query, const QJsonArray &messages);
    void contactsReady(const QString &query, const QJsonArray &contacts);
    void mailChanged(const QString &account, const QString &box);
    void messageSentResult(bool success, const QString &errorMsg);

private slots:
    void onSocketConnected();
    void onSocketDisconnected();
    void onSocketReadyRead();
    void onSocketError(QLocalSocket::LocalSocketError socketError);
    void onReconnectTimer();

private:
    void processLine(const QByteArray &line);
    void populateDemoData();

    QLocalSocket *m_socket{nullptr};
    QTimer *m_reconnectTimer{nullptr};
    QByteArray m_readBuffer;
    bool m_connected{false};
    bool m_behind{false};
    bool m_useMockFallback{false};
    QString m_lastSyncedAt;
    int m_screenerCount{3};
    int m_unreadCount{4};
    int m_nextReqId{1};

    QMap<int, QString> m_pendingCommands;
};
