#pragma once

#include <QWebEngineUrlRequestInterceptor>
#include <QObject>
#include <QSet>
#include <QStringList>

class PixelBlockInterceptor : public QWebEngineUrlRequestInterceptor {
    Q_OBJECT
    Q_PROPERTY(int blockedCount READ blockedCount NOTIFY blockedCountChanged)

public:
    explicit PixelBlockInterceptor(QObject *parent = nullptr);

    void interceptRequest(QWebEngineUrlRequestInfo &info) override;

    int blockedCount() const { return m_blockedCount; }
    Q_INVOKABLE void resetCount() {
        m_blockedCount = 0;
        emit blockedCountChanged(0);
    }

signals:
    void blockedCountChanged(int count);
    void trackerDetected(const QString &url, const QString &domain);

private:
    bool isTrackerUrl(const QUrl &url) const;

    int m_blockedCount{0};
    QStringList m_trackerDomains;
    QStringList m_trackerKeywords;
};
