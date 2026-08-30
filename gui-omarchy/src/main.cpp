#include <QGuiApplication>
#include <QtWebEngineQuick/QtWebEngineQuick>
#include <QWebEngineProfile>
#include <QFile>
#include <QFont>
#include <QFontDatabase>
#include <QIcon>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickStyle>

#include "MailModel.hpp"
#include "MailboxClient.hpp"
#include "OmarchyTheme.hpp"
#include "PixelBlock.hpp"

int main(int argc, char *argv[]) {
    QtWebEngineQuick::initialize();
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

    auto *pixelBlock = new PixelBlock(&app);
    QWebEngineProfile::defaultProfile()->setUrlRequestInterceptor(pixelBlock);

    QQmlApplicationEngine engine;
    client.setJsEngine(&engine);
    QQmlContext *ctx = engine.rootContext();
    ctx->setContextProperty("Theme", &theme);
    ctx->setContextProperty("Mailbox", &client);
    ctx->setContextProperty("listModel", &listModel);
    ctx->setContextProperty("PixelBlock", pixelBlock);

    // The vendored Dark Reader engine, handed to QML as a string so ReadingView
    // can inline it into the HTML-mail document it builds for WebEngine.
    QString darkReaderJs;
    if (QFile f(":/qml/vendor/darkreader.js"); f.open(QIODevice::ReadOnly | QIODevice::Text))
        darkReaderJs = QString::fromUtf8(f.readAll());
    ctx->setContextProperty("DarkReaderJs", darkReaderJs);

    QObject::connect(
        &engine, &QQmlApplicationEngine::objectCreationFailed, &app,
        [] { QCoreApplication::exit(-1); }, Qt::QueuedConnection);
    engine.load(QUrl("qrc:/qml/Main.qml"));
    if (engine.rootObjects().isEmpty())
        return -1;

    return app.exec();
}
