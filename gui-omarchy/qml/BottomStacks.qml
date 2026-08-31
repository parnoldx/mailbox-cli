import QtQuick

// HEY's trick: the two hand-tended piles — Reply Later and Set Aside — ride
// along the bottom of the Inbox as little fanned stacks, so mail you have
// triaged for later stays in sight instead of behind a bucket switch. Tapping a
// card opens it; tapping the pile's glyph jumps to that bucket in full.
//
// Shown only on the Inbox, and only for piles that actually hold something.
Item {
    id: root

    readonly property bool onInbox: win.currentKey() === "INBOX"
    visible: onInbox && (replyLaterModel.count + asideModel.count) > 0
    implicitHeight: visible ? row.implicitHeight + 28 : 0

    Row {
        id: row
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: 14
        spacing: 28

        PileStack {
            model: replyLaterModel
            bucketKey: "Reply Later"
            glyph: "\uf112"
        }
        PileStack {
            model: asideModel
            bucketKey: "Aside"
            glyph: "\uf02e"
        }
    }
}
