#include "PixelBlockInterceptor.hpp"
#include <QUrl>
#include <QDebug>

PixelBlockInterceptor::PixelBlockInterceptor(QObject *parent)
    : QWebEngineUrlRequestInterceptor(parent)
{
    // Common email tracking and spy pixel domains based on PixelBlock & MailTrackerBlocker lists
    m_trackerDomains = {
        "mailfoogae.appspot.com",
        "mandrillapp.com",
        "sendgrid.net",
        "hubspot.com",
        "hs-analytics.net",
        "hs-scripts.com",
        "mixmax.com",
        "superhuman.com",
        "yesware.com",
        "mailchimp.com",
        "streak.com",
        "bananatag.com",
        "sidekickopen.com",
        "track.customer.io",
        "litmus.com",
        "emailanalytics.com",
        "activecampaign.com",
        "getresponse.com",
        "intercom-mail.com",
        "salesforce.com",
        "pardot.com",
        "marketo.com",
        "campaign-archive.com",
        "list-manage.com"
    };

    m_trackerKeywords = {
        "open.gif",
        "pixel.gif",
        "track.gif",
        "/beacon",
        "/trk",
        "wf.gif",
        "transparent.gif",
        "1x1.gif",
        "pixel.png"
    };
}

void PixelBlockInterceptor::interceptRequest(QWebEngineUrlRequestInfo &info) {
    QUrl url = info.requestUrl();

    // Check if the requested resource is a tracker pixel
    if (isTrackerUrl(url)) {
        info.block(true);
        m_blockedCount++;
        emit blockedCountChanged(m_blockedCount);
        emit trackerDetected(url.toString(), url.host());
        qDebug() << "[PixelBlock] Blocked tracking pixel:" << url.toString();
    }
}

bool PixelBlockInterceptor::isTrackerUrl(const QUrl &url) const {
    QString host = url.host().toLower();
    QString path = url.path().toLower();

    for (const QString &domain : m_trackerDomains) {
        if (host == domain || host.endsWith("." + domain)) {
            // If it's on a known tracking domain and asking for an image/beacon, block it
            return true;
        }
    }

    for (const QString &kw : m_trackerKeywords) {
        if (path.contains(kw)) {
            return true;
        }
    }

    return false;
}
