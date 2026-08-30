#include <QGuiApplication>
#include <QFont>
#include <QFontDatabase>
#include <QIcon>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickStyle>

#include "MailModel.hpp"
#include "MailboxClient.hpp"
#include "OmarchyTheme.hpp"

int main(int argc, char *argv[]) {
    QGuiApplication app(argc, argv);
    app.setApplicationName("Mailbox");
    app.setApplicationDisplayName("Mailbox");
    QQuickStyle::setStyle("Basic");

    // Omarchy's own font. Fall back quietly if it is not installed.
    QFont base("JetBrainsMono Nerd Font");
    base.setStyleHint(QFont::Monospace);
    base.setPixelSize(13);
    app.setFont(base);

    OmarchyTheme theme;
    MailboxClient client;
    MailModel listModel;

    QQmlApplicationEngine engine;
    client.setJsEngine(&engine);
    QQmlContext *ctx = engine.rootContext();
    ctx->setContextProperty("Theme", &theme);
    ctx->setContextProperty("Mailbox", &client);
    ctx->setContextProperty("listModel", &listModel);

    QObject::connect(
        &engine, &QQmlApplicationEngine::objectCreationFailed, &app,
        [] { QCoreApplication::exit(-1); }, Qt::QueuedConnection);
    engine.load(QUrl("qrc:/qml/Main.qml"));
    if (engine.rootObjects().isEmpty())
        return -1;

    return app.exec();
}
