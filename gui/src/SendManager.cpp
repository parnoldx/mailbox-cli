#include "SendManager.hpp"
#include "MailboxClient.hpp"
#include <QRegularExpression>
#include <QDebug>

SendManager::SendManager(MailboxClient *client, QObject *parent)
    : QObject(parent),
      m_client(client),
      m_tickTimer(new QTimer(this))
{
    m_tickTimer->setInterval(100);
    connect(m_tickTimer, &QTimer::timeout, this, &SendManager::onTick);
}

void SendManager::scheduleSend(const QString &to, const QString &cc, const QString &bcc,
                               const QString &subject, const QString &bodyHtml,
                               const QStringList &attachments)
{
    m_pendingTo = to;
    m_pendingCc = cc;
    m_pendingBcc = bcc;
    m_pendingSubject = subject;
    m_pendingBodyHtml = bodyHtml;
    m_pendingAttachments = attachments;

    m_pendingRecipient = to.split(',').value(0).trimmed();
    if (m_pendingRecipient.contains('<')) {
        int start = m_pendingRecipient.indexOf('<');
        m_pendingRecipient = m_pendingRecipient.left(start).trimmed();
    }
    if (m_pendingRecipient.isEmpty()) m_pendingRecipient = to;

    m_hasForgottenAttachment = (attachments.isEmpty() && checkForAttachmentKeywords(bodyHtml));

    m_isSending = true;
    m_elapsedMs = 0;
    m_secondsRemaining = 5;
    m_progress = 0.0;

    m_tickTimer->start();

    emit stateChanged();
    emit countdownChanged();
    emit sendScheduled(m_pendingRecipient, m_pendingSubject, m_hasForgottenAttachment);
}

void SendManager::onTick() {
    m_elapsedMs += 100;
    m_progress = static_cast<qreal>(m_elapsedMs) / m_totalMs;
    int remaining = (m_totalMs - m_elapsedMs + 999) / 1000;
    if (remaining != m_secondsRemaining) {
        m_secondsRemaining = remaining;
    }
    emit countdownChanged();

    if (m_elapsedMs >= m_totalMs) {
        m_tickTimer->stop();
        forceSendNow();
    }
}

void SendManager::cancelSend() {
    if (!m_isSending) return;
    m_tickTimer->stop();
    m_isSending = false;
    m_progress = 0.0;
    m_secondsRemaining = 0;

    emit stateChanged();
    emit countdownChanged();
    emit sendCancelled();
}

void SendManager::forceSendNow() {
    if (!m_isSending) return;
    m_tickTimer->stop();
    m_isSending = false;
    m_progress = 1.0;
    m_secondsRemaining = 0;

    if (m_client) {
        m_client->sendMessage(m_pendingTo, m_pendingCc, m_pendingBcc, m_pendingSubject, m_pendingBodyHtml, m_pendingAttachments);
    }

    emit stateChanged();
    emit countdownChanged();
    emit sendExecuted();
}

QJsonObject SendManager::getLastDraft() const {
    QJsonObject d;
    d["to"] = m_pendingTo;
    d["cc"] = m_pendingCc;
    d["bcc"] = m_pendingBcc;
    d["subject"] = m_pendingSubject;
    d["body_html"] = m_pendingBodyHtml;
    d["attachments"] = QJsonArray::fromStringList(m_pendingAttachments);
    return d;
}

bool SendManager::checkForAttachmentKeywords(const QString &body) const {
    // Regex for common English and German attachment keywords
    static const QRegularExpression re(
        "\\b(attached|attachment|attachments|attaching|enclosed|anbei|angeh[aä]ngt|anliegend|beigef[uü]gt)\\b",
        QRegularExpression::CaseInsensitiveOption
    );
    return re.match(body).hasMatch();
}
