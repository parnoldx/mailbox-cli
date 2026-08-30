#include "ContactSuggestModel.hpp"

ContactSuggestModel::ContactSuggestModel(QObject *parent)
    : QAbstractListModel(parent)
{
    // Seed initial address book contacts
    m_allContacts = {
        {"Max Mustermann", "max@example.org", "PA", "#E06C75"},
        {"Sam Rivers", "sarah.connor@example.com", "SC", "#98C379"},
        {"Lee Turner", "linus@example.org", "LT", "#61AFEF"},
        {"Alex Morgan", "alex@example.org", "AM", "#E5C07B"},
        {"Dana Hart", "dana@example.com", "DH", "#C678DD"},
        {"Dana Berg", "dana@example.com", "DJ", "#56B6C2"},
        {"Gina Ross", "gina@example.com", "GV", "#D19A66"},
        {"Bruno Stone", "bruno@example.com", "BS", "#4FACFE"}
    };
    m_filteredContacts = m_allContacts;
}

int ContactSuggestModel::rowCount(const QModelIndex &parent) const {
    if (parent.isValid()) return 0;
    return m_filteredContacts.size();
}

QVariant ContactSuggestModel::data(const QModelIndex &index, int role) const {
    if (!index.isValid() || index.row() < 0 || index.row() >= m_filteredContacts.size()) {
        return QVariant();
    }
    const ContactItem &c = m_filteredContacts.at(index.row());
    switch (role) {
        case NameRole: return c.name;
        case EmailRole: return c.email;
        case InitialsRole: return c.initials;
        case AvatarColorRole: return c.avatarColor;
        default: return QVariant();
    }
}

QHash<int, QByteArray> ContactSuggestModel::roleNames() const {
    QHash<int, QByteArray> roles;
    roles[NameRole] = "name";
    roles[EmailRole] = "email";
    roles[InitialsRole] = "initials";
    roles[AvatarColorRole] = "avatarColor";
    return roles;
}

void ContactSuggestModel::filter(const QString &query) {
    beginResetModel();
    m_filteredContacts.clear();
    QString q = query.trimmed();
    if (q.isEmpty()) {
        m_filteredContacts = m_allContacts;
    } else {
        for (const ContactItem &c : m_allContacts) {
            if (c.name.contains(q, Qt::CaseInsensitive) || c.email.contains(q, Qt::CaseInsensitive)) {
                m_filteredContacts.append(c);
            }
        }
    }
    endResetModel();
    emit countChanged();
}

void ContactSuggestModel::setContacts(const QJsonArray &contacts) {
    beginResetModel();
    m_allContacts.clear();
    for (const QJsonValue &v : contacts) {
        QJsonObject o = v.toObject();
        ContactItem c;
        c.name = o.value("name").toString();
        c.email = o.value("email").toString();
        c.initials = c.name.left(2).toUpper();
        c.avatarColor = "#61AFEF";
        m_allContacts.append(c);
    }
    m_filteredContacts = m_allContacts;
    endResetModel();
    emit countChanged();
}
