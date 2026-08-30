#pragma once

#include <QAbstractListModel>
#include <QList>
#include <QJsonObject>
#include <QJsonArray>

struct ContactItem {
    QString name;
    QString email;
    QString initials;
    QString avatarColor;
};

class ContactSuggestModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)

public:
    enum Roles {
        NameRole = Qt::UserRole + 1,
        EmailRole,
        InitialsRole,
        AvatarColorRole
    };
    Q_ENUM(Roles)

    explicit ContactSuggestModel(QObject *parent = nullptr);

    int rowCount(const QModelIndex &parent = QModelIndex()) const override;
    QVariant data(const QModelIndex &index, int role = Qt::DisplayRole) const override;
    QHash<int, QByteArray> roleNames() const override;

    Q_INVOKABLE void filter(const QString &query);
    Q_INVOKABLE void setContacts(const QJsonArray &contacts);

signals:
    void countChanged();

private:
    QList<ContactItem> m_allContacts;
    QList<ContactItem> m_filteredContacts;
};
