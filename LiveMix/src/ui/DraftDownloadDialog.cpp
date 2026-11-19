// 文件作用：草稿下载弹窗实现。
#include "DraftDownloadDialog.h"

#include "../core/FileManager.h"
#include "../core/MediaUtils.h"
#include "Controls.h"
#include "Icons.h"

#include <QClipboard>
#include <QFrame>
#include <QFontMetrics>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QLabel>
#include <QPainter>
#include <QScrollArea>
#include <QVBoxLayout>

namespace {

QString fileKindLabel(DraftFileKind kind)
{
    switch (kind) {
    case DraftFileKind::Config:
        return QStringLiteral("配置");
    case DraftFileKind::Video:
        return QStringLiteral("视频");
    case DraftFileKind::Audio:
        return QStringLiteral("音频");
    case DraftFileKind::Image:
        return QStringLiteral("封面");
    }
    return QStringLiteral("文件");
}

void setLabelTextIfChanged(QLabel *label, const QString &text)
{
    if (label && label->text() != text) {
        label->setText(text);
    }
}

QString formatSpeed(double bytesPerSecond)
{
    constexpr double kMb = 1024.0 * 1024.0;
    constexpr double kKb = 1024.0;
    if (bytesPerSecond >= kMb) {
        return QStringLiteral("%1 MB/s").arg(bytesPerSecond / kMb, 0, 'f', 1);
    }
    return QStringLiteral("%1 KB/s")
        .arg(qMax(1.0, bytesPerSecond / kKb), 0, 'f', 0);
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

// -----------------------------------------------------------------------------
// 阶段指示器：下载配置 → 下载素材 → 写入草稿目录
// -----------------------------------------------------------------------------

class DraftStageStepper : public QWidget
{
public:
    enum State { Waiting, Active, Done, Error };

    explicit DraftStageStepper(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(24);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

    void setStates(const QVector<int> &states)
    {
        if (m_states == states) {
            return;
        }
        m_states = states;
        update();
    }

    QSize sizeHint() const override { return QSize(420, 24); }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setRenderHint(QPainter::TextAntialiasing, true);

        static const QStringList labels = {
            QStringLiteral("下载草稿配置"),
            QStringLiteral("下载素材"),
            QStringLiteral("写入草稿目录"),
        };
        const int sizes[3] = {18, 18, 18};
        const int gap = 6;
        const QFont labelFont = Theme::font(11);
        const QFontMetrics metrics(labelFont);
        int labelWidths[3];
        int used = 0;
        for (int i = 0; i < 3; ++i) {
            labelWidths[i] = metrics.horizontalAdvance(labels.at(i));
            used += sizes[i] + gap + labelWidths[i];
        }
        const int lineSpace = qMax(14, (width() - used) / 2);

        int x = 0;
        for (int i = 0; i < 3; ++i) {
            const int state = m_states.value(i, Waiting);
            QColor fill;
            QColor border;
            QColor textColor;
            int weight = QFont::Normal;
            switch (state) {
            case Done:
                fill = Theme::Success;
                border = Theme::Success;
                textColor = Theme::Text2;
                break;
            case Active:
                fill = Theme::Primary;
                border = Theme::Primary;
                textColor = Theme::Text1;
                weight = 650;
                break;
            case Error:
                fill = Theme::alpha(Theme::Danger, 0.16);
                border = Theme::Danger;
                textColor = Theme::Danger;
                break;
            case Waiting:
            default:
                fill = Theme::Surface2;
                border = Theme::BorderStrong;
                textColor = Theme::Text3;
                break;
            }

            const QRectF circle(x, (height() - sizes[i]) / 2.0, sizes[i], sizes[i]);
            painter.setPen(QPen(border, 1.0));
            painter.setBrush(fill);
            painter.drawEllipse(circle.adjusted(0.5, 0.5, -0.5, -0.5));

            if (state == Done) {
                Icons::paint(&painter, circle.adjusted(5, 5, -5, -5), Icons::Glyph::Check,
                             Theme::OnAccent, 2.0);
            } else {
                painter.setFont(Theme::monoFont(10));
                painter.setPen(state == Error ? Theme::Danger : Theme::OnAccent);
                painter.drawText(circle, Qt::AlignCenter,
                                 state == Error ? QStringLiteral("!")
                                                : QString::number(i + 1));
            }

            painter.setFont(Theme::font(11, weight));
            painter.setPen(textColor);
            painter.drawText(QRectF(x + sizes[i] + gap, 0, labelWidths[i], height()),
                             Qt::AlignLeft | Qt::AlignVCenter, labels.at(i));

            x += sizes[i] + gap + labelWidths[i];
            if (i < 2) {
                const QColor lineColor =
                    state == Done ? Theme::alpha(Theme::Success, 0.6) : Theme::Border;
                painter.setPen(QPen(lineColor, 1.0));
                painter.drawLine(QPointF(x + 6, height() / 2.0),
                                 QPointF(x + lineSpace - 6, height() / 2.0));
                x += lineSpace;
            }
        }
    }

private:
    QVector<int> m_states{Waiting, Waiting, Waiting};
};

// -----------------------------------------------------------------------------
// 文件行
// -----------------------------------------------------------------------------

class DraftFileRow : public QWidget
{
public:
    explicit DraftFileRow(const DraftFileProgress &file, QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setFixedHeight(30);
        auto *layout = new QHBoxLayout(this);
        layout->setContentsMargins(12, 4, 12, 4);
        layout->setSpacing(10);

        auto *kind = new QLabel(fileKindLabel(file.kind), this);
        kind->setAlignment(Qt::AlignCenter);
        kind->setFixedSize(34, 18);
        kind->setFont(Theme::font(9, 500));
        kind->setStyleSheet(QStringLiteral(
            "color:#62697E;background:#1A1E28;border:1px solid #343B4E;"
            "border-radius:4px;"));
        layout->addWidget(kind, 0, Qt::AlignVCenter);

        m_path = new ElidedLabel(file.path, this);
        m_path->setPixelSize(11);
        m_path->setMonoFont(true);
        m_path->setTextColor(Theme::Text2);
        layout->addWidget(m_path, 1);

        m_size = new QLabel(this);
        m_size->setFixedWidth(58);
        m_size->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        m_size->setFont(Theme::monoFont(10));
        m_size->setStyleSheet(QStringLiteral("color:#62697E;background:transparent;"));
        layout->addWidget(m_size, 0, Qt::AlignVCenter);

        auto *statusBox = new QWidget(this);
        statusBox->setFixedWidth(104);
        auto *statusLayout = new QVBoxLayout(statusBox);
        statusLayout->setContentsMargins(0, 0, 0, 0);
        statusLayout->setSpacing(3);
        m_status = new QLabel(statusBox);
        m_status->setAlignment(Qt::AlignRight | Qt::AlignVCenter);
        m_status->setFont(Theme::font(10));
        m_status->setStyleSheet(QStringLiteral("background:transparent;"));
        statusLayout->addWidget(m_status);
        m_miniBar = new ProgressBar(statusBox);
        m_miniBar->setFixedHeight(2);
        statusLayout->addWidget(m_miniBar, 0, Qt::AlignRight | Qt::AlignVCenter);
        layout->addWidget(statusBox, 0, Qt::AlignVCenter);

        update(file);
    }

    void update(const DraftFileProgress &file)
    {
        // 状态切换才改样式，进度刷新只更新数字，避免每帧重新计算样式表。
        if (!m_statusInitialized || file.status != m_lastStatus) {
            m_statusInitialized = true;
            m_lastStatus = file.status;
            m_background = Qt::transparent;
            switch (file.status) {
            case DraftFileStatus::Downloading:
                m_background = Theme::alpha(Theme::Primary, 0.07);
                m_path->setTextColor(Theme::Text1);
                m_status->setStyleSheet(
                    QStringLiteral("color:#6E8BFF;background:transparent;"));
                m_miniBar->setBarColor(Theme::Primary);
                m_miniBar->setVisible(true);
                break;
            case DraftFileStatus::Done:
                m_path->setTextColor(Theme::Text2);
                m_status->setText(QStringLiteral("已下载"));
                m_status->setStyleSheet(
                    QStringLiteral("color:#3ECF8E;background:transparent;"));
                m_miniBar->setVisible(false);
                break;
            case DraftFileStatus::Error:
                m_background = Theme::alpha(Theme::Danger, 0.07);
                m_path->setTextColor(Theme::Text1);
                m_status->setText(QStringLiteral("下载失败"));
                m_status->setStyleSheet(
                    QStringLiteral("color:#F6465D;background:transparent;"));
                m_miniBar->setVisible(false);
                break;
            case DraftFileStatus::Queued:
            default:
                m_path->setTextColor(Theme::Text3);
                m_status->setText(QStringLiteral("排队中"));
                m_status->setStyleSheet(
                    QStringLiteral("color:#62697E;background:transparent;"));
                m_miniBar->setVisible(false);
                break;
            }
            QWidget::update();
        }

        if (file.status == DraftFileStatus::Downloading) {
            m_status->setText(
                QStringLiteral("%1%").arg(qBound(0, file.progress, 100)));
            m_miniBar->setValue(file.progress);
        }

        QString sizeText;
        if (file.totalBytes >= 0) {
            sizeText = MediaUtils::formatByteSize(file.totalBytes);
        } else if (file.loadedBytes > 0) {
            sizeText = MediaUtils::formatByteSize(file.loadedBytes);
        } else {
            sizeText = QStringLiteral("—");
        }
        if (m_size->text() != sizeText) {
            m_size->setText(sizeText);
        }
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        if (m_background.alpha() > 0) {
            painter.fillRect(rect(), m_background);
        }
        painter.setPen(QPen(Theme::alpha(Theme::Border, 0.7), 1.0));
        painter.drawLine(0, height() - 1, width(), height() - 1);
    }

private:
    ElidedLabel *m_path = nullptr;
    QLabel *m_size = nullptr;
    QLabel *m_status = nullptr;
    ProgressBar *m_miniBar = nullptr;
    QColor m_background;
    DraftFileStatus m_lastStatus = DraftFileStatus::Queued;
    bool m_statusInitialized = false;
};

// -----------------------------------------------------------------------------
// DraftDownloadDialog
// -----------------------------------------------------------------------------

DraftDownloadDialog::DraftDownloadDialog(QWidget *parent)
    : ModalCard(FileManager::revealLabel(), 680, parent)
{
    setContentSpacing(0);
}

DraftDownloadDialog::Mode DraftDownloadDialog::modeFor(const DraftDownloadJob &job)
{
    if (job.status == DraftDownloadStatus::Done) {
        return Mode::Done;
    }
    if (job.status == DraftDownloadStatus::Error) {
        return Mode::Error;
    }
    return Mode::Running;
}

QString DraftDownloadDialog::stageText(const DraftDownloadJob &job)
{
    if (job.status == DraftDownloadStatus::Error) {
        return QStringLiteral("下载已中断");
    }
    if (job.status == DraftDownloadStatus::Done) {
        return QStringLiteral("下载完成");
    }
    switch (job.stage) {
    case DraftDownloadStage::Writing:
        return QStringLiteral("正在写入剪映草稿目录");
    case DraftDownloadStage::Assets:
        return QStringLiteral("正在下载素材文件");
    case DraftDownloadStage::Config:
    case DraftDownloadStage::Done:
    default:
        return QStringLiteral("正在下载草稿配置");
    }
}

QString DraftDownloadDialog::chipText(const DraftDownloadJob &job)
{
    if (job.status == DraftDownloadStatus::Done) {
        return QStringLiteral("已完成");
    }
    if (job.status == DraftDownloadStatus::Error) {
        return QStringLiteral("下载失败");
    }
    if (job.status == DraftDownloadStatus::Writing) {
        return QStringLiteral("正在写入");
    }
    return QStringLiteral("正在准备");
}

int DraftDownloadDialog::chipTone(const DraftDownloadJob &job)
{
    if (job.status == DraftDownloadStatus::Done) {
        return Chip::Ok;
    }
    if (job.status == DraftDownloadStatus::Error) {
        return Chip::Error;
    }
    return Chip::Run;
}

QVector<int> DraftDownloadDialog::stepStates(const DraftDownloadJob &job)
{
    if (job.status == DraftDownloadStatus::Done) {
        return {DraftStageStepper::Done, DraftStageStepper::Done,
                DraftStageStepper::Done};
    }
    if (job.status == DraftDownloadStatus::Error) {
        if (job.stage == DraftDownloadStage::Writing) {
            return {DraftStageStepper::Done, DraftStageStepper::Done,
                    DraftStageStepper::Error};
        }
        if (job.stage == DraftDownloadStage::Config) {
            return {DraftStageStepper::Error, DraftStageStepper::Waiting,
                    DraftStageStepper::Waiting};
        }
        return {DraftStageStepper::Done, DraftStageStepper::Error,
                DraftStageStepper::Waiting};
    }
    if (job.status == DraftDownloadStatus::Writing) {
        return {DraftStageStepper::Done, DraftStageStepper::Done,
                DraftStageStepper::Active};
    }
    if (job.stage == DraftDownloadStage::Config) {
        return {DraftStageStepper::Active, DraftStageStepper::Waiting,
                DraftStageStepper::Waiting};
    }
    return {DraftStageStepper::Done, DraftStageStepper::Active,
            DraftStageStepper::Waiting};
}

void DraftDownloadDialog::setJob(const DraftDownloadJob &job)
{
    const Mode nextMode = modeFor(job);
    const bool structural = m_job.draftRowId != job.draftRowId || nextMode != m_mode
        || job.files.size() != m_job.files.size() || job.stage != m_job.stage;

    m_job = job;
    m_mode = nextMode;

    if (structural) {
        rebuildContent();
        rebuildFooter();
        emit layoutChanged();
    } else {
        refresh();
    }
}

void DraftDownloadDialog::rebuildContent()
{
    while (QLayoutItem *item = contentLayout()->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }

    m_stepper = nullptr;
    m_stageLabel = nullptr;
    m_percentLabel = nullptr;
    m_progress = nullptr;
    m_bytesLabel = nullptr;
    m_metaLabel = nullptr;
    m_fileRows.clear();
    m_fileIndexes.clear();
    m_groupHeaders.clear();
    m_fileScroll = nullptr;
    m_lastActiveRow = -1;

    auto *root = new QWidget(this);
    auto *layout = new QVBoxLayout(root);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    // 标题、草稿名与状态徽标。
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
    auto *subtitle =
        new ElidedLabel(meta.join(QStringLiteral(" · ")), textColumn);
    subtitle->setPixelSize(11);
    subtitle->setMonoFont(true);
    subtitle->setTextColor(Theme::Text3);
    textLayout->addWidget(subtitle);
    headLayout->addWidget(textColumn, 1);
    headLayout->addWidget(
        new Chip(chipText(m_job), static_cast<Chip::Tone>(chipTone(m_job)), head), 0,
        Qt::AlignTop);
    layout->addWidget(head);
    layout->addSpacing(12);

    if (m_mode == Mode::Running || m_mode == Mode::Error) {
        if (m_mode == Mode::Error) {
            layout->addWidget(buildErrorBanner(root));
            layout->addSpacing(12);
        } else {
            // 单行省略标签：避免自动换行导致弹窗高度估算偏小、内容互相重叠。
            auto *hint = new ElidedLabel(
                QStringLiteral("打开前需把草稿与素材下载到本地剪映目录，视网络情况约需 "
                               "1 分钟；完成后自动定位到草稿文件夹。"),
                root);
            hint->setPixelSize(11);
            hint->setTextColor(Theme::Text3);
            layout->addWidget(hint);
            layout->addSpacing(12);
        }

        m_stepper = new DraftStageStepper(root);
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
        m_stageLabel = Ui::label(stageText(m_job), 12, 400, Theme::Text2, summaryTop);
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
        m_bytesLabel = Ui::label(QString(), 10, 400, Theme::Text3, summaryBottom);
        m_bytesLabel->setFont(Theme::monoFont(10));
        summaryBottomLayout->addWidget(m_bytesLabel);
        summaryBottomLayout->addStretch(1);
        m_metaLabel = Ui::label(QString(), 10, 400, Theme::Text3, summaryBottom);
        m_metaLabel->setFont(Theme::monoFont(10));
        summaryBottomLayout->addWidget(m_metaLabel);
        summaryLayout->addWidget(summaryBottom);

        layout->addWidget(summary);
        layout->addSpacing(12);
        layout->addWidget(buildFileList(root));
        if (m_mode == Mode::Error && m_job.doneFiles > 0) {
            layout->addSpacing(10);
            auto *hint = new ElidedLabel(
                QStringLiteral(
                    "继续下载只会补齐失败与未完成的文件，已完成的部分会保留。"),
                root);
            hint->setPixelSize(11);
            hint->setTextColor(Theme::Text3);
            layout->addWidget(hint);
        }
        layout->addStretch(1);
    } else {
        layout->addWidget(buildSuccessBanner(root));
        layout->addSpacing(12);
        layout->addWidget(buildFileList(root));
        layout->addSpacing(10);
        auto *hint = new ElidedLabel(
            QStringLiteral("已自动定位到草稿文件夹，在剪映草稿列表中打开该草稿即可继续编辑。"),
            root);
        hint->setPixelSize(11);
        hint->setTextColor(Theme::Text3);
        layout->addWidget(hint);
        layout->addStretch(1);
    }

    contentLayout()->addWidget(root);
    // 弹窗已经显示时，新建内容默认是隐藏的，布局会跳过它导致高度估算为 0；
    // 显式 show 后 sizeHint 才包含新内容，随后由 layoutChanged 触发重新居中。
    root->show();
    contentLayout()->invalidate();
    refresh();
}

QWidget *DraftDownloadDialog::buildFileList(QWidget *parent)
{
    auto *scroll = new QScrollArea(parent);
    scroll->setFrameShape(QFrame::NoFrame);
    scroll->setWidgetResizable(true);
    scroll->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    scroll->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    scroll->setStyleSheet(QStringLiteral(
        "QScrollArea { background:#101218; border:1px solid #242938; border-radius:8px; }"
        "QScrollArea > QWidget > QWidget { background:#101218; }"));

    auto *host = new QWidget(scroll);
    auto *layout = new QVBoxLayout(host);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    auto addGroup = [&](bool config) {
        QVector<int> indexes;
        for (int i = 0; i < m_job.files.size(); ++i) {
            const bool isConfig = m_job.files.at(i).kind == DraftFileKind::Config;
            if (isConfig == config) {
                indexes.append(i);
            }
        }
        if (indexes.isEmpty()) {
            return;
        }

        auto *header = new QFrame(host);
        header->setFixedHeight(24);
        header->setStyleSheet(QStringLiteral(
            "QFrame { background:#1A1E28; border:none;"
            "border-bottom:1px solid #242938; }"));
        auto *headerLayout = new QHBoxLayout(header);
        headerLayout->setContentsMargins(12, 0, 12, 0);
        headerLayout->setSpacing(8);
        auto *label = new QLabel(header);
        label->setTextFormat(Qt::RichText);
        label->setFont(Theme::font(10));
        label->setStyleSheet(QStringLiteral("background:transparent;"));
        headerLayout->addWidget(label);
        headerLayout->addStretch(1);
        layout->addWidget(header);

        GroupHeader group;
        group.config = config;
        group.label = label;
        m_groupHeaders.append(group);

        for (int index : indexes) {
            auto *row = new DraftFileRow(m_job.files.at(index), host);
            m_fileRows.append(row);
            m_fileIndexes.append(index);
            layout->addWidget(row);
        }
    };

    addGroup(true);
    addGroup(false);

    scroll->setWidget(host);
    m_fileScroll = scroll;
    const int contentHeight = m_groupHeaders.size() * 24 + m_fileRows.size() * 30;
    scroll->setFixedHeight(qBound(72, contentHeight + 2, 244));
    scroll->setVisible(!m_fileRows.isEmpty());
    return scroll;
}

QWidget *DraftDownloadDialog::buildSuccessBanner(QWidget *parent)
{
    auto *banner = new QFrame(parent);
    banner->setStyleSheet(QStringLiteral(
        "QFrame { background:rgba(62,207,142,0.08);"
        "border:1px solid rgba(62,207,142,0.30); border-radius:8px; }"));
    auto *layout = new QVBoxLayout(banner);
    layout->setContentsMargins(14, 12, 14, 12);
    layout->setSpacing(0);

    auto *top = new QWidget(banner);
    auto *topLayout = new QHBoxLayout(top);
    topLayout->setContentsMargins(0, 0, 0, 0);
    topLayout->setSpacing(10);
    topLayout->addWidget(new IconBadge(Icons::Glyph::Check, Theme::OnAccent,
                                       Theme::Success, 28, top),
                         0, Qt::AlignTop);

    auto *text = new QWidget(top);
    auto *textLayout = new QVBoxLayout(text);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(2);
    textLayout->addWidget(
        Ui::label(QStringLiteral("草稿已就绪"), 13, 650, Theme::Success, text));
    textLayout->addWidget(Ui::label(
        QStringLiteral("%1 个文件 · %2 已写入剪映草稿目录")
            .arg(m_job.totalFiles)
            .arg(MediaUtils::formatByteSize(m_job.totalBytes)),
        11, 400, Theme::Text2, text));
    topLayout->addWidget(text, 1);
    layout->addWidget(top);
    layout->addSpacing(10);

    auto *pathRow = new QFrame(banner);
    pathRow->setStyleSheet(QStringLiteral(
        "QFrame { background:rgba(20,23,31,0.72);"
        "border:1px solid #242938; border-radius:6px; }"));
    auto *pathLayout = new QHBoxLayout(pathRow);
    pathLayout->setContentsMargins(8, 4, 4, 4);
    pathLayout->setSpacing(8);
    pathLayout->addWidget(new IconBadge(Icons::Glyph::Folder, Theme::Text3,
                                        Qt::transparent, 20, pathRow));
    auto *path = new ElidedLabel(
        m_job.localPath.isEmpty() ? QStringLiteral("草稿目录") : m_job.localPath,
        pathRow);
    path->setPixelSize(10);
    path->setMonoFont(true);
    path->setTextColor(Theme::Text2);
    pathLayout->addWidget(path, 1);

    auto *copy = new GlyphButton(Icons::Glyph::Copy, 20, pathRow);
    copy->setGlyphSize(11);
    copy->setColors(Theme::Text3, Theme::Primary);
    copy->setToolTipText(QStringLiteral("复制草稿路径"));
    connect(copy, &QAbstractButton::clicked, this, [this] {
        QGuiApplication::clipboard()->setText(m_job.localPath);
        emit toastRequested(QStringLiteral("已复制草稿路径"));
    });
    pathLayout->addWidget(copy, 0, Qt::AlignVCenter);
    layout->addWidget(pathRow);
    return banner;
}

QWidget *DraftDownloadDialog::buildErrorBanner(QWidget *parent)
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

    int failed = 0;
    for (const DraftFileProgress &file : m_job.files) {
        if (file.status == DraftFileStatus::Error) {
            ++failed;
        }
    }

    QString reason = m_job.error;
    const QString prefix = QStringLiteral("下载失败：");
    if (reason.startsWith(prefix)) {
        reason.remove(0, prefix.size());
    }
    while (reason.endsWith(QChar(0x3002)) || reason.endsWith(QLatin1Char('.'))) {
        reason.chop(1);
    }
    if (reason.isEmpty()) {
        reason = QStringLiteral("网络连接中断");
    }

    auto *text = new QWidget(banner);
    auto *textLayout = new QVBoxLayout(text);
    textLayout->setContentsMargins(0, 0, 0, 0);
    textLayout->setSpacing(3);
    textLayout->addWidget(
        Ui::label(QStringLiteral("下载未完成"), 13, 650, Theme::Danger, text));
    const QString detailText = failed > 0
        ? QStringLiteral("%1，%2 个文件下载失败。已完成的 %3 个文件会保留，无需重新下载。")
              .arg(reason)
              .arg(failed)
              .arg(m_job.doneFiles)
        : QStringLiteral("%1，请稍后重试。").arg(reason);
    auto *detail = new ElidedLabel(detailText, text);
    detail->setPixelSize(11);
    detail->setTextColor(Theme::Text2);
    textLayout->addWidget(detail);
    layout->addWidget(text, 1);
    return banner;
}

void DraftDownloadDialog::refresh()
{
    if (m_stepper) {
        m_stepper->setStates(stepStates(m_job));
    }
    setLabelTextIfChanged(m_stageLabel, stageText(m_job));

    const bool error = m_job.status == DraftDownloadStatus::Error;
    const bool done = m_job.status == DraftDownloadStatus::Done;
    const int percent = done ? 100 : qMin(99, int(m_job.progress));

    if (m_percentLabel) {
        setLabelTextIfChanged(m_percentLabel, QStringLiteral("%1%").arg(percent));
        const QString style = QStringLiteral("color:%1;background:transparent;")
                                  .arg(error ? QStringLiteral("#F6465D")
                                             : (done ? QStringLiteral("#3ECF8E")
                                                     : QStringLiteral("#6E8BFF")));
        if (m_percentLabel->styleSheet() != style) {
            m_percentLabel->setStyleSheet(style);
        }
    }
    if (m_progress) {
        m_progress->setValue(int(qBound(0.0, m_job.progress, 100.0)));
        m_progress->setBarColor(error ? Theme::Danger
                                      : (done ? Theme::Success : Theme::Primary));
    }
    if (m_bytesLabel) {
        setLabelTextIfChanged(
            m_bytesLabel,
            m_job.totalBytes > 0
                ? QStringLiteral("%1 / %2").arg(
                      MediaUtils::formatByteSize(m_job.loadedBytes),
                      MediaUtils::formatByteSize(m_job.totalBytes))
                : QStringLiteral("—"));
    }
    if (m_metaLabel) {
        QString meta;
        if (error) {
            meta = QStringLiteral("%1/%2 个文件已完成")
                       .arg(m_job.doneFiles)
                       .arg(m_job.totalFiles);
        } else if (done) {
            meta = QStringLiteral("%1 个文件 · %2")
                       .arg(m_job.totalFiles)
                       .arg(MediaUtils::formatByteSize(m_job.totalBytes));
        } else if (m_job.status == DraftDownloadStatus::Writing) {
            meta = QStringLiteral("文件已全部下载，正在落盘…");
        } else if (m_job.stage == DraftDownloadStage::Config) {
            meta = QStringLiteral("正在读取草稿结构…");
        } else {
            meta = formatSpeed(m_job.speed);
            if (m_job.etaSeconds > 0) {
                const QString eta = m_job.etaSeconds >= 60
                    ? QStringLiteral("约 %1 分钟").arg((m_job.etaSeconds + 59) / 60)
                    : QStringLiteral("%1 秒").arg(m_job.etaSeconds);
                meta += QStringLiteral(" · 预计还需 %1").arg(eta);
            }
        }
        setLabelTextIfChanged(m_metaLabel, meta);
    }

    int activeRow = -1;
    int failedRow = -1;
    for (int i = 0; i < m_fileRows.size(); ++i) {
        const int index = m_fileIndexes.value(i, -1);
        if (index >= 0 && index < m_job.files.size()) {
            const DraftFileProgress &file = m_job.files.at(index);
            m_fileRows.at(i)->update(file);
            if (activeRow < 0 && file.status == DraftFileStatus::Downloading) {
                activeRow = i;
            }
            if (failedRow < 0 && file.status == DraftFileStatus::Error) {
                failedRow = i;
            }
        }
    }

    // 文件列表跟随当前下载项滚动；失败时定位到第一个失败文件。
    const int focusRow = activeRow >= 0 ? activeRow : failedRow;
    if (focusRow != m_lastActiveRow) {
        m_lastActiveRow = focusRow;
        if (m_fileScroll && focusRow >= 0 && focusRow < m_fileRows.size()) {
            m_fileScroll->ensureWidgetVisible(m_fileRows.at(focusRow), 0, 60);
        }
    }

    for (const GroupHeader &group : m_groupHeaders) {
        int count = 0;
        int finished = 0;
        qint64 size = 0;
        for (const DraftFileProgress &file : m_job.files) {
            const bool isConfig = file.kind == DraftFileKind::Config;
            if (isConfig != group.config) {
                continue;
            }
            ++count;
            if (file.status == DraftFileStatus::Done) {
                ++finished;
            }
            size += qMax(file.totalBytes, file.loadedBytes);
        }
        if (group.label) {
            const QString sizeText =
                size > 0 ? MediaUtils::formatByteSize(size) : QStringLiteral("—");
            const QString html =
                QStringLiteral("<span style='color:#9BA2B5;font-weight:600'>%1</span>"
                               " <span style='color:#62697E'>%2 个文件 · %3 · %4/%2</span>")
                    .arg(group.config ? QStringLiteral("草稿配置")
                                      : QStringLiteral("素材资源"))
                    .arg(count)
                    .arg(sizeText)
                    .arg(finished);
            if (group.label->text() != html) {
                group.label->setText(html);
            }
        }
    }
}

void DraftDownloadDialog::rebuildFooter()
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

