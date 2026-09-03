cmake_minimum_required(VERSION 3.20)
project(mailbox-gui LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

find_package(Qt6 REQUIRED COMPONENTS Core Gui Qml Quick QuickControls2 Network WebEngineQuick PdfQuick QuickDialogs2 Multimedia)

qt_add_executable(mailbox-gui
    src/main.cpp
    src/OmarchyTheme.hpp src/OmarchyTheme.cpp
    src/MailboxClient.hpp src/MailboxClient.cpp
    src/MailModel.hpp src/MailModel.cpp
    src/PixelBlock.hpp src/PixelBlock.cpp
    src/SurfaceWatcher.hpp src/SurfaceWatcher.cpp
)

qt_add_resources(mailbox-gui "qml"
    PREFIX "/"
    FILES
        qml/Main.qml
        qml/BucketView.qml
        qml/FeedView.qml
        qml/ReadingView.qml
        qml/CommandLauncher.qml
        qml/SearchView.qml
        qml/RowActions.qml
        qml/ScreenerMoveMenu.qml
        qml/ComposerView.qml
        qml/LexxyEditor.qml
        qml/RecipientPills.qml
        qml/MailFormat.js
        qml/Triage.js
        qml/AppButton.qml
        qml/SendUndoToast.qml
        qml/FlashToast.qml
        qml/MailRow.qml
        qml/ThreadMessage.qml
        qml/WebRevive.qml
        qml/BottomStacks.qml
        qml/PileStack.qml
        qml/SectionLabel.qml
        qml/Avatar.qml
        qml/Pill.qml
        qml/Chip.qml
        qml/Kbd.qml
        qml/AttachmentChip.qml
        qml/QuickLook.qml
        qml/vendor/darkreader.js
        qml/vendor/feed.html
        qml/vendor/lexxy.js
        qml/vendor/lexxy.css
        icons/mailbox-gui.svg
)

target_include_directories(mailbox-gui PRIVATE src)
target_link_libraries(mailbox-gui PRIVATE
    Qt6::Core Qt6::Gui Qt6::Qml Qt6::Quick Qt6::QuickControls2 Qt6::Network Qt6::WebEngineQuick Qt6::PdfQuick Qt6::QuickDialogs2 Qt6::Multimedia)

# `cmake --install build --prefix ~/.local` drops the binary on PATH as
# `mailbox-gui` — the name the mailbox.email notification widget shells out
# to by default when you click a message — plus the .desktop entry and icon
# so the launcher can find it.
include(GNUInstallDirs)
install(TARGETS mailbox-gui RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR})
install(FILES share/applications/mailbox-gui.desktop
        DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/applications)
install(FILES icons/mailbox-gui.svg
        DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/icons/hicolor/scalable/apps)
install(FILES icons/mailbox-gui.png
        DESTINATION ${CMAKE_INSTALL_DATAROOTDIR}/icons/hicolor/256x256/apps)
