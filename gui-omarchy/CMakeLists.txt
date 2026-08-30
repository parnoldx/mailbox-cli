cmake_minimum_required(VERSION 3.20)
project(mailbox-omarchy LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)

find_package(Qt6 REQUIRED COMPONENTS Core Gui Qml Quick QuickControls2 Network WebEngineQuick PdfQuick QuickDialogs2)

qt_add_executable(mailbox-omarchy
    src/main.cpp
    src/OmarchyTheme.hpp src/OmarchyTheme.cpp
    src/MailboxClient.hpp src/MailboxClient.cpp
    src/MailModel.hpp src/MailModel.cpp
    src/PixelBlock.hpp src/PixelBlock.cpp
)

qt_add_resources(mailbox-omarchy "qml"
    PREFIX "/"
    FILES
        qml/Main.qml
        qml/BucketView.qml
        qml/ReadingView.qml
        qml/CommandLauncher.qml
        qml/MailRow.qml
        qml/SectionLabel.qml
        qml/Avatar.qml
        qml/Pill.qml
        qml/Kbd.qml
        qml/AttachmentChip.qml
        qml/QuickLook.qml
        qml/vendor/darkreader.js
)

target_include_directories(mailbox-omarchy PRIVATE src)
target_link_libraries(mailbox-omarchy PRIVATE
    Qt6::Core Qt6::Gui Qt6::Qml Qt6::Quick Qt6::QuickControls2 Qt6::Network Qt6::WebEngineQuick Qt6::PdfQuick Qt6::QuickDialogs2)
