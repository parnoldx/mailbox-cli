#pragma once

#include <QWebEngineUrlRequestInterceptor>
#include <QStringList>

// PixelBlock drops the requests an HTML mail makes to phone home: 1x1 beacons,
// open-tracking endpoints and the usual analytics hosts. It never blocks the
// message's own inline data: images, so legitimate mail still renders.
//
// `blocked` counts hits since the last reset so the reader can show a badge.
class PixelBlock : public QWebEngineUrlRequestInterceptor {
    Q_OBJECT
    Q_PROPERTY(int blocked READ blocked NOTIFY blockedChanged)

public:
    explicit PixelBlock(QObject *parent = nullptr);

    int blocked() const { return m_blocked; }
    Q_INVOKABLE void reset();

    // Install this interceptor on the default WebEngine profile. Idempotent and
    // cheap to call again. Deferred to the first time a web view is about to
    // mount, because touching the default profile is what forces the ~half
    // second of Chromium start-up, and the Inbox never needs it.
    Q_INVOKABLE void arm();

    void interceptRequest(QWebEngineUrlRequestInfo &info) override;

signals:
    void blockedChanged();

private:
    bool m_armed{false};
    int m_blocked{0};
    QStringList m_hosts;   // substring match against the request host
    QStringList m_paths;   // substring match against the request path
};
