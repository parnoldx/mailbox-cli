#include <QGuiApplication>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QtWebEngineQuick/qtwebenginequickglobal.h>
#include <QWebEngineProfile>
#include <QIcon>

#include "MailboxClient.hpp"
#include "MessageListModel.hpp"
#include "ContactSuggestModel.hpp"
#include "PixelBlockInterceptor.hpp"
#include "SendManager.hpp"
#include "ThemeBridge.hpp"

int main(int argc, char *argv[]) {
    // WebEngine must be initialized before QGuiApplication
    QtWebEngineQuick::initialize();

    QGuiApplication app(argc, argv);
    app.setApplicationName("Mailbox");
    app.setOrganizationName("Mailbox");
    app.setOrganizationDomain("mailbox.org");

    QQmlApplicationEngine engine;

    auto *mailboxClient = new MailboxClient(&app);
    auto *messageListModel = new MessageListModel(&app);
    auto *contactSuggestModel = new ContactSuggestModel(&app);
    auto *pixelBlockInterceptor = new PixelBlockInterceptor(&app);
    auto *sendManager = new SendManager(mailboxClient, &app);
    auto *themeBridge = new ThemeBridge(&app);

    // Install the PixelBlock tracker stripper on the default WebEngine profile
    QWebEngineProfile::defaultProfile()->setUrlRequestInterceptor(pixelBlockInterceptor);

    // Connect mailbox client data updates to message list model
    QObject::connect(mailboxClient, &MailboxClient::boxLoaded, messageListModel, [messageListModel](const QString &box, const QJsonArray &messages) {
        Q_UNUSED(box);
        messageListModel->loadFromMessages(messages);
    });

    QQmlContext *ctx = engine.rootContext();
    ctx->setContextProperty("mailboxClient", mailboxClient);
    ctx->setContextProperty("messageListModel", messageListModel);
    ctx->setContextProperty("contactSuggestModel", contactSuggestModel);
    ctx->setContextProperty("pixelBlockInterceptor", pixelBlockInterceptor);
    ctx->setContextProperty("sendManager", sendManager);
    ctx->setContextProperty("themeBridge", themeBridge);
    ctx->setContextProperty("Theme", themeBridge);

    const QUrl url(QStringLiteral("qrc:/qml/Main.qml"));
    QObject::connect(&engine, &QQmlApplicationEngine::objectCreated,
                     &app, [url](QObject *obj, const QUrl &objUrl) {
        if (!obj && url == objUrl)
            QCoreApplication::exit(-1);
    }, Qt::QueuedConnection);

    engine.load(url);

    return app.exec();
}
