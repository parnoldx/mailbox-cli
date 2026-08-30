#pragma once

#include <QAbstractListModel>
#include <QVariantList>

// MailModel holds one box's worth of message summaries. The daemon returns a
// flat "Name <addr>" from-string and a "YYYY-MM-DD HH:MM" date; the model splits
// those into the display-name, address and a short relative date the row wants.
class MailModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCountProp NOTIFY changed)

public:
    enum Role {
        IdRole = Qt::UserRole + 1,
        FromNameRole,
        FromAddrRole,
        SubjectRole,
        DateRole,
        DateRawRole,
        SeenRole,
    };

    using QAbstractListModel::QAbstractListModel;

    int rowCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role) const override;
    QHash<int, QByteArray> roleNames() const override;

    int rowCountProp() const { return m_rows.size(); }
    Q_INVOKABLE void setRows(const QVariantList &rows);
    Q_INVOKABLE QVariantMap get(int i) const;

signals:
    void changed();

private:
    struct Row {
        QString id, fromName, fromAddr, subject, date, dateRaw;
        bool seen = false;
    };
    QList<Row> m_rows;
};
