import QtQuick
import "MailFormat.js" as Fmt

Item {
    id: root
    property var row: ({})
    property bool fresh: false
    property bool highlighted: false
    // What a tap does. The default opens the row in the reader; the search
    // overlay overrides it to open a hit without leaving its own context.
    property var openAction: (function (id) { win.openMessage(id) })
    // Right-click triage. Off for search results (a hit is open-only) and for
    // Drafts rows (a draft is not routed / set aside / trashed to Trash).
    property bool menuEnabled: true
    // A far-right trash icon for fast delete — the Drafts bucket uses it.
    property bool showDelete: false
    signal deleteClicked()
    height: 78

    Rectangle {
        anchors.fill: parent
        anchors.topMargin: 2
        anchors.bottomMargin: 2
        radius: Theme.radiusSmall
        color: root.highlighted ? Theme.selection
             : hover.hovered ? Theme.cardHover : "transparent"
        border.width: root.highlighted ? 1 : 0
        border.color: Theme.accent
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }

        Rectangle {
            width: 3; radius: 2
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom
                      topMargin: 16; bottomMargin: 16 }
            color: Theme.accent
            opacity: root.fresh ? 1 : 0
            Behavior on opacity { NumberAnimation { duration: Theme.anim } }
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Avatar {
        id: av
        width: 40; height: 40; radius: 20
        anchors { left: parent.left; leftMargin: 18; verticalCenter: parent.verticalCenter }
        name: root.row.fromName || ""
        seed: root.row.fromAddr || ""
    }

    Column {
        anchors {
            left: av.right; leftMargin: 16
            right: parent.right; rightMargin: 64
            verticalCenter: parent.verticalCenter
        }
        spacing: 4
        Text {
            width: parent.width
            text: root.row.fromName || ""
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 13
            font.weight: root.fresh ? Font.Bold : Font.Normal
            color: root.fresh ? Theme.textPrimary : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        Text {
            width: parent.width
            text: Fmt.stripSubjectPrefixes(root.row.subject) || root.row.subject || ""
            elide: Text.ElideRight
            font.family: Theme.fontFamily
            font.pixelSize: 14
            font.weight: root.fresh ? Font.DemiBold : Font.Normal
            color: root.fresh ? Theme.textPrimary : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    // How many Messages are in this conversation — pinned top-right, the way
    // HEY badges a thread. The daemon already collapsed the listing to one
    // row per Thread and this is its whole size, wherever its Messages sit.
    Rectangle {
        id: countBadge
        visible: (root.row.count || 0) > 1
        anchors { top: parent.top; right: parent.right; topMargin: 16; rightMargin: 16 }
        width: Math.max(20, countText.implicitWidth + 14); height: 20; radius: 10
        color: Theme.selection
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Text {
            id: countText
            anchors.centerIn: parent
            text: root.row.count || ""
            font.family: Theme.fontFamily
            font.pixelSize: 11
            font.weight: Font.DemiBold
            color: Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
    }

    Text {
        id: date
        anchors { right: parent.right; rightMargin: root.showDelete ? 44 : 14; bottom: parent.bottom; bottomMargin: 14 }
        text: root.row.date || ""
        font.family: Theme.fontFamily
        font.pixelSize: 11
        color: Theme.textDim
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    // Fast-delete affordance, pinned to the far right and vertically centred.
    Rectangle {
        id: delBtn
        visible: root.showDelete
        anchors { right: parent.right; rightMargin: 14; verticalCenter: parent.verticalCenter }
        width: 24; height: 24; radius: 12
        color: delHover.hovered ? Theme.red : Theme.selection
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Text {
            anchors.centerIn: parent
            text: ""
            font.family: Theme.fontFamily
            font.pixelSize: 10
            color: delHover.hovered ? "#ffffff" : Theme.textDim
            Behavior on color { ColorAnimation { duration: Theme.anim } }
        }
        HoverHandler { id: delHover; cursorShape: Qt.PointingHandCursor }
        TapHandler { onTapped: root.deleteClicked() }
    }

    Rectangle {
        anchors { bottom: parent.bottom; left: av.right; right: parent.right; leftMargin: 16 }
        height: 1
        color: Theme.hairline
        opacity: 0.4
        Behavior on color { ColorAnimation { duration: Theme.anim } }
    }

    HoverHandler { id: hover }
    TapHandler { onTapped: root.openAction(root.row.id) }
    // Right-click opens the triage menu for this row's whole Thread.
    TapHandler {
        acceptedButtons: Qt.RightButton
        enabled: root.menuEnabled
        onTapped: win.showRowMenu(root.row)
    }
}
