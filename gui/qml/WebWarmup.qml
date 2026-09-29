import QtQuick
import QtWebEngine

// Chromium costs a second or two to cold-start, and the first HTML mail of a
// session used to pay that on click — the engine was deliberately kept off
// the start-up path (see main.cpp). This mounts a 1×1 invisible view a moment
// after the Inbox is up instead: the first render stays exactly as it was,
// and by the time a real mail opens, the engine is hot. It also arms the
// tracking-pixel blocker here, so the profile is touched off the click path.
//
// The view stays mounted: a warm engine is the point, and the Chromium
// processes would be running from the first HTML mail on anyway.
Item {
    width: 1
    height: 1
    visible: false

    WebEngineView {
        id: blank
        anchors.fill: parent
        Component.onCompleted: {
            PixelBlock.arm()
            blank.loadHtml("")
        }
    }
}
