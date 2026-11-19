// 文件作用：应用入口，初始化日志、主题与主窗口，并处理开发预览参数。
#include "app/ApplicationStyle.h"
#include "app/MainWindow.h"
#include "core/Logger.h"
#include "ui/Icons.h"

#include <QAbstractButton>
#include <QApplication>
#include <QCommandLineOption>
#include <QCommandLineParser>
#include <QPixmap>
#include <QScrollArea>
#include <QScrollBar>
#include <QTimer>

int main(int argc, char *argv[])
{
    QApplication app(argc, argv);
    QApplication::setOrganizationName(QStringLiteral("LiveMix"));
    QApplication::setApplicationName(QStringLiteral("LiveMix Studio"));
    QApplication::setApplicationDisplayName(QStringLiteral("LiveMix Studio"));
    QApplication::setApplicationVersion(QStringLiteral(LIVEMIX_VERSION));

    if (!Logger::install()) {
        qWarning() << "Unable to write log files under" << Logger::logDirectory();
    }
    qInfo() << "LiveMix Studio" << QApplication::applicationVersion()
            << "starting; log directory:" << Logger::logDirectory();

    Theme::apply(app);
    app.setWindowIcon(Icons::appIcon());

    QCommandLineParser parser;
    parser.setApplicationDescription(QStringLiteral("LiveMix Studio desktop client"));
    parser.addHelpOption();
    parser.addVersionOption();

    QCommandLineOption pageOption(
        QStringLiteral("page"),
        QStringLiteral("Open a specific page for preview "
                       "(dash/material/workspace/pipeline/prompt/tasks/settings/spec)."),
        QStringLiteral("page"));
    QCommandLineOption modalOption(
        QStringLiteral("modal"),
        QStringLiteral("Open a modal or the prompt page for preview "
                       "(add/add-local/start/prompt/viewer)."),
        QStringLiteral("modal"));
    QCommandLineOption fullscreenOption(
        QStringLiteral("fullscreen"),
        QStringLiteral("Open the full-screen prompt editor (edit/view)."),
        QStringLiteral("mode"));
    QCommandLineOption pipelineStepOption(
        QStringLiteral("pipeline-step"),
        QStringLiteral("Open the generation pipeline at a specific step (1-3)."),
        QStringLiteral("step"));
    QCommandLineOption clickOption(
        QStringLiteral("click"),
        QStringLiteral("Click buttons with these labels in order after launch, "
                       "comma separated (development preview)."),
        QStringLiteral("labels"));
    QCommandLineOption scrollBottomOption(
        QStringLiteral("scroll-bottom"),
        QStringLiteral("Scroll the visible page to the bottom (development preview)."));
    QCommandLineOption screenshotOption(
        QStringLiteral("screenshot"),
        QStringLiteral("Grab the window to a PNG file and exit (development preview)."),
        QStringLiteral("path"));
    QCommandLineOption screenshotDelayOption(
        QStringLiteral("screenshot-delay"),
        QStringLiteral("Delay before taking the screenshot in milliseconds "
                       "(development preview, default 650)."),
        QStringLiteral("ms"), QStringLiteral("650"));
    QCommandLineOption localFileOption(
        QStringLiteral("local-file"),
        QStringLiteral("Preselect a local video in the add-source dialog "
                       "(development preview)."),
        QStringLiteral("path"));
    parser.addOptions({pageOption, modalOption, fullscreenOption, pipelineStepOption,
                       clickOption, scrollBottomOption, screenshotOption,
                       screenshotDelayOption, localFileOption});
    parser.process(app);

    MainWindow window;
    window.setPreviewLocalFile(parser.value(localFileOption));
    if (parser.isSet(pageOption)) {
        window.showPageForPreview(parser.value(pageOption));
    }
    if (parser.isSet(pipelineStepOption)) {
        window.showPipelineStepForPreview(parser.value(pipelineStepOption).toInt() - 1);
    }
    window.show();

    if (parser.isSet(modalOption)) {
        const QString modal = parser.value(modalOption);
        QTimer::singleShot(120, &window, [&window, modal] {
            window.showModalForPreview(modal);
        });
    }
    if (parser.isSet(fullscreenOption)) {
        const bool readOnly = parser.value(fullscreenOption) == QLatin1String("view");
        QTimer::singleShot(120, &window, [&window, readOnly] {
            window.showFullscreenForPreview(readOnly);
        });
    }
    if (parser.isSet(clickOption)) {
        const QStringList labels =
            parser.value(clickOption).split(QLatin1Char(','), Qt::SkipEmptyParts);
        for (int i = 0; i < labels.size(); ++i) {
            const QString label = labels.at(i).trimmed();
            QTimer::singleShot(260 + i * 600, &window, [&window, label] {
                const QList<QAbstractButton *> buttons =
                    window.findChildren<QAbstractButton *>();
                for (QAbstractButton *button : buttons) {
                    if (button->isVisible() && button->text() == label) {
                        button->click();
                        break;
                    }
                }
            });
        }
    }
    if (parser.isSet(scrollBottomOption)) {
        QTimer::singleShot(300, &window, [&window] {
            const QList<QScrollArea *> areas = window.findChildren<QScrollArea *>();
            for (QScrollArea *area : areas) {
                if (area->isVisible() && area->verticalScrollBar()) {
                    area->verticalScrollBar()->setValue(area->verticalScrollBar()->maximum());
                    break;
                }
            }
        });
    }

    if (parser.isSet(screenshotOption)) {
        const QString path = parser.value(screenshotOption);
        int delay = parser.value(screenshotDelayOption).toInt();
        if (delay <= 0) {
            delay = 650;
        }
        QTimer::singleShot(delay, &window, [&window, path] {
            window.grab().save(path);
            QCoreApplication::quit();
        });
    }

    return app.exec();
}
