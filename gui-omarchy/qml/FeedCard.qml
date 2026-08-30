import QtQuick
import QtQuick.Controls.Basic

// One item in The Feed: a sender line, the subject, and the newsletter itself
// rendered as HTML on a sheet. Collapsed the sheet is clipped with a fade and a
// "Read more"; expanded it grows to the whole article in place. The heavy
// WebEngine render is only built when the card is near the viewport (or open),
// so a long feed does not spin up hundreds of web views at once.
Column {
    id: root

    property var controller: null
    property var scroller: null
    property var row: ({})
    property bool isNew: false
    property bool highlighted: false
    property bool expanded: false
    property bool showDividerBelow: false

    // The height the article sheet holds while collapsed — used for the loading
    // placeholder too, so the card is this tall from the first frame.
    readonly property int collapsedH: 360

    spacing: 6

    readonly property var bodyRec: controller ? controller.bodies[row.id] : undefined
    readonly property bool ready: bodyRec !== undefined && bodyRec !== null

    Component.onCompleted: if (controller) controller.needBody(row.id)

    function _esc(s) {
        return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    }
    readonly property string articleHtml: {
        if (!ready) return ""
        if (bodyRec.html && bodyRec.html.length > 0) return bodyRec.html
        return "<pre style='white-space:pre-wrap;font-family:inherit;font-size:14px'>"
             + _esc(bodyRec.body || "(this message has no text part)") + "</pre>"
    }

    // Position of this card's top/bottom in the Flickable's viewport, done with
    // plain reactive properties (mapToItem in a binding would not recompute as
    // the column lays out, leaving every card stuck "far away" on first paint).
    readonly property real _colY: parent ? parent.y : 0
    readonly property real topInView: scroller ? (y + _colY - scroller.contentY) : 0
    readonly property real bottomInView: topInView + card.height

    // Near the viewport -> worth building the WebEngine render. Expanded cards
    // stay rendered even once scrolled away.
    readonly property bool nearViewport: !!scroller
        && bottomInView > -1400
        && topInView < scroller.height + 1800

    // Scrolled fully above the fold -> the reader has got at least this far.
    onBottomInViewChanged: if (bottomInView < 4 && controller && controller.active)
                               controller.markUpTo(row.dateRaw)

    Rectangle {
        id: card
        width: parent.width
        height: inner.implicitHeight + 24
        clip: true
        radius: Theme.radiusSmall
        color: root.highlighted ? Theme.selection : Theme.cardBg
        border.width: 1
        border.color: root.highlighted ? Theme.accent : Theme.hairline
        Behavior on color { ColorAnimation { duration: Theme.anim } }
        Behavior on border.color { ColorAnimation { duration: Theme.anim } }
        Behavior on height { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }

        HoverHandler { id: cardHover }

        // New-since-last-visit marker.
        Rectangle {
            width: 3; radius: 2
            anchors { left: parent.left; top: parent.top; bottom: parent.bottom
                      topMargin: 14; bottomMargin: 14 }
            color: Theme.accent
            opacity: root.isNew ? 1 : 0
            Behavior on opacity { NumberAnimation { duration: Theme.anim } }
        }

        Column {
            id: inner
            anchors { left: parent.left; right: parent.right; top: parent.top
                      leftMargin: 16; rightMargin: 14; topMargin: 14 }
            spacing: 12

            // -- sender / subject line --------------------------------
            Item {
                width: parent.width
                height: 40

                Avatar {
                    id: av
                    width: 34; height: 34; radius: 17
                    anchors.left: parent.left
                    anchors.verticalCenter: parent.verticalCenter
                    name: root.row.fromName || ""
                    seed: root.row.fromAddr || ""
                }
                Column {
                    anchors { left: av.right; leftMargin: 14; right: date.left; rightMargin: 12
                              verticalCenter: parent.verticalCenter }
                    spacing: 3
                    Text {
                        width: parent.width
                        text: root.row.fromName || ""
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        font.weight: root.isNew ? Font.DemiBold : Font.Normal
                        color: root.isNew ? Theme.textPrimary : Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    Text {
                        width: parent.width
                        text: root.row.subject || ""
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 14
                        font.weight: Font.DemiBold
                        color: Theme.textPrimary
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                }
                Text {
                    id: date
                    anchors { right: chevron.left; rightMargin: 10; verticalCenter: parent.verticalCenter }
                    text: root.row.date || ""
                    font.family: Theme.fontFamily
                    font.pixelSize: 11
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }
                Text {
                    id: chevron
                    anchors { right: parent.right; verticalCenter: parent.verticalCenter }
                    text: root.expanded ? "▾" : "▸"
                    font.family: Theme.fontFamily
                    font.pixelSize: 12
                    color: Theme.textDim
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                }

                TapHandler { onTapped: if (root.controller) root.controller.toggle(root.row.id) }
            }

            // -- the article ----------------------------------------
            Rectangle {
                id: sheetBox
                width: parent.width
                height: sheetLoader.item ? sheetLoader.item.implicitHeight : root.collapsedH
                radius: Theme.radiusSmall
                clip: true
                color: Theme.dark ? Theme.background : "#ffffff"
                border.width: 1
                border.color: Theme.hairline
                Behavior on color { ColorAnimation { duration: Theme.anim } }
                Behavior on height { NumberAnimation { duration: Theme.anim; easing.type: Easing.OutCubic } }

                Loader {
                    id: sheetLoader
                    anchors { fill: parent; margins: 1 }
                    active: root.ready && (root.nearViewport || root.expanded)
                    sourceComponent: articleComp
                }

                Component {
                    id: articleComp
                    FeedArticle {
                        html: root.articleHtml
                        expanded: root.expanded
                        collapsedHeight: root.collapsedH
                    }
                }

                // Placeholder before the render is built.
                Column {
                    anchors { left: parent.left; right: parent.right; top: parent.top; margins: 16 }
                    spacing: 8
                    visible: !sheetLoader.active || !sheetLoader.item
                    Text {
                        width: parent.width
                        text: root.ready
                              ? String(root.bodyRec.body || "").replace(/\s+/g, " ").trim().substring(0, 240)
                              : "Loading preview…"
                        wrapMode: Text.Wrap
                        maximumLineCount: 4
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 12
                        lineHeight: 1.45
                        color: Theme.dark ? Theme.foreground : "#1b1b1b"
                    }
                }
            }

            // -- footer -------------------------------------------
            Row {
                width: parent.width
                spacing: 10
                bottomPadding: 2

                Rectangle {
                    width: moreLabel.implicitWidth + 22; height: 24; radius: 12
                    color: moreHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Text {
                        id: moreLabel
                        anchors.centerIn: parent
                        text: root.expanded ? "▴  Show less" : "Read more  ▾"
                        font.family: Theme.fontFamily; font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    HoverHandler { id: moreHover }
                    TapHandler { onTapped: if (root.controller) root.controller.toggle(root.row.id) }
                }

                Rectangle {
                    width: openLabel.implicitWidth + 22; height: 24; radius: 12
                    color: openHover.hovered ? Theme.cardHover : Theme.selection
                    Behavior on color { ColorAnimation { duration: Theme.anim } }
                    Text {
                        id: openLabel
                        anchors.centerIn: parent
                        text: "Open full page  ↗"
                        font.family: Theme.fontFamily; font.pixelSize: 10
                        color: Theme.textDim
                        Behavior on color { ColorAnimation { duration: Theme.anim } }
                    }
                    HoverHandler { id: openHover }
                    TapHandler { onTapped: win.openMessage(root.row.id) }
                }
            }
        }
    }

    FeedDivider {
        width: parent.width
        visible: root.showDividerBelow
        label: "you got to here last time"
    }
}
