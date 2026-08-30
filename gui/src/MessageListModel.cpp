#include "MessageListModel.hpp"
#include <QCryptographicHash>
#include <QDebug>

MessageListModel::MessageListModel(QObject *parent)
    : QAbstractListModel(parent)
{
    populateBucketData("inbox");
}

int MessageListModel::rowCount(const QModelIndex &parent) const {
    if (parent.isValid()) return 0;
    return m_filteredItems.size();
}

QVariant MessageListModel::data(const QModelIndex &index, int role) const {
    if (!index.isValid() || index.row() < 0 || index.row() >= m_filteredItems.size()) {
        return QVariant();
    }

    const MessageItem &item = m_filteredItems.at(index.row());
    switch (role) {
        case IdRole: return item.id;
        case SubjectRole: return item.subject;
        case FromNameRole: return item.fromName;
        case FromEmailRole: return item.fromEmail;
        case ToNameRole: return item.toName;
        case ToEmailRole: return item.toEmail;
        case DateRole: return item.date;
        case SnippetRole: return item.snippet;
        case BodyHtmlRole: return item.bodyHtml;
        case SeenRole: return item.seen;
        case AsideRole: return item.aside;
        case PaperTrailRole: return item.paperTrail;
        case FeedRole: return item.feed;
        case HasAttachmentsRole: return item.hasAttachments;
        case AttachmentsCountRole: return item.attachmentsCount;
        case BucketRole: return item.bucket;
        case AvatarColorRole: return item.avatarColor;
        case InitialsRole: return item.initials;
        default: return QVariant();
    }
}

QHash<int, QByteArray> MessageListModel::roleNames() const {
    QHash<int, QByteArray> roles;
    roles[IdRole] = "id";
    roles[SubjectRole] = "subject";
    roles[FromNameRole] = "fromName";
    roles[FromEmailRole] = "fromEmail";
    roles[ToNameRole] = "toName";
    roles[ToEmailRole] = "toEmail";
    roles[DateRole] = "date";
    roles[SnippetRole] = "snippet";
    roles[BodyHtmlRole] = "bodyHtml";
    roles[SeenRole] = "seen";
    roles[AsideRole] = "aside";
    roles[PaperTrailRole] = "paperTrail";
    roles[FeedRole] = "feed";
    roles[HasAttachmentsRole] = "hasAttachments";
    roles[AttachmentsCountRole] = "attachmentsCount";
    roles[BucketRole] = "bucket";
    roles[AvatarColorRole] = "avatarColor";
    roles[InitialsRole] = "initials";
    return roles;
}

void MessageListModel::setCurrentBucket(const QString &bucket) {
    if (m_currentBucket == bucket) return;
    m_currentBucket = bucket;
    populateBucketData(bucket);
    emit currentBucketChanged();
}

void MessageListModel::setSearchQuery(const QString &query) {
    if (m_searchQuery == query) return;
    m_searchQuery = query;
    rebuildFilteredList();
    emit searchQueryChanged();
}

void MessageListModel::loadFromMessages(const QJsonArray &messages) {
    beginResetModel();
    m_allItems.clear();

    for (const QJsonValue &v : messages) {
        QJsonObject obj = v.toObject();
        MessageItem item;
        item.id = obj.value("id").toString();
        item.subject = obj.value("subject").toString();
        item.fromName = obj.value("from_name").toString();
        item.fromEmail = obj.value("from_email").toString();
        item.toName = obj.value("to_name").toString("Peter");
        item.toEmail = obj.value("to_email").toString("you@example.com");
        item.date = obj.value("date").toString();
        item.snippet = obj.value("snippet").toString();
        item.bodyHtml = obj.value("body_html").toString();
        item.seen = obj.value("seen").toBool(false);
        item.aside = obj.value("aside").toBool(false);
        item.paperTrail = obj.value("paper_trail").toBool(false);
        item.feed = obj.value("feed").toBool(false);
        item.hasAttachments = obj.value("has_attachments").toBool(false);
        item.attachmentsCount = obj.value("attachments_count").toInt(0);
        item.bucket = obj.value("bucket").toString("inbox");
        item.initials = computeInitials(item.fromName, item.fromEmail);
        item.avatarColor = computeAvatarColor(item.fromEmail);
        m_allItems.append(item);
    }

    rebuildFilteredList();
    endResetModel();
    emit countChanged();
}

