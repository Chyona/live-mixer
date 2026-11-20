// 文件作用：「导出成片」弹窗实现。
#include "VideoExportDialog.h"

#include "../core/FileManager.h"
#include "../core/MediaUtils.h"
#include "Controls.h"
#include "Icons.h"

#include <QClipboard>
#include <QFrame>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QLabel>
#include <QPainter>
#include <QVBoxLayout>

namespace {

constexpr double kMb = 1024.0 * 1024.0;

QString formatElapsed(qint64 milliseconds)
{
    const qint64 totalSeconds = qMax<qint64>(0, milliseconds) / 1000;
    return QStringLiteral("%1:%2")
        .arg(totalSeconds / 60, 2, 10, QLatin1Char('0'))
        .arg(totalSeconds % 60, 2, 10, QLatin1Char('0'));
}

QString formatEta(int seconds)
{
    if (seconds < 0) {
        return QStringLiteral("—");
    }
    if (seconds == 0) {
        return QStringLiteral("0 秒");
    }
    if (seconds < 60) {
        return QStringLiteral("%1 秒").arg(seconds);
    }
    const int minutes = seconds / 60;
    const int rest = seconds % 60;
    return rest == 0 ? QStringLiteral("%1 分").arg(minutes)
                     : QStringLiteral("%1 分 %2 秒").arg(minutes).arg(rest);
}

QString formatSpeed(double bytesPerSecond)
{
    if (bytesPerSecond <= 0.0) {
        return QStringLiteral("—");
    }
    return QStringLiteral("%1 MB/s").arg(bytesPerSecond / kMb, 0, 'f', 1);
}

void setLabelTextIfChanged(QLabel *label, const QString &text)
{
    if (label && label->text() != text) {
        label->setText(text);
    }
}

class IconBadge : public QWidget
{
public:
    IconBadge(Icons::Glyph glyph, const QColor &color, const QColor &background,
              int side, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_glyph(glyph)
        , m_color(color)
        , m_background(background)
    {
        setFixedSize(side, side);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_background.alpha() > 0) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(m_background);
            painter.drawEllipse(QRectF(rect()));
        }
        const qreal glyphSize = width() * 0.46;
        const QRectF glyphRect((width() - glyphSize) / 2.0,
                               (height() - glyphSize) / 2.0, glyphSize, glyphSize);
        Icons::paint(&painter, glyphRect, m_glyph, m_color, 2.0);
    }

private:
    Icons::Glyph m_glyph;
    QColor m_color;
    QColor m_background;
};

} // namespace

VideoExportDialog::VideoExportDialog(QWidget *parent)
    : ModalCard(QStringLiteral("导出成片"), 680, parent)
{
    setContentSpacing(0);
}

VideoExportDialog::Mode VideoExportDialog::modeFor(const VideoExportJob &job)
{
    if (job.status == VideoExportStatus::Done) {
        return Mode::Done;
    }
    if (job.status == VideoExportStatus::Error) {
        return Mode::Error;
    }
    return Mode::Running;
}

QVector<int> VideoExportDialog::stepStates(const VideoExportJob &job)
{
    if (job.status == VideoExportStatus::Done) {
        return {StageStepper::Done, StageStepper::Done, StageStepper::Done};
    }

    const int active = job.stage == VideoExportStage::Compose
        ? 0
        : (job.stage == VideoExportStage::Encode ? 1 : 2);
    QVector<int> states;
    for (int i = 0; i < 3; ++i) {
        if (i < active) {
            states.append(StageStepper::Done);
        } else if (i > active) {
            states.append(StageStepper::Waiting);
        } else {
            states.append(job.status == VideoExportStatus::Error
                              ? StageStepper::Error
                              : StageStepper::Active);
        }
    }
    return states;
}

QString VideoExportDialog::stageLabel(const VideoExportJob &job)
{
    if (job.status == VideoExportStatus::Error) {
        return QStringLiteral("导出已中断");
    }
    if (job.status == VideoExportStatus::Done) {
        return QStringLiteral("导出完成");
    }
    switch (job.stage) {
    case VideoExportStage::Compose:
        return QStringLiteral("正在合成画面");
    case VideoExportStage::Encode:
        return QStringLiteral("正在编码成片");
    case VideoExportStage::Download:
        return QStringLiteral("正在下载成片");
    case VideoExportStage::Done:
    default:
        return QStringLiteral("导出完成");
    }
}

