cmake_minimum_required(VERSION 3.20)
project(mailbox-gui LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_AUTOMOC ON)
set(CMAKE_AUTORCC ON)
set(CMAKE_AUTOUIC ON)

find_package(Qt6 REQUIRED COMPONENTS
    Core
    Gui
    Qml
    Quick
    QuickControls2
    WebEngineQuick
    WebChannel
    Network
)

add_executable(mailbox-gui
    src/main.cpp
    src/MailboxClient.hpp
    src/MailboxClient.cpp
    src/MessageListModel.hpp
    src/MessageListModel.cpp
    src/ContactSuggestModel.hpp
    src/ContactSuggestModel.cpp
    src/PixelBlockInterceptor.hpp
    src/PixelBlockInterceptor.cpp
    src/SendManager.hpp
    src/SendManager.cpp
    src/ThemeBridge.hpp
    src/ThemeBridge.cpp
    resources.qrc
)

target_link_libraries(mailbox-gui PRIVATE
    Qt6::Core
    Qt6::Gui
    Qt6::Qml
    Qt6::Quick
    Qt6::QuickControls2
    Qt6::WebEngineQuick
    Qt6::WebChannel
    Qt6::Network
)

target_include_directories(mailbox-gui PRIVATE src)
