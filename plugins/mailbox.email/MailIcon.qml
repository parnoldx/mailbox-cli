import QtQuick
import QtQuick.Shapes
import qs.Commons

// MailIcon.qml — Smooth, high-precision vector envelope icon.
//
// Drawn using GPU-accelerated vector shapes with rounded corners,
// a curved flap fold, and 4x MSAA antialiasing for maximum sharpness.
Item {
  id: root

  property real iconSize: Style.space(14)
  property color color: Color.accent

  // Proportions: classic 4:3 envelope ratio
  readonly property real boxWidth: Math.round(iconSize * 1.2)
  readonly property real boxHeight: Math.round(iconSize * 0.85)
  readonly property real strokeW: Math.max(1.4, Math.round(iconSize * 0.1 * 10) / 10)
  readonly property real cornerR: 2.5

  implicitWidth: boxWidth + 4
  implicitHeight: boxHeight + 4
  width: implicitWidth
  height: implicitHeight

  Shape {
    anchors.centerIn: parent
    width: root.boxWidth
    height: root.boxHeight
    antialiasing: true
    layer.enabled: true
    layer.samples: 4

    // 1. Smooth rounded envelope body
    ShapePath {
      strokeColor: root.color
      strokeWidth: root.strokeW
      fillColor: "transparent"
      capStyle: ShapePath.RoundCap
      joinStyle: ShapePath.RoundJoin

      startX: root.cornerR
      startY: 0.5

      // Top edge
      PathLine { x: root.boxWidth - root.cornerR; y: 0.5 }
      // Top-right corner
      PathArc {
        x: root.boxWidth - 0.5
        y: root.cornerR
        radiusX: root.cornerR
        radiusY: root.cornerR
      }
      // Right edge
      PathLine { x: root.boxWidth - 0.5; y: root.boxHeight - root.cornerR }
      // Bottom-right corner
      PathArc {
        x: root.boxWidth - root.cornerR
        y: root.boxHeight - 0.5
        radiusX: root.cornerR
        radiusY: root.cornerR
      }
      // Bottom edge
      PathLine { x: root.cornerR; y: root.boxHeight - 0.5 }
      // Bottom-left corner
      PathArc {
        x: 0.5
        y: root.boxHeight - root.cornerR
        radiusX: root.cornerR
        radiusY: root.cornerR
      }
      // Left edge
      PathLine { x: 0.5; y: root.cornerR }
      // Top-left corner
      PathArc {
        x: root.cornerR
        y: 0.5
        radiusX: root.cornerR
        radiusY: root.cornerR
      }
    }

    // 2. Elegant flap fold with subtle rounded apex
    ShapePath {
      strokeColor: root.color
      strokeWidth: root.strokeW
      fillColor: "transparent"
      capStyle: ShapePath.RoundCap
      joinStyle: ShapePath.RoundJoin

      startX: 1.5
      startY: root.cornerR + 0.5

      PathLine {
        x: root.boxWidth / 2 - 1.2
        y: root.boxHeight * 0.56
      }
      PathQuad {
        x: root.boxWidth / 2 + 1.2
        y: root.boxHeight * 0.56
        controlX: root.boxWidth / 2
        controlY: root.boxHeight * 0.56 + 1.2
      }
      PathLine {
        x: root.boxWidth - 1.5
        y: root.cornerR + 0.5
      }
    }
  }
}