void VideoExportDialog::setJob(const VideoExportJob &job)
{
    const Mode nextMode = modeFor(job);
    const bool structural = !m_built || m_job.draftRowId != job.draftRowId
        || nextMode != m_mode;

    m_job = job;
    m_mode = nextMode;
    m_built = true;

    if (structural) {
        rebuildContent();
        rebuildFooter();
        emit layoutChanged();
    } else {
        refresh();
    }
}

void VideoExportDialog::rebuildContent()
{
    while (QLayoutItem *item = contentLayout()->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }

    m_chip = nullptr;
    m_stepper = nullptr;
    m_stageLabel = nullptr;
    m_percentLabel = nullptr;
    m_progress = nullptr;
    m_detailLabel = nullptr;
    m_metaLabel = nullptr;
    m_outputSizeLabel = nullptr;

    auto *root = new QWidget(this);
    auto *layout = new QVBoxLayout(root);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    auto *head = new QWidget(root);
    auto *headLayout = new QHBoxLayout(head);
    headLayout->setContentsMargins(0, 0, 0, 0);
    headLayout->setSpacing(12);

    auto *textColumn = new QWidget(head);
    auto *textLayout = new QVBoxLayout(textColumn);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(2);
    auto *title = new ElidedLabel(m_job.title, textColumn);
    title->setPixelSize(13);
    title->setFontWeight(650);
    title->setTextColor(Theme::Text1);
    textLayout->addWidget(title);

    QStringList meta;
    if (!m_job.draftName.isEmpty()) {
        meta.append(m_job.draftName);
    }
    if (!m_job.rangeText.isEmpty()) {
        meta.append(QStringLiteral("源于 %1").arg(m_job.rangeText));
    }
    if (!m_job.durationText.isEmpty()) {
        meta.append(m_job.durationText);
    }
    auto *subtitle = new ElidedLabel(meta.join(QStringLiteral(" · ")), textColumn);
    subtitle->setPixelSize(11);
    subtitle->setMonoFont(true);
    subtitle->setTextColor(Theme::Text3);
    textLayout->addWidget(subtitle);
    headLayout->addWidget(textColumn, 1);

    const bool done = m_mode == Mode::Done;
    const bool failed = m_mode == Mode::Error;
    QString chipText = QStringLiteral("正在合成");
    Chip::Tone chipTone = Chip::Run;
    if (done) {
        chipText = QStringLiteral("已完成");
        chipTone = Chip::Ok;
    } else if (failed) {
        chipText = QStringLiteral("导出失败");
        chipTone = Chip::Error;
    } else if (m_job.stage == VideoExportStage::Encode) {
        chipText = QStringLiteral("正在编码");
    } else if (m_job.stage == VideoExportStage::Download) {
        chipText = QStringLiteral("正在下载");
    }
    m_chip = new Chip(chipText, chipTone, head);
    headLayout->addWidget(m_chip, 0, Qt::AlignTop);
    layout->addWidget(head);
    layout->addSpacing(12);

    if (m_mode == Mode::Running || m_mode == Mode::Error) {
        if (m_mode == Mode::Error) {
            layout->addWidget(buildErrorBanner(root));
        } else {
            const int expectedSeconds =
                qMax(1, int((m_job.expectedMs + 999) / 1000));
            auto *hint = new ElidedLabel(
                QStringLiteral("云端合成并导出 MP4 到本地，预计用时约 %1；"
                               "关闭窗口后导出会继续。")
                    .arg(formatEta(expectedSeconds)),
                root);
            hint->setPixelSize(11);
            hint->setTextColor(Theme::Text3);
            layout->addWidget(hint);
        }
        layout->addSpacing(12);

        m_stepper = new StageStepper(
            {QStringLiteral("合成画面"), QStringLiteral("编码成片"),
             QStringLiteral("下载到本地")},
            root);
        layout->addWidget(m_stepper);
        layout->addSpacing(12);

        auto *summary = new QWidget(root);
        auto *summaryLayout = new QVBoxLayout(summary);
        summaryLayout->setContentsMargins(0, 0, 0, 0);
        summaryLayout->setSpacing(0);

        auto *summaryTop = new QWidget(summary);
        auto *summaryTopLayout = new QHBoxLayout(summaryTop);
        summaryTopLayout->setContentsMargins(0, 0, 0, 0);
        summaryTopLayout->setSpacing(8);
        m_stageLabel = Ui::label(stageLabel(m_job), 12, 400, Theme::Text2, summaryTop);
        summaryTopLayout->addWidget(m_stageLabel);
        summaryTopLayout->addStretch(1);
        m_percentLabel = Ui::label(QString(), 17, 650, Theme::Primary, summaryTop);
        m_percentLabel->setFont(Theme::monoFont(17, 650));
        summaryTopLayout->addWidget(m_percentLabel);
        summaryLayout->addWidget(summaryTop);
        summaryLayout->addSpacing(8);

        m_progress = new ProgressBar(summary);
        m_progress->setFixedHeight(6);
        summaryLayout->addWidget(m_progress);
        summaryLayout->addSpacing(6);

        auto *summaryBottom = new QWidget(summary);
        auto *summaryBottomLayout = new QHBoxLayout(summaryBottom);
        summaryBottomLayout->setContentsMargins(0, 0, 0, 0);
        summaryBottomLayout->setSpacing(12);
        m_detailLabel = Ui::label(QString(), 10, 400, Theme::Text3, summaryBottom);
        m_detailLabel->setFont(Theme::monoFont(10));
        summaryBottomLayout->addWidget(m_detailLabel);
        summaryBottomLayout->addStretch(1);
        m_metaLabel = Ui::label(QString(), 10, 400, Theme::Text3, summaryBottom);
        m_metaLabel->setFont(Theme::monoFont(10));
        summaryBottomLayout->addWidget(m_metaLabel);
        summaryLayout->addWidget(summaryBottom);

        layout->addWidget(summary);
        layout->addSpacing(12);
        layout->addWidget(buildOutputInfo(root));
        if (m_mode == Mode::Error) {
            layout->addSpacing(10);
            auto *hint = new ElidedLabel(
                QStringLiteral("继续导出会从断点开始，已完成的画面不会重新合成。"),
                root);
            hint->setPixelSize(11);
            hint->setTextColor(Theme::Text3);
            layout->addWidget(hint);
        }
        layout->addStretch(1);
    } else {
        layout->addWidget(buildSuccessBanner(root));
        layout->addSpacing(12);
        layout->addWidget(buildOutputInfo(root));
        layout->addSpacing(10);
        auto *hint = new ElidedLabel(
            QStringLiteral("成片已保存到本地，可在文件管理器中查看或复制到其他位置。"),
            root);
        hint->setPixelSize(11);
        hint->setTextColor(Theme::Text3);
        layout->addWidget(hint);
        layout->addStretch(1);
    }

    contentLayout()->addWidget(root);
    root->show();
    contentLayout()->invalidate();
    refresh();
}

