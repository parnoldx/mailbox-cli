#include "PixelBlock.hpp"

#include <QUrl>
#include <QTimer>
#include <QWebEngineProfile>

PixelBlock::PixelBlock(QObject *parent)
    : QWebEngineUrlRequestInterceptor(parent)
{
    // Pure analytics / open-tracking hosts. Kept deliberately tight so that a
    // newsletter's real images and CDN assets still load.
    // Keep in step with internal/trackers: named ESPs plus generic open paths.
    m_hosts = {
        "doubleclick.net", "google-analytics.com", "googletagmanager.com",
        "hs-analytics.net", "mixpanel.com", "segment.io", "segment.com",
        "amplitude.com", "matomo.", "piwik.", "stats.g.doubleclick",
        "email.mg.", "sendgrid.net/wf/open", "list-manage.com/track",
        "mailchimp.com/track", "click.e.", "trk.", "/open.aspx",
        "awstrack.me", "t.hubspotemail.net", "t.hubspotfree.net",
        "track.hubspot.com", "mandrillapp.com/track", "pstmrk.it/open",
        "email.mailgun.net/o/", "track.mailgun.org", "mjt.lu/oo",
        "mailtrack.io/trace", "mltrk.io/pixel", "emltrk.com",
        "go.pardot.com/l/", "exct.net/open.aspx", "t.yesware.com",
        "email.substack.com/o", "r.superhuman.com", "via.intercom.io/o",
        "trk.klclick.com", "linkedin.com/emimp/", "getmailspring.com/open",
        "mailfoogae.appspot.com", "yamm-track.appspot", "polymail.io/track",
        "contactmonkey.com/api/v1/tracker", "openrate.aweber.com",
        "sendibt1.com", "sendibt2.com", "sendibm1.com",
    };
    // Path shapes that only ever mean "someone opened this".
    m_paths = {
        "/wf/open", "/o/eJ", "/e/open", "/ea/open", "/oo/", "/open.php",
        "/track/open", "/trackopen", "/pixel", "/px.gif", "/beacon",
        "/impression", "/piwik", "/matomo", "/collect?", "1x1.png",
        "1x1.gif", "spacer.gif", "blank.gif", "/detectblocker",
        "/email_opened", "/o.gif", "/pixel.gif", "/ut.php",
    };
}

void PixelBlock::arm()
{
    if (m_armed)
        return;
    m_armed = true;
    QWebEngineProfile::defaultProfile()->setUrlRequestInterceptor(this);
}

void PixelBlock::reset()
{
    if (m_blocked == 0)
        return;
    m_blocked = 0;
    emit blockedChanged();
}

void PixelBlock::interceptRequest(QWebEngineUrlRequestInfo &info)
{
    const QUrl url = info.requestUrl();
    const QString scheme = url.scheme();

    // The message's own content and local resources are always fine.
    if (scheme == "data" || scheme == "qrc" || scheme == "file" || scheme == "about")
        return;

    const auto type = info.resourceType();
    const bool subresource =
        type == QWebEngineUrlRequestInfo::ResourceTypeImage ||
        type == QWebEngineUrlRequestInfo::ResourceTypePing ||
        type == QWebEngineUrlRequestInfo::ResourceTypeXhr ||
        type == QWebEngineUrlRequestInfo::ResourceTypeMedia ||
        type == QWebEngineUrlRequestInfo::ResourceTypeScript ||
        type == QWebEngineUrlRequestInfo::ResourceTypeFontResource;

    if (!subresource)
        return;

    const QString host = url.host();
    const QString path = url.path().toLower();
    const QString full = url.toString().toLower();

    bool hit = false;
    for (const QString &h : m_hosts)
        if (host.contains(h) || full.contains(h)) { hit = true; break; }
    if (!hit)
        for (const QString &p : m_paths)
            if (path.contains(p) || full.contains(p)) { hit = true; break; }

    // Anything that is not an image and comes from off-page is almost never
    // something a mail needs to render.
    if (!hit && (type == QWebEngineUrlRequestInfo::ResourceTypePing ||
                 type == QWebEngineUrlRequestInfo::ResourceTypeXhr ||
                 type == QWebEngineUrlRequestInfo::ResourceTypeScript))
        hit = true;

    if (hit) {
        info.block(true);
        ++m_blocked;
        // Coalesce the notify: a page load fires many requests at once.
        QTimer::singleShot(0, this, [this] { emit blockedChanged(); });
    }
}
