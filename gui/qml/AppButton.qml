import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Button {
    id: control

    property string variant: "secondary" // "primary" | "secondary" | "ghost" | "danger" | "warning"
    property string iconText: ""
    property int customRadius: Theme.radiusSmall
    property bool pillShape: false

    font.family: Theme.fontFamily
    font.pixelSize: 13
    font.bold: variant === "primary"

    implicitHeight: 36
    implicitWidth: Math.max(84, contentLayout.implicitWidth + 32)
    leftPadding: 16
    rightPadding: 16

    HoverHandler {
        cursorShape: Qt.PointingHandCursor
    }

    contentItem: RowLayout {
        id: contentLayout
        spacing: 8
        anchors.centerIn: parent

        Text {
            visible: control.iconText !== ""
            text: control.iconText
            font.pixelSize: 15
            color: control.variant === "primary" ? "#0f1f28" :
                   control.variant === "danger" ? Theme.urgent :
                   control.variant === "warning" ? Theme.warning :
                   (control.hovered ? Theme.accent : Theme.foreground)
        }

        Text {
            text: control.text
            font: control.font
            color: control.variant === "primary" ? "#0f1f28" :
                   control.variant === "danger" ? Theme.urgent :
                   control.variant === "warning" ? Theme.warning :
                   (control.hovered ? Theme.foreground : (control.variant === "ghost" ? Theme.dim : Theme.foreground))
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
        }
    }

    background: Rectangle {
        implicitHeight: control.implicitHeight
        radius: control.pillShape ? height / 2 : control.customRadius

        color: {
            if (control.variant === "primary") {
                return control.down ? Qt.darker(Theme.accent, 1.15) :
                       (control.hovered ? Theme.accentHover : Theme.accent)
            } else if (control.variant === "danger") {
                return control.down ? Qt.darker(Theme.surfaceAlt, 1.2) :
                       (control.hovered ? "#3A222B" : "transparent")
            } else if (control.variant === "ghost") {
                return control.hovered ? Theme.surfaceHover : "transparent"
            } else if (control.variant === "warning") {
                return control.hovered ? Theme.surfaceHover : Theme.surfaceAlt
            } else {
                // Secondary (default)
                return control.down ? Qt.darker(Theme.surfaceAlt, 1.1) :
                       (control.hovered ? Theme.surfaceHover : Theme.surfaceAlt)
            }
        }

        border.color: {
            if (control.variant === "primary") {
                return "transparent"
            } else if (control.variant === "danger") {
                return control.hovered ? Theme.urgent : Theme.border
            } else if (control.variant === "ghost") {
                return control.hovered ? Theme.border : "transparent"
            } else if (control.variant === "warning") {
                return Theme.warning
            } else {
                return control.hovered ? Theme.accent : Theme.border
            }
        }
        border.width: 1

        Behavior on color { ColorAnimation { duration: Theme.animFast } }
        Behavior on border.color { ColorAnimation { duration: Theme.animFast } }
    }
}
