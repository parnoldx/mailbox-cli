#pragma once

#include <QAbstractListModel>
#include <QVariantList>

// MailModel holds one box's worth of message summaries. The daemon returns a
// flat "Name <addr>" from-string and a "YYYY-MM-DD HH:MM" date; the model splits
// those into the display-name, address and a short relative date the row wants.
class MailModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCountProp NOTIFY changed)
    // Every row as a plain {id, fromName, …} map, for the views that keep a
    // JS-array mirror of the model (BucketView's new/seen split, the search
    // list, the Feed, the bottom stacks) instead of re-walking count/get(i).
    Q_PROPERTY(QVariantList rows READ rows NOTIFY changed)

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
    QVariantList rows() const;
    Q_INVOKABLE void setRows(const QVariantList &rows);
    Q_INVOKABLE QVariantMap get(int i) const;

signals:
    void changed();

private:
    struct Row {
        QString id, fromName, fromAddr, subject, date, dateRaw;
        bool seen = false;
        // How many Messages are in this row's Thread in all, wherever they
        // sit — 0 for a Message on its own (the daemon already collapsed the
        // listing to one row per Thread; this is just its badge).
        int count = 0;
    };
    static QVariantMap rowMap(const Row &r);
    QList<Row> m_rows;
};