QWidget *VideoExportDialog::buildOutputInfo(QWidget *parent)
{
    auto *box = new QFrame(parent);
    box->setStyleSheet(QStringLiteral(
        "QFrame { background:#101218; border:1px solid #242938; border-radius:8px; }"));
    auto *layout = new QVBoxLayout(box);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    auto *spec = new QWidget(box);
    auto *specLayout = new QHBoxLayout(spec);
    specLayout->setContentsMargins(12, 7, 12, 7);
    specLayout->setSpacing(8);
    auto *specKey = Ui::label(QStringLiteral("输出规格"), 10, 400, Theme::Text3, spec);
    specKey->setFixedWidth(52);
    specLayout->addWidget(specKey);
    auto *specValue = Ui::label(
        QStringLiteral("MP4 · H.264 · 1080p · 30fps"), 10, 400, Theme::Text2, spec);
    specValue->setFont(Theme::monoFont(10));
    specLayout->addWidget(specValue, 1);
    m_outputSizeLabel = Ui::label(QString(), 10, 400, Theme::Text3, spec);
    m_outputSizeLabel->setFont(Theme::monoFont(10));
    specLayout->addWidget(m_outputSizeLabel);
    layout->addWidget(spec);

    auto *line = new QFrame(box);
    line->setFixedHeight(1);
    line->setStyleSheet(QStringLiteral("QFrame { background:#1D2230; border:none; }"));
    layout->addWidget(line);

    auto *pathRow = new QWidget(box);
    auto *pathLayout = new QHBoxLayout(pathRow);
    pathLayout->setContentsMargins(12, 7, 6, 7);
    pathLayout->setSpacing(8);
    auto *pathKey = Ui::label(QStringLiteral("输出位置"), 10, 400, Theme::Text3, pathRow);
    pathKey->setFixedWidth(52);
    pathLayout->addWidget(pathKey);
    auto *path = new ElidedLabel(m_job.outputPath, pathRow);
    path->setPixelSize(10);
    path->setMonoFont(true);
    path->setTextColor(Theme::Text2);
    pathLayout->addWidget(path, 1);

    auto *copy = new GlyphButton(Icons::Glyph::Copy, 20, pathRow);
    copy->setGlyphSize(11);
    copy->setColors(Theme::Text3, Theme::Primary);
    copy->setToolTipText(QStringLiteral("复制成片路径"));
    connect(copy, &QAbstractButton::clicked, this, [this] {
        QGuiApplication::clipboard()->setText(m_job.outputPath);
        emit toastRequested(QStringLiteral("已复制成片路径"));
    });
    pathLayout->addWidget(copy, 0, Qt::AlignVCenter);
    layout->addWidget(pathRow);
    return box;
}

