import QtQuick
import QtQuick.Controls

// The day an invite lands on, as one horizontal strip. The invite is yellow.
// Everything else already on that day takes its calendar colour. Free stretches
// say how long they are, so Accept / Maybe / Decline is a look at the day.
Item {
    id: root
    property var blocks: []

    property var laid: []
    property var gaps: []
    property var ticks: []
    property var alldayLaid: []
    property int lanes: 1
    property bool hasHours: false
    property real awakeX: 0
    property real awakeW: 0
    readonly property int laneH: 84

    implicitHeight: column.implicitHeight
    height: implicitHeight

    onBlocksChanged: root.layout()
    onWidthChanged: root.layout()
    Component.onCompleted: root.layout()

    Connections {
        target: Theme
        function onChanged() {
            root.layout();
        }
    }

    // Hidden swatch so a "#rrggbb" string and a palette colour both answer
    // inkOn with a real QColor.
    Rectangle {
        id: probe
        visible: false
        width: 1
        height: 1
    }

    function inkOn(c) {
        probe.color = c;
        var col = probe.color;
        var l = 0.299 * col.r + 0.587 * col.g + 0.114 * col.b;
        return l > 0.62 ? "#1a1b26" : "#f4f4f5";
    }
    function clock(mins) {
        mins = Math.round(mins);
        var h = Math.floor(mins / 60) % 24;
        var m = mins % 60;
        return (h < 10 ? "0" : "") + h + ":" + (m < 10 ? "0" : "") + m;
    }
    function hourLabel(mins) {
        var h = Math.floor(mins / 60) % 24;
        return (h < 10 ? "0" : "") + h;
    }
    function hoursText(mins) {
        var rounded = Math.round(mins / 30) / 2;
        return rounded + "h";
    }

    function layout() {
        var w = root.width;
        if (w < 8)
            return;
        var raw = root.blocks || [];
        var proposed = null;
        var timed = [];
        var allday = [];
        for (var i = 0; i < raw.length; i++) {
            var b = raw[i];
            if (!b)
                continue;
            if (b.all_day) {
                allday.push(b);
                continue;
            }
            var s = new Date(b.start);
            var e = new Date(b.end || b.start);
            if (isNaN(s.getTime()))
                continue;
            if (isNaN(e.getTime()) || e <= s)
                e = new Date(s.getTime() + 60 * 60000);
            if (b.proposed)
                proposed = {
                    start: s,
                    end: e
                };
            timed.push({
                b: b,
                start: s,
                end: e
            });
        }
        var anchor = proposed ? proposed.start : (timed.length ? timed[0].start : null);
        if (!anchor && allday.length) {
            var ad = new Date(allday[0].start);
            if (!isNaN(ad.getTime()))
                anchor = ad;
        }
        root.alldayLaid = paintAllDay(allday);
        if (!anchor || isNaN(anchor.getTime())) {
            root.laid = [];
            root.gaps = [];
            root.ticks = [];
            root.lanes = 1;
            root.hasHours = false;
            return;
        }
        var day0 = new Date(anchor.getFullYear(), anchor.getMonth(), anchor.getDate(), 0, 0, 0, 0);
        function minsOf(d) {
            return (d.getTime() - day0.getTime()) / 60000;
        }

        var events = [];
        for (var j = 0; j < timed.length; j++) {
            var a = minsOf(timed[j].start);
            var z = minsOf(timed[j].end);
            if (z <= 0 || a >= 1440)
                continue;
            a = Math.max(0, a);
            z = Math.min(1440, z);
            if (z <= a)
                z = Math.min(1440, a + 1);
            events.push({
                start: a,
                end: z,
                summary: timed[j].b.summary || "Busy",
                proposed: !!timed[j].b.proposed,
                color: timed[j].b.color || "",
                calendar: timed[j].b.calendar || ""
            });
        }
        events.sort(function (p, q) {
            if (p.start !== q.start)
                return p.start - q.start;
            return p.end - q.end;
        });

        var winStart = 7 * 60;
        var winEnd = 23 * 60;
        for (var k = 0; k < events.length; k++) {
            winStart = Math.min(winStart, Math.floor(events[k].start / 60) * 60);
            winEnd = Math.max(winEnd, Math.ceil(events[k].end / 60) * 60);
        }
        winStart = Math.max(0, winStart);
        winEnd = Math.min(1440, Math.max(winEnd, winStart + 60));

        var laneEnds = [];
        for (var n = 0; n < events.length; n++) {
            var lane = -1;
            for (var L = 0; L < laneEnds.length; L++) {
                if (laneEnds[L] <= events[n].start + 0.01) {
                    lane = L;
                    break;
                }
            }
            if (lane < 0) {
                lane = laneEnds.length;
                laneEnds.push(events[n].end);
            } else {
                laneEnds[lane] = events[n].end;
            }
            events[n].lane = lane;
        }
        var span = winEnd - winStart;
        function xOf(m) {
            return (m - winStart) / span * w;
        }

        var laid = [];
        for (var p = 0; p < events.length; p++) {
            var ev = events[p];
            var x = xOf(ev.start);
            var bw = Math.max(6, xOf(ev.end) - x);
            if (x + bw > w)
                bw = Math.max(4, w - x);
            var fill = ev.proposed ? Theme.yellow : (ev.color ? ev.color : Theme.accent);
            laid.push({
                x: x,
                w: bw,
                lane: ev.lane,
                fill: fill,
                ink: root.inkOn(fill),
                proposed: ev.proposed,
                label: ev.summary,
                tip: root.clock(ev.start) + "–" + root.clock(ev.end) + "  " + ev.summary + (ev.calendar ? "  ·  " + ev.calendar : "") + (ev.proposed ? "  ·  this invite" : "")
            });
        }

        var pxPerHour = w / (span / 60);
        var step = pxPerHour < 28 ? 240 : (pxPerHour < 46 ? 120 : 60);
        var ticks = [];
        var first = Math.ceil(winStart / step) * step;
        if (first === winStart)
            first += step;
        for (var t = first; t < winEnd; t += step) {
            if (winEnd - t < step * 0.35)
                continue;
            ticks.push({
                x: xOf(t),
                label: root.hourLabel(t)
            });
        }

        var awakeA = Math.max(winStart, 8 * 60);
        var awakeZ = Math.min(winEnd, 22 * 60);
        root.awakeX = xOf(awakeA);
        root.awakeW = Math.max(0, xOf(awakeZ) - xOf(awakeA));

        var covered = events.slice().sort(function (p, q) {
            return p.start - q.start;
        });
        var gaps = [];
        var cursor = awakeA;
        for (var g = 0; g < covered.length; g++) {
            var ca = Math.max(covered[g].start, awakeA);
            var cz = Math.min(covered[g].end, awakeZ);
            if (cz <= awakeA || ca >= awakeZ)
                continue;
            if (ca - cursor >= 60)
                gaps.push(gapAt(cursor, ca));
            if (cz > cursor)
                cursor = cz;
        }
        if (awakeZ - cursor >= 60)
            gaps.push(gapAt(cursor, awakeZ));
        function gapAt(from, to) {
            var gw = xOf(to) - xOf(from);
            if (gw < 22)
                return null;
            return {
                x: xOf((from + to) / 2),
                label: root.hoursText(to - from)
            };
        }
        var kept = [];
        for (var gi = 0; gi < gaps.length; gi++) {
            if (gaps[gi])
                kept.push(gaps[gi]);
        }

        root.lanes = Math.max(1, laneEnds.length);
        root.laid = laid;
        root.ticks = ticks;
        root.gaps = kept;
        root.hasHours = true;
    }

    function paintAllDay(list) {
        var out = [];
        for (var i = 0; i < list.length; i++) {
            var b = list[i];
            var fill = b.proposed ? Theme.yellow : (b.color ? b.color : Theme.accent);
            var summary = b.summary || "Busy";
            out.push({
                summary: summary,
                fill: fill,
                ink: root.inkOn(fill),
                tip: summary + " · all day" + (b.proposed ? " · this invite" : "")
            });
        }
        return out;
    }

    Column {
        id: column
        width: parent.width
        spacing: 6

        Flow {
            width: parent.width
            spacing: 6
            visible: root.alldayLaid.length > 0
            Repeater {
                model: root.alldayLaid
                delegate: Rectangle {
                    required property var modelData
                    radius: 6
                    height: 22
                    width: chip.width + 16
                    color: modelData.fill
                    clip: true
                    Text {
                        id: chip
                        anchors.verticalCenter: parent.verticalCenter
                        x: 8
                        width: Math.min(implicitWidth, column.width - 16)
                        text: modelData.summary
                        elide: Text.ElideRight
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: modelData.ink
                    }
                    HoverHandler {
                        id: dayHover
                    }
                    ToolTip.visible: dayHover.hovered
                    ToolTip.text: modelData.tip
                    ToolTip.delay: 300
                }
            }
        }

        Item {
            width: parent.width
            height: root.hasHours ? labels.implicitHeight : 0
            visible: root.hasHours
            Text {
                id: labels
                visible: false
                text: "00"
                font.family: Theme.fontFamily
                font.pixelSize: 10
            }
            Repeater {
                model: root.ticks
                delegate: Text {
                    required property var modelData
                    x: Math.max(0, Math.min(modelData.x - width / 2, root.width - width))
                    text: modelData.label
                    font.family: Theme.fontFamily
                    font.pixelSize: 10
                    color: Theme.textDim
                }
            }
        }

        Rectangle {
            id: track
            visible: root.hasHours
            width: parent.width
            height: root.lanes * root.laneH
            radius: 8
            color: Theme.darkerBackground
            // The waking day. What is left on either side is morning and night.
            Rectangle {
                x: root.awakeX
                width: root.awakeW
                height: parent.height
                color: Theme.selection
            }
            Item {
                x: root.awakeX + root.awakeW
                width: Math.max(0, track.width - x)
                height: parent.height
                clip: true
                Repeater {
                    model: parent.width > 16 ? 6 : 0
                    delegate: Rectangle {
                        required property int index
                        width: 2
                        height: 2
                        radius: 1
                        color: Theme.brightForeground
                        opacity: 0.4 + (index % 3) * 0.12
                        x: 8 + (index * 13) % Math.max(1, parent.width - 12)
                        y: 12 + (index * 23) % Math.max(1, parent.height - 18)
                    }
                }
            }
            Repeater {
                model: root.ticks
                delegate: Rectangle {
                    required property var modelData
                    x: modelData.x
                    width: 1
                    height: parent.height
                    color: Theme.hairline
                    opacity: 0.85
                }
            }
            Repeater {
                model: root.laid
                delegate: Rectangle {
                    id: bar
                    required property var modelData
                    x: modelData.x
                    y: 5 + modelData.lane * root.laneH
                    width: modelData.w
                    height: root.laneH - 10
                    radius: 5
                    color: modelData.fill
                    border.width: modelData.proposed ? 1 : 0
                    border.color: "#1a1b26"
                    Text {
                        visible: bar.width >= 18 && bar.width < 72
                        anchors.centerIn: parent
                        width: bar.height - 12
                        rotation: -90
                        text: modelData.label
                        elide: Text.ElideRight
                        horizontalAlignment: Text.AlignHCenter
                        font.family: Theme.fontFamily
                        font.pixelSize: 10
                        color: modelData.ink
                    }
                    Text {
                        visible: bar.width >= 72
                        anchors.fill: parent
                        anchors.margins: 6
                        text: modelData.label
                        wrapMode: Text.Wrap
                        elide: Text.ElideRight
                        maximumLineCount: 3
                        verticalAlignment: Text.AlignVCenter
                        font.family: Theme.fontFamily
                        font.pixelSize: 11
                        color: modelData.ink
                    }
                    HoverHandler {
                        id: barHover
                    }
                    ToolTip.visible: barHover.hovered
                    ToolTip.text: modelData.tip
                    ToolTip.delay: 300
                }
            }
        }

        Item {
            width: parent.width
            height: root.gaps.length > 0 ? 14 : 0
            visible: root.hasHours && root.gaps.length > 0
            Repeater {
                model: root.gaps
                delegate: Text {
                    required property var modelData
                    x: Math.max(0, Math.min(modelData.x - width / 2, root.width - width))
                    text: modelData.label
                    font.family: Theme.fontFamily
                    font.pixelSize: 10
                    color: Theme.textDim
                }
            }
        }
    }
}
