#pragma once

#include <QObject>
#include <QTimer>
#include <QJsonObject>
#include <QStringList>

class MailboxClient;

class SendManager : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool isSending READ isSending NOTIFY stateChanged)
    Q_PROPERTY(int secondsRemaining READ secondsRemaining NOTIFY countdownChanged)
    Q_PROPERTY(qreal progress READ progress NOTIFY countdownChanged)
    Q_PROPERTY(QString pendingRecipient READ pendingRecipient NOTIFY stateChanged)
    Q_PROPERTY(QString pendingSubject READ pendingSubject NOTIFY stateChanged)
    Q_PROPERTY(bool hasForgottenAttachment READ hasForgottenAttachment NOTIFY stateChanged)

public:
    explicit SendManager(MailboxClient *client, QObject *parent = nullptr);

    bool isSending() const { return m_isSending; }
    int secondsRemaining() const { return m_secondsRemaining; }
    qreal progress() const { return m_progress; }
    QString pendingRecipient() const { return m_pendingRecipient; }
    QString pendingSubject() const { return m_pendingSubject; }
    bool hasForgottenAttachment() const { return m_hasForgottenAttachment; }

    Q_INVOKABLE void scheduleSend(const QString &to, const QString &cc, const QString &bcc,
                                  const QString &subject, const QString &bodyHtml,
                                  const QStringList &attachments);
    Q_INVOKABLE void cancelSend();
    Q_INVOKABLE void forceSendNow();
    Q_INVOKABLE QJsonObject getLastDraft() const;

signals:
    void stateChanged();
    void countdownChanged();
    void sendScheduled(const QString &recipient, const QString &subject, bool attachmentWarning);
    void sendCancelled();
    void sendExecuted();

private slots:
    void onTick();

private:
    bool checkForAttachmentKeywords(const QString &body) const;

    MailboxClient *m_client{nullptr};
    QTimer *m_tickTimer{nullptr};
    bool m_isSending{false};
    int m_totalMs{5000};
    int m_elapsedMs{0};
    int m_secondsRemaining{5};
    qreal m_progress{0.0};

    QString m_pendingTo;
    QString m_pendingCc;
    QString m_pendingBcc;
    QString m_pendingSubject;
    QString m_pendingBodyHtml;
    QStringList m_pendingAttachments;
    QString m_pendingRecipient;
    bool m_hasForgottenAttachment{false};
};