void MessageListModel::populateBucketData(const QString &bucket) {
    beginResetModel();
    m_allItems.clear();

    if (bucket == "inbox") {
        // Unread
        MessageItem m1;
        m1.id = "101";
        m1.subject = "Project Architecture & QtQuick Roadmap";
        m1.fromName = "Max Mustermann";
        m1.fromEmail = "max@example.org";
        m1.toName = "Peter";
        m1.toEmail = "you@example.com";
        m1.date = "Today, 14:32";
        m1.snippet = "Preview text.";
        m1.bodyHtml = "<p>Example body.</p>";
        m1.seen = false;
        m1.hasAttachments = true;
        m1.attachmentsCount = 2;
        m1.bucket = "inbox";
        m1.initials = computeInitials(m1.fromName, m1.fromEmail);
        m1.avatarColor = computeAvatarColor(m1.fromEmail);
        m_allItems.append(m1);

        MessageItem m2;
        m2.id = "102";
        m2.subject = "Meeting Notes: Omarchy Theme Sync & Palette";
        m2.fromName = "Sam Rivers";
        m2.fromEmail = "sarah.connor@example.com";
        m2.toName = "Peter";
        m2.toEmail = "you@example.com";
        m2.date = "Today, 11:15";
        m2.snippet = "Preview text.";
        m2.bodyHtml = "<p>Example body.</p>";
        m2.seen = false;
        m2.hasAttachments = false;
        m2.bucket = "inbox";
        m2.initials = computeInitials(m2.fromName, m2.fromEmail);
        m2.avatarColor = computeAvatarColor(m2.fromEmail);
        m_allItems.append(m2);

        // Previously seen
        MessageItem m3;
        m3.id = "103";
        m3.subject = "Weekly Engineering Sync & Milestones";
        m3.fromName = "Lee Turner";
        m3.fromEmail = "linus@example.org";
        m3.toName = "Peter";
        m3.toEmail = "you@example.com";
        m3.date = "Yesterday";
        m3.snippet = "Preview text.";
        m3.bodyHtml = "<p>Example body.</p>";
        m3.seen = true;
        m3.bucket = "inbox";
        m3.initials = computeInitials(m3.fromName, m3.fromEmail);
        m3.avatarColor = computeAvatarColor(m3.fromEmail);
        m_allItems.append(m3);

        MessageItem m4;
        m4.id = "104";
        m4.subject = "Release candidate tag v2.4.0";
        m4.fromName = "Alex Morgan";
        m4.fromEmail = "alex@example.org";
        m4.toName = "Peter";
        m4.toEmail = "you@example.com";
        m4.date = "Aug 27";
        m4.snippet = "Preview text.";
        m4.bodyHtml = "<p>Example body.</p>";
        m4.seen = true;
        m4.bucket = "inbox";
        m4.initials = computeInitials(m4.fromName, m4.fromEmail);
        m4.avatarColor = computeAvatarColor(m4.fromEmail);
        m_allItems.append(m4);
    } else if (bucket == "feed") {
        // RSS / Magazine style Feed
        MessageItem f1;
        f1.id = "201";
        f1.subject = "BYTEBYTEGO: How WhatsApp Scaled to 2 Billion Users";
        f1.fromName = "Alex Xu";
        f1.fromEmail = "newsletter@bytebytego.com";
        f1.toName = "Peter";
        f1.toEmail = "you@example.com";
        f1.date = "Today, 08:00";
        f1.snippet = "Preview text.";
        f1.bodyHtml = "<p>Example body.</p>";
        f1.seen = true;
        f1.feed = true;
        f1.bucket = "feed";
        f1.initials = "BB";
        f1.avatarColor = "#61AFEF";
        m_allItems.append(f1);

        MessageItem f2;
        f2.id = "202";
        f2.subject = "Dev Digest #742: Modern C++23 in Production";
        f2.fromName = "Kale Davis";
        f2.fromEmail = "curator@example.com";
        f2.toName = "Peter";
        f2.toEmail = "you@example.com";
        f2.date = "Aug 29";
        f2.snippet = "Preview text.";
        f2.bodyHtml = "<p>Example body.</p>";
        f2.seen = true;
        f2.feed = true;
        f2.bucket = "feed";
        f2.initials = "HN";
        f2.avatarColor = "#E5C07B";
        m_allItems.append(f2);
    } else if (bucket == "paper") {
        // Paper Trail: Receipts, confirmations, invoices
        MessageItem p1;
        p1.id = "301";
        p1.subject = "Invoice #INV-1000 for hosting plan SRV-1";
        p1.fromName = "Acme Hosting";
        p1.fromEmail = "billing@example.com";
        p1.toName = "Peter";
        p1.toEmail = "you@example.com";
        p1.date = "Today, 06:14";
        p1.snippet = "Preview text.";
        p1.bodyHtml = "<p>Example body.</p>";
        p1.seen = true;
        p1.paperTrail = true;
        p1.hasAttachments = true;
        p1.attachmentsCount = 1;
        p1.bucket = "paper";
        p1.initials = "HO";
        p1.avatarColor = "#E06C75";
        m_allItems.append(p1);

        MessageItem p2;
        p2.id = "302";
        p2.subject = "Flight Confirmation: BER → MUC (LH 2041)";
        p2.fromName = "Lufthansa";
        p2.fromEmail = "booking@lufthansa.com";
        p2.toName = "Peter";
        p2.toEmail = "you@example.com";
        p2.date = "Aug 26";
        p2.snippet = "Preview text.";
        p2.bodyHtml = "<p>Example body.</p>";
        p2.seen = true;
        p2.paperTrail = true;
        p2.hasAttachments = true;
        p2.attachmentsCount = 1;
        p2.bucket = "paper";
        p2.initials = "LH";
        p2.avatarColor = "#98C379";
        m_allItems.append(p2);
    } else if (bucket == "reply_later") {
        MessageItem r1;
        r1.id = "401";
        r1.subject = "Question regarding CardDAV sync interval";
        r1.fromName = "Dana Hart";
        r1.fromEmail = "dana@example.com";
        r1.toName = "Peter";
        r1.toEmail = "you@example.com";
        r1.date = "Aug 28";
        r1.snippet = "Preview text.";
        r1.bodyHtml = "<p>Example body.</p>";
        r1.seen = false;
        r1.bucket = "reply_later";
        r1.initials = "DH";
        r1.avatarColor = "#C678DD";
        m_allItems.append(r1);
    } else if (bucket == "aside") {
        MessageItem a1;
        a1.id = "501";
        a1.subject = "Long Read: The Evolution of Unix Mail Architecture";
        a1.fromName = "Dana Berg";
        a1.fromEmail = "dana@example.com";
        a1.toName = "Peter";
        a1.toEmail = "you@example.com";
        a1.date = "Aug 25";
        a1.snippet = "Preview text.";
        a1.bodyHtml = "<p>Example body.</p>";
        a1.seen = false;
        a1.aside = true;
        a1.bucket = "aside";
        a1.initials = "DJ";
        a1.avatarColor = "#56B6C2";
        m_allItems.append(a1);
    }

    rebuildFilteredList();
    endResetModel();
    emit countChanged();
}

