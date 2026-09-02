#include "MailModel.hpp"

#include <QDate>
#include <QDateTime>
#include <QRegularExpression>

static void splitFrom(const QString &raw, QString &name, QString &addr) {
    static const QRegularExpression re(R"(^\s*(.*?)\s*<([^>]+)>\s*$)");
    const auto m = re.match(raw);
    if (m.hasMatch()) {
        name = m.captured(1).remove('"').trimmed();
        addr = m.captured(2).trimmed();
    } else {
        addr = raw.trimmed();
    }
    if (name.isEmpty())
        name = addr.section('@', 0, 0);
}

static QString shortDate(const QString &raw) {
    const QDateTime dt = QDateTime::fromString(raw, "yyyy-MM-dd HH:mm");
    if (!dt.isValid())
        return raw;
    const QDate today = QDate::currentDate();
    if (dt.date() == today)
        return dt.toString("HH:mm");
    if (dt.date().year() == today.year())
        return dt.toString("d MMM");
    return dt.toString("MMM yyyy");
}

int MailModel::rowCount(const QModelIndex &) const { return m_rows.size(); }

QVariant MailModel::data(const QModelIndex &index, int role) const {
    if (index.row() < 0 || index.row() >= m_rows.size())
        return {};
    const Row &r = m_rows.at(index.row());
    switch (role) {
    case IdRole: return r.id;
    case FromNameRole: return r.fromName;
    case FromAddrRole: return r.fromAddr;
    case SubjectRole: return r.subject;
    case DateRole: return r.date;
    case DateRawRole: return r.dateRaw;
    case SeenRole: return r.seen;
    case CountRole: return r.count;
    }
    return {};
}

QHash<int, QByteArray> MailModel::roleNames() const {
    return {
        {IdRole, "msgId"},   {FromNameRole, "fromName"}, {FromAddrRole, "fromAddr"},
        {SubjectRole, "subject"}, {DateRole, "date"},    {DateRawRole, "dateRaw"},
        {SeenRole, "seen"},  {CountRole, "count"},
    };
}

void MailModel::setRows(const QVariantList &rows) {
    beginResetModel();
    m_rows.clear();
    for (const QVariant &v : rows) {
        const QVariantMap m = v.toMap();
        Row r;
        r.id = m.value("id").toString();
        r.subject = m.value("subject").toString().trimmed();
        if (r.subject.isEmpty())
            r.subject = "(no subject)";
        r.dateRaw = m.value("date").toString();
        r.date = shortDate(r.dateRaw);
        r.seen = m.value("seen").toBool();
        r.count = m.value("count").toInt();
        splitFrom(m.value("from").toString(), r.fromName, r.fromAddr);
        m_rows.push_back(r);
    }
    endResetModel();
    emit changed();
}

QVariantMap MailModel::get(int i) const {
    if (i < 0 || i >= m_rows.size())
        return {};
    const Row &r = m_rows.at(i);
    return {{"id", r.id}, {"fromName", r.fromName}, {"fromAddr", r.fromAddr},
            {"subject", r.subject}, {"date", r.date}, {"dateRaw", r.dateRaw},
            {"seen", r.seen}, {"count", r.count}};
}