QWidget *VideoExportDialog::buildSuccessBanner(QWidget *parent)
{
    auto *banner = new QFrame(parent);
    banner->setStyleSheet(QStringLiteral(
        "QFrame { background:rgba(62,207,142,0.08);"
        "border:1px solid rgba(62,207,142,0.30); border-radius:8px; }"));
    auto *layout = new QHBoxLayout(banner);
    layout->setContentsMargins(14, 12, 14, 12);
    layout->setSpacing(10);
    layout->addWidget(new IconBadge(Icons::Glyph::Check, Theme::OnAccent,
                                    Theme::Success, 28, banner),
                      0, Qt::AlignTop);

    auto *text = new QWidget(banner);
    auto *textLayout = new QVBoxLayout(text);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(2);
    textLayout->addWidget(
        Ui::label(QStringLiteral("成片已导出"), 13, 650, Theme::Success, text));
    textLayout->addWidget(Ui::label(
        QStringLiteral("%1 · MP4 1080p · 用时 %2")
            .arg(MediaUtils::formatByteSize(m_job.fileSizeBytes),
                 formatElapsed(m_job.elapsedMs)),
        11, 400, Theme::Text2, text));
    layout->addWidget(text, 1);
    return banner;
}

QWidget *VideoExportDialog::buildErrorBanner(QWidget *parent)
{
    auto *banner = new QFrame(parent);
    banner->setStyleSheet(QStringLiteral(
        "QFrame { background:rgba(246,70,93,0.08);"
        "border:1px solid rgba(246,70,93,0.30); border-radius:8px; }"));
    auto *layout = new QHBoxLayout(banner);
    layout->setContentsMargins(14, 12, 14, 12);
    layout->setSpacing(10);
    layout->addWidget(new IconBadge(Icons::Glyph::Alert, Theme::Danger,
                                    Theme::alpha(Theme::Danger, 0.16), 28, banner),
                      0, Qt::AlignTop);

    auto *text = new QWidget(banner);
    auto *textLayout = new QVBoxLayout(text);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(3);
    textLayout->addWidget(
        Ui::label(QStringLiteral("导出未完成"), 13, 650, Theme::Danger, text));
    QString reason = m_job.error;
    while (reason.endsWith(QChar(0x3002)) || reason.endsWith(QLatin1Char('.'))) {
        reason.chop(1);
    }
    if (reason.isEmpty()) {
        reason = QStringLiteral("云端合成中断");
    }
    auto *detail = new ElidedLabel(
        QStringLiteral("%1，导出已停止。已完成的合成结果会保留，继续导出将从断点开始。")
            .arg(reason),
        text);
    detail->setPixelSize(11);
    detail->setTextColor(Theme::Text2);
    textLayout->addWidget(detail);
    layout->addWidget(text, 1);
    return banner;
}

