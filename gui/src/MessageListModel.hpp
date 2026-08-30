#pragma once

#include <QAbstractListModel>
#include <QList>
#include <QJsonObject>
#include <QJsonArray>
#include <QColor>

struct MessageItem {
    QString id;
    QString subject;
    QString fromName;
    QString fromEmail;
    QString toName;
    QString toEmail;
    QString date;
    QString snippet;
    QString bodyHtml;
    bool seen{false};
    bool aside{false};
    bool paperTrail{false};
    bool feed{false};
    bool hasAttachments{false};
    int attachmentsCount{0};
    QString bucket;
    QString avatarColor;
    QString initials;
};

class MessageListModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
    Q_PROPERTY(QString currentBucket READ currentBucket WRITE setCurrentBucket NOTIFY currentBucketChanged)
    Q_PROPERTY(QString searchQuery READ searchQuery WRITE setSearchQuery NOTIFY searchQueryChanged)

public:
    enum Roles {
        IdRole = Qt::UserRole + 1,
        SubjectRole,
        FromNameRole,
        FromEmailRole,
        ToNameRole,
        ToEmailRole,
        DateRole,
        SnippetRole,
        BodyHtmlRole,
        SeenRole,
        AsideRole,
        PaperTrailRole,
        FeedRole,
        HasAttachmentsRole,
        AttachmentsCountRole,
        BucketRole,
        AvatarColorRole,
        InitialsRole
    };
    Q_ENUM(Roles)

    explicit MessageListModel(QObject *parent = nullptr);

    int rowCount(const QModelIndex &parent = QModelIndex()) const override;
    QVariant data(const QModelIndex &index, int role = Qt::DisplayRole) const override;
    QHash<int, QByteArray> roleNames() const override;

    QString currentBucket() const { return m_currentBucket; }
    void setCurrentBucket(const QString &bucket);

    QString searchQuery() const { return m_searchQuery; }
    void setSearchQuery(const QString &query);

    Q_INVOKABLE void loadFromMessages(const QJsonArray &messages);
    Q_INVOKABLE void populateBucketData(const QString &bucket);
    Q_INVOKABLE void setSeen(int index, bool seen);
    Q_INVOKABLE void setAside(int index);
    Q_INVOKABLE void removeMessage(int index);
    Q_INVOKABLE QJsonObject getMessage(int index) const;

signals:
    void countChanged();
    void currentBucketChanged();
    void searchQueryChanged();

private:
    void rebuildFilteredList();
    QString computeInitials(const QString &name, const QString &email) const;
    QString computeAvatarColor(const QString &email) const;

    QList<MessageItem> m_allItems;
    QList<MessageItem> m_filteredItems;
    QString m_currentBucket{"inbox"};
    QString m_searchQuery;
};