void MessageListModel::rebuildFilteredList() {
    m_filteredItems.clear();
    for (const MessageItem &item : m_allItems) {
        if (!m_searchQuery.isEmpty()) {
            bool matches = item.subject.contains(m_searchQuery, Qt::CaseInsensitive)
                        || item.fromName.contains(m_searchQuery, Qt::CaseInsensitive)
                        || item.fromEmail.contains(m_searchQuery, Qt::CaseInsensitive)
                        || item.snippet.contains(m_searchQuery, Qt::CaseInsensitive);
            if (!matches) continue;
        }
        m_filteredItems.append(item);
    }
}

void MessageListModel::setSeen(int index, bool seen) {
    if (index < 0 || index >= m_filteredItems.size()) return;
    m_filteredItems[index].seen = seen;
    QModelIndex idx = createIndex(index, 0);
    emit dataChanged(idx, idx, {SeenRole});
}

void MessageListModel::setAside(int index) {
    if (index < 0 || index >= m_filteredItems.size()) return;
    beginRemoveRows(QModelIndex(), index, index);
    m_filteredItems.removeAt(index);
    endRemoveRows();
    emit countChanged();
}

void MessageListModel::removeMessage(int index) {
    if (index < 0 || index >= m_filteredItems.size()) return;
    beginRemoveRows(QModelIndex(), index, index);
    m_filteredItems.removeAt(index);
    endRemoveRows();
    emit countChanged();
}

QJsonObject MessageListModel::getMessage(int index) const {
    if (index < 0 || index >= m_filteredItems.size()) return QJsonObject();
    const MessageItem &item = m_filteredItems.at(index);
    QJsonObject obj;
    obj["id"] = item.id;
    obj["subject"] = item.subject;
    obj["from_name"] = item.fromName;
    obj["from_email"] = item.fromEmail;
    obj["to_name"] = item.toName;
    obj["to_email"] = item.toEmail;
    obj["date"] = item.date;
    obj["snippet"] = item.snippet;
    obj["body_html"] = item.bodyHtml;
    obj["seen"] = item.seen;
    obj["has_attachments"] = item.hasAttachments;
    obj["attachments_count"] = item.attachmentsCount;
    obj["avatar_color"] = item.avatarColor;
    obj["initials"] = item.initials;
    return obj;
}

QString MessageListModel::computeInitials(const QString &name, const QString &email) const {
    if (!name.trimmed().isEmpty()) {
        QStringList parts = name.split(' ', Qt::SkipEmptyParts);
        if (parts.size() >= 2) {
            return (parts[0].left(1) + parts[1].left(1)).toUpper();
        } else if (!parts.isEmpty()) {
            return parts[0].left(2).toUpper();
        }
    }
    if (!email.isEmpty()) {
        return email.left(2).toUpper();
    }
    return "??";
}

QString MessageListModel::computeAvatarColor(const QString &email) const {
    static const QStringList palette = {
        "#E06C75", "#98C379", "#E5C07B", "#61AFEF",
        "#C678DD", "#56B6C2", "#D19A66", "#4FACFE"
    };
    QByteArray hash = QCryptographicHash::hash(email.toLower().trimmed().toUtf8(), QCryptographicHash::Md5);
    quint32 val = 0;
    for (int i = 0; i < 4 && i < hash.size(); ++i) {
        val = (val << 8) | (static_cast<quint8>(hash[i]));
    }
    return palette[val % palette.size()];
}