void VideoExportDialog::refresh()
{
    if (m_stepper) {
        m_stepper->setStates(stepStates(m_job));
    }
    if (m_chip && m_mode == Mode::Running) {
        const QString chipText = m_job.stage == VideoExportStage::Encode
            ? QStringLiteral("正在编码")
            : (m_job.stage == VideoExportStage::Download ? QStringLiteral("正在下载")
                                                         : QStringLiteral("正在合成"));
        m_chip->setText(chipText);
    }
    setLabelTextIfChanged(m_stageLabel, stageLabel(m_job));

    const bool done = m_mode == Mode::Done;
    const bool failed = m_mode == Mode::Error;
    const int percent =
        done ? 100 : qMin(99, int(qBound(0.0, m_job.progress, 100.0)));

    if (m_percentLabel) {
        setLabelTextIfChanged(m_percentLabel, QStringLiteral("%1%").arg(percent));
        const QString style =
            QStringLiteral("color:%1;background:transparent;")
                .arg(failed ? QStringLiteral("#F6465D")
                            : (done ? QStringLiteral("#3ECF8E")
                                    : QStringLiteral("#6E8BFF")));
        if (m_percentLabel->styleSheet() != style) {
            m_percentLabel->setStyleSheet(style);
        }
    }
    if (m_progress) {
        m_progress->setValue(int(qBound(0.0, m_job.progress, 100.0)));
        m_progress->setBarColor(failed ? Theme::Danger
                                       : (done ? Theme::Success : Theme::Primary));
    }

    QString detail;
    if (done) {
        detail = QStringLiteral("%1 · MP4 · 1080p")
                     .arg(MediaUtils::formatByteSize(m_job.fileSizeBytes));
    } else if (m_job.stage == VideoExportStage::Compose) {
        detail = QStringLiteral("已合成 %1 / %2 帧")
                     .arg(m_job.framesDone)
                     .arg(m_job.framesTotal);
    } else if (m_job.stage == VideoExportStage::Encode) {
        detail = QStringLiteral("已编码 %1 / %2 分段 · H.264")
                     .arg(m_job.segmentsDone)
                     .arg(m_job.segmentsTotal);
    } else if (m_job.stage == VideoExportStage::Download) {
        detail = QStringLiteral("%1 / %2 · %3")
                     .arg(MediaUtils::formatByteSize(m_job.downloadedBytes),
                          MediaUtils::formatByteSize(m_job.fileSizeBytes),
                          formatSpeed(m_job.speedBytesPerSecond));
    } else {
        detail = QStringLiteral("%1 · MP4 · 1080p")
                     .arg(MediaUtils::formatByteSize(m_job.fileSizeBytes));
    }
    setLabelTextIfChanged(m_detailLabel, detail);

    QString meta;
    if (m_mode == Mode::Running) {
        meta = QStringLiteral("已用 %1 · 预计还需 %2")
                   .arg(formatElapsed(m_job.elapsedMs), formatEta(m_job.etaSeconds));
    } else if (done) {
        meta = QStringLiteral("共用时 %1").arg(formatElapsed(m_job.elapsedMs));
    } else {
        meta = QStringLiteral("已用 %1").arg(formatElapsed(m_job.elapsedMs));
    }
    setLabelTextIfChanged(m_metaLabel, meta);
    if (m_outputSizeLabel) {
        setLabelTextIfChanged(
            m_outputSizeLabel,
            QStringLiteral("文件约 %1")
                .arg(MediaUtils::formatByteSize(m_job.fileSizeBytes)));
    }
}

void VideoExportDialog::rebuildFooter()
{
    QHBoxLayout *footer = footerLayout();
    while (QLayoutItem *item = footer->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }
    footer->addStretch(1);

    const bool windows = Theme::isWindowsWindowStyle();
    auto addActions = [footer, windows](QWidget *primary, QWidget *secondary) {
        if (windows) {
            footer->addWidget(primary);
            footer->addWidget(secondary);
        } else {
            footer->addWidget(secondary);
            footer->addWidget(primary);
        }
    };

    if (m_mode == Mode::Done) {
        auto *close = new Button(QStringLiteral("关闭"), Button::Default, this);
        connect(close, &QAbstractButton::clicked, this, [this] { emit closeRequested(); });

        auto *reveal = new Button(FileManager::revealPathLabel(), Button::Primary, this);
        reveal->setLeadingGlyph(Icons::Glyph::Folder);
        connect(reveal, &QAbstractButton::clicked, this, [this] {
            emit revealRequested(m_job.outputPath);
        });
        addActions(reveal, close);
        return;
    }

    if (m_mode == Mode::Error) {
        auto *close = new Button(QStringLiteral("关闭"), Button::Default, this);
        connect(close, &QAbstractButton::clicked, this, [this] { emit closeRequested(); });

        auto *retry = new Button(QStringLiteral("继续导出"), Button::Primary, this);
        connect(retry, &QAbstractButton::clicked, this, [this] {
            emit retryRequested(m_job.draftRowId);
        });
        addActions(retry, close);
        return;
    }

    auto *hint = Ui::label(QStringLiteral("关闭窗口后导出会继续"), 10, 400, Theme::Text3,
                           this);
    footer->insertWidget(0, hint);
    footer->setStretch(0, 0);
    footer->setStretch(1, 1);

    auto *background = new Button(QStringLiteral("后台继续"), Button::Primary, this);
    connect(background, &QAbstractButton::clicked, this, [this] { emit closeRequested(); });
    auto *cancel = new Button(QStringLiteral("取消导出"), Button::Ghost, this);
    connect(cancel, &QAbstractButton::clicked, this, [this] {
        emit cancelRequested(m_job.draftRowId);
    });
    addActions(background, cancel);
}
