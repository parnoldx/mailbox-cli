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
        CountRole,
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
        // How many Messages of this row's Thread have a Placement in the box
        // being shown — 0 for a Message on its own (the daemon already
        // collapsed the listing to one row per Thread; this is just its badge).
        int count = 0;
    };
    QList<Row> m_rows;
};