    // Windows 11：主操作在左，关闭/取消在右；macOS 保持相反顺序。
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

        auto *open = new Button(FileManager::revealLabel(), Button::Primary, this);
        open->setLeadingGlyph(Icons::Glyph::Folder);
        connect(open, &QAbstractButton::clicked, this, [this] {
            emit openFolderRequested(m_job.localPath);
        });
        addActions(open, close);
        return;
    }

    if (m_mode == Mode::Error) {
        auto *close = new Button(QStringLiteral("关闭"), Button::Default, this);
        connect(close, &QAbstractButton::clicked, this, [this] { emit closeRequested(); });

        auto *retry = new Button(QStringLiteral("继续下载"), Button::Primary, this);
        connect(retry, &QAbstractButton::clicked, this, [this] {
            emit retryRequested(m_job.draftRowId);
        });
        addActions(retry, close);
        return;
    }

    auto *hint = Ui::label(QStringLiteral("关闭窗口后下载会继续"), 10, 400, Theme::Text3,
                           this);
    footer->insertWidget(0, hint);
    footer->setStretch(0, 0);
    footer->setStretch(1, 1);

    auto *background = new Button(QStringLiteral("后台继续"), Button::Primary, this);
    connect(background, &QAbstractButton::clicked, this, [this] { emit closeRequested(); });

    if (m_job.status == DraftDownloadStatus::Downloading) {
        auto *cancel = new Button(QStringLiteral("取消下载"), Button::Ghost, this);
        connect(cancel, &QAbstractButton::clicked, this, [this] {
            emit cancelRequested(m_job.draftRowId);
        });
        addActions(background, cancel);
    } else {
        footer->addWidget(background);
    }
}
