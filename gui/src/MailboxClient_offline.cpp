#include "MailboxClient.hpp"

#include <QDateTime>
#include <QHash>
#include <QTimer>
#include <QVariantList>

// The canned answers the client falls back to when the daemon socket is
// down, so the prototype still demos. Split out of MailboxClient.cpp to keep
// the transport itself small; it is still one class.

static QVariantMap msg(const QString &id, const QString &date, const QString &from,
                       const QString &subject, bool seen) {
    return {{"id", id}, {"date", date}, {"from", from}, {"subject", subject}, {"seen", seen}};
}

void MailboxClient::answerOffline(const QString &id, const QStringList &cmd, const QVariantMap &args) {
    const QString verb = cmd.join(' ');
    QVariantMap reply{{"ok", true}, {"error", ""}};
    QVariantMap mirror{{"synced_at", QDateTime::currentDateTimeUtc().toString(Qt::ISODate)},
                       {"behind", false}, {"connected", false}};
    reply["mirror"] = mirror;
    m_syncedAt = mirror.value("synced_at").toString();
    emit mirrorChanged();

    if (verb == "box list") {
        reply["data"] = QVariantList{
            QVariantMap{{"box", "INBOX"}, {"count", 3}, {"unseen", 2}, {"watched", true}},
            QVariantMap{{"box", "Feed"}, {"count", 2}, {"unseen", 0}, {"watched", false}},
            QVariantMap{{"box", "Paper Trail"}, {"count", 7}, {"unseen", 0}, {"watched", false}},
            QVariantMap{{"box", "Screener"}, {"count", 1}, {"unseen", 1}, {"watched", true}},
        };
    } else if (verb == "box view") {
        const QString box = args.value("positional").toString();
        QVariantList d;
        if (box == "Feed") {
            d << msg("Feed:2455", "2026-08-29 21:37", "News Digest <no-reply@example.com>", "This week in tech", true)
              << msg("Feed:2453", "2026-08-29 08:02", "Analysis Weekly <feed@example.com>", "A weekly roundup", true)
              << msg("Feed:2447", "2026-08-27 17:44", "Dev Digest <mail@example.com>", "Issue #42: this week", true)
              << msg("Feed:2431", "2026-08-24 06:10", "Wellness Letter <newsletter@example.net>", "This week's letter", true)
              << msg("Feed:2402", "2026-08-19 12:15", "Tech Podcast <podcast@example.com>", "A weekly episode", true)
              << msg("Feed:2347", "2026-08-12 11:15", "Tech News <newsletter@example.com>", "Weekly roundup", true);
        } else if (box == "Paper Trail") {
            d << msg("Paper Trail:3250", "2026-02-27 05:21", "Acme Support <no-reply@example.com>", "Registration confirmation", true)
              << msg("Paper Trail:2506", "2026-01-04 20:50", "Online Shop <dsar-request@example.com>", "Your data request", true)
              << msg("Paper Trail:2957", "2025-05-06 07:05", "Widgets Support <support@example.com>", "Re: Question about the manual", true);
        } else if (box == "Screener") {
            d << msg("Screener:1", "2026-08-30 09:12", "Acme Hosting <billing@example.com>", "Your invoice INV-1000 is ready", false);
        } else if (box == "Aside") {
            d << msg("Aside:1", "2026-08-28 14:03", "Max Mustermann <max@example.org>", "Uhu", true)
              << msg("Aside:2", "2026-08-26 09:41", "Widgets Support <support@example.com>", "Re: Question about the manual", true);
        } else if (box == "Reply Later") {
            d << msg("Reply Later:1", "2026-08-29 18:22", "Max Mustermann <max@example.org>", "Hi", false)
              << msg("Reply Later:2", "2026-08-27 11:07", "Clinic <no-reply@example.com>", "Your appointment is confirmed", true);
        } else {
            d << msg("36731", "2026-08-25 06:29", "Max Mustermann <max@example.org>", "Plan", false)
              << msg("36732", "2026-08-24 06:31", "Clinic <no-reply@example.com>", "Your appointment is confirmed", false)
              << msg("36635", "2026-07-22 17:04", "Max Mustermann <max@example.org>", "Recipe card", true);
        }
        reply["data"] = d;
    } else if (verb == "message view") {
        const QString mid = args.value("positional").toString();
        // Give each Feed item its own body so the offline demo shows real
        // previews and a real expand, not the same paragraph six times.
        static const QHash<QString, QString> feedBodies{};
        const QString body = feedBodies.value(
            mid,
            "The daemon is offline, so this is a canned message. Start it with:\n"
            "  ./bin/mailbox daemon\n\n"
            "and the real Mirror shows up here — headers, bodies and all.");
        reply["data"] = QVariantMap{
            {"id", mid},
            {"from", "Newsletter <feed@example.com>"},
            {"to", "Max Mustermann <max@example.org>"},
            {"subject", "(demo)"},
            {"date", "2026-08-25T06:29:35Z"},
            {"seen", true},
            {"body_format", "plain"},
            {"body", body}};
    } else if (verb == "attachment list") {
        QString mid = args.value("positional").toString();
        if (mid.contains("36635"))
            reply["data"] = QVariantList{QVariantMap{
                {"id", mid + ":1"}, {"index", 1},
                {"filename", "recipe-card.pdf"},
                {"mime_type", "application/pdf"}, {"disposition", "attachment"}, {"size", 38444}}};
        else
            reply["data"] = QVariantList{};
    } else if (verb == "attachment save") {
        reply["ok"] = false;
        reply["error"] = "offline: start the daemon to save attachments";
    } else if (verb == "attachment bytes") {
        reply["ok"] = false;
        reply["error"] = "offline: start the daemon to load inline images";
    } else if (verb == "status") {
        reply["data"] = QVariantList{QVariantMap{{"account", "primary"}, {"count", 3}}};
    } else {
        reply["data"] = QVariantList{};
    }

    QTimer::singleShot(0, this, [this, id, reply] { deliver(id, reply); });
}
