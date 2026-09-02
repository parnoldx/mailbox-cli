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
#include "SurfaceWatcher.hpp"

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
    // The two hand-tended piles shown as stacks along the bottom of the Inbox.
    MailModel asideModel;
    MailModel replyLaterModel;
    // Results of the full-screen search overlay — its own model so opening a
    // hit and coming back does not leave the bucket list showing the results.
    MailModel searchModel;

    auto *pixelBlock = new PixelBlock(&app);
    QWebEngineProfile::defaultProfile()->setUrlRequestInterceptor(pixelBlock);

    SurfaceWatcher surfaceWatcher;

    QQmlApplicationEngine engine;
    client.setJsEngine(&engine);
    QQmlContext *ctx = engine.rootContext();
    ctx->setContextProperty("Theme", &theme);
    ctx->setContextProperty("Mailbox", &client);
    ctx->setContextProperty("listModel", &listModel);
    ctx->setContextProperty("asideModel", &asideModel);
    ctx->setContextProperty("replyLaterModel", &replyLaterModel);
    ctx->setContextProperty("searchModel", &searchModel);
    ctx->setContextProperty("PixelBlock", pixelBlock);
    ctx->setContextProperty("SurfaceWatcher", &surfaceWatcher);

    // The vendored Dark Reader engine, handed to QML as a string so ReadingView
    // can inline it into the HTML-mail document it builds for WebEngine.
    QString darkReaderJs;
    if (QFile f(":/qml/vendor/darkreader.js"); f.open(QIODevice::ReadOnly | QIODevice::Text))
        darkReaderJs = QString::fromUtf8(f.readAll());
    ctx->setContextProperty("DarkReaderJs", darkReaderJs);

    // The vendored Lexxy editor (github.com/basecamp/lexxy), self-contained ESM
    // bundle + stylesheet, inlined by LexxyEditor.qml into the compose editor's
    // WebEngine document.
    auto readResource = [](const char *path) {
        QFile f(QString::fromLatin1(path));
        return f.open(QIODevice::ReadOnly | QIODevice::Text) ? QString::fromUtf8(f.readAll()) : QString();
    };
    ctx->setContextProperty("LexxyJs", readResource(":/qml/vendor/lexxy.js"));
    ctx->setContextProperty("LexxyCss", readResource(":/qml/vendor/lexxy.css"));

    QObject::connect(
        &engine, &QQmlApplicationEngine::objectCreationFailed, &app,
        [] { QCoreApplication::exit(-1); }, Qt::QueuedConnection);
    engine.load(QUrl("qrc:/qml/Main.qml"));
    if (engine.rootObjects().isEmpty())
        return -1;

    return app.exec();
}
