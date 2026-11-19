// 文件作用：任务中心页面实现，按状态分组展示任务并实时刷新进度。
#include "TasksPage.h"

#include "../ui/Controls.h"

#include <QDateTime>
#include <QGraphicsOpacityEffect>
#include <QHBoxLayout>
#include <QLabel>
#include <QVBoxLayout>

namespace {

QString stepBadgeText(int step)
{
    return QString::number(qBound(1, step + 1, 3));
}

Chip::Tone stepChipTone(int step)
{
    return step == 1 ? Chip::Ai : Chip::Run;
}

QString stepToneName(int step)
{
    return step == 1 ? QStringLiteral("ai") : QStringLiteral("primary");
}

int overallProgress(const ClipTask &task)
{
    return qBound(0, (qBound(0, task.step, 2) * 100 + qBound(0, task.progress, 100)) / 3,
                  100);
}

QString formatMs(qint64 ms)
{
    if (ms <= 0) {
        return QString();
    }
    return QDateTime::fromMSecsSinceEpoch(ms).toString(QStringLiteral("MM-dd HH:mm"));
}

QColor stageColor(const QString &tone)
{
    if (tone == QLatin1String("ai")) {
        return Theme::Ai;
    }
    if (tone == QLatin1String("primary")) {
        return Theme::Primary;
    }
    if (tone == QLatin1String("success")) {
        return Theme::Success;
    }
    if (tone == QLatin1String("danger")) {
        return Theme::Danger;
    }
    return Theme::Text3;
}

QColor stageFill(const QString &tone)
{
    if (tone == QLatin1String("ai")) {
        return Theme::alpha(Theme::Ai, 0.13);
    }
    if (tone == QLatin1String("primary")) {
        return Theme::alpha(Theme::Primary, 0.14);
    }
    if (tone == QLatin1String("success")) {
        return Theme::Success;
    }
    if (tone == QLatin1String("danger")) {
        return Theme::alpha(Theme::Danger, 0.13);
    }
    return Theme::Surface2;
}

QWidget *makeStageBadge(const QString &text, const QString &tone, QWidget *parent)
{
    auto *badge = new Card(stageFill(tone), Qt::transparent, 8, parent);
    badge->setFixedSize(28, 28);
    auto *layout = badge->contentLayout();
    layout->setContentsMargins(0, 0, 0, 0);
    auto *label = Ui::label(text, tone == QLatin1String("success") ? 13 : 12, 600,
                            tone == QLatin1String("success") ? Theme::OnAccent
                                                             : stageColor(tone),
                            badge);
    label->setFont(Theme::monoFont(12));
    label->setAlignment(Qt::AlignCenter);
    layout->addWidget(label);
    return badge;
}

QWidget *sectionLabel(const QString &text, QWidget *parent)
{
    auto *label = Ui::label(text, 11, 400, Theme::Text3, parent);
    QFont font = label->font();
    font.setLetterSpacing(QFont::AbsoluteSpacing, 0.5);
    label->setFont(font);
    label->setContentsMargins(0, 0, 0, 8);
    return label;
}

} // namespace

TasksPage::TasksPage(QWidget *parent)
    : Page(parent)
{
    auto *header = new PageHeader(
        QStringLiteral("任务中心"),
        QStringLiteral("一键成片任务会在这里持续更新；离开页面后仍可回来查看进度。"),
        this);
    auto *refresh = new Button(QStringLiteral("刷新"), Button::Default, this);
    connect(refresh, &QAbstractButton::clicked, this, &TasksPage::refreshRequested);
    header->addAction(refresh);
    bodyLayout()->addWidget(header);

    m_listHost = new QWidget(this);
    m_listLayout = new QVBoxLayout(m_listHost);
    m_listLayout->setContentsMargins(0, 0, 0, 0);
    m_listLayout->setSpacing(8);
    bodyLayout()->addWidget(m_listHost);
    bodyLayout()->addStretch(1);
}

void TasksPage::setTasks(const QVector<ClipTask> &tasks)
{
    m_tasks = tasks;
    rebuild();
}

void TasksPage::rebuild()
{
    while (QLayoutItem *item = m_listLayout->takeAt(0)) {
        if (QWidget *widget = item->widget()) {
            widget->hide();
            widget->deleteLater();
        }
        delete item;
    }

    if (m_tasks.isEmpty()) {
        auto *empty = new Card(Theme::Surface1, Theme::Border, 10, m_listHost);
        auto *layout = empty->contentLayout();
        layout->setContentsMargins(24, 38, 24, 38);
        layout->setSpacing(8);
        auto *title = Ui::label(QStringLiteral("暂无任务"), 15, 600, Theme::Text1, empty);
        title->setAlignment(Qt::AlignCenter);
        layout->addWidget(title);
        auto *hint = Ui::label(
            QStringLiteral("在源视频库点击「一键成片」后，任务会显示在这里，可随时回来查看进度。"),
            12, 400, Theme::Text3, empty);
        hint->setAlignment(Qt::AlignCenter);
        hint->setWordWrap(true);
        layout->addWidget(hint);
        m_listLayout->addWidget(empty);
        m_listLayout->addStretch(1);
        return;
    }

    QVector<ClipTask> running;
    QVector<ClipTask> failed;
    QVector<ClipTask> completed;
    for (const ClipTask &task : m_tasks) {
        switch (task.status) {
        case ClipTaskStatus::Running:
            running.append(task);
            break;
        case ClipTaskStatus::Failed:
            failed.append(task);
            break;
        case ClipTaskStatus::Completed:
            completed.append(task);
            break;
        }
    }

    bool firstSection = true;
    auto addSection = [this, &firstSection](const QString &name,
                                            const QVector<ClipTask> &tasks) {
        if (tasks.isEmpty()) {
            return;
        }
        if (!firstSection) {
            m_listLayout->addSpacing(9);
        }
        firstSection = false;
        m_listLayout->addWidget(sectionLabel(name, m_listHost));

        for (const ClipTask &task : tasks) {
            const bool isRunning = task.status == ClipTaskStatus::Running;
            const bool isFailed = task.status == ClipTaskStatus::Failed;
            const bool isCompleted = task.status == ClipTaskStatus::Completed;

            auto *row = new Card(
                Theme::Surface1,
                isFailed ? Theme::alpha(Theme::Danger, 0.35) : Theme::Border, 9,
                m_listHost);
            auto *rowLayout = new QHBoxLayout;
            rowLayout->setContentsMargins(14, 15, 14, 15);
            rowLayout->setSpacing(12);
            row->contentLayout()->addLayout(rowLayout);

            QString badgeText;
            QString badgeTone;
            QString chipText;
            Chip::Tone chipTone = Chip::Neutral;
            QString subtitle;
            if (isRunning) {
                badgeText = stepBadgeText(task.step);
                badgeTone = stepToneName(task.step);
                chipText =
                    QStringLiteral("%1 · %2%").arg(clipTaskStepName(task.step)).arg(task.progress);
                chipTone = stepChipTone(task.step);
            } else if (isCompleted) {
                badgeText = QStringLiteral("✓");
                badgeTone = QStringLiteral("success");
                chipText = QStringLiteral("已完成");
                chipTone = Chip::Ok;
            } else {
                badgeText = QStringLiteral("!");
                badgeTone = QStringLiteral("danger");
                chipText = QStringLiteral("失败");
                chipTone = Chip::Error;
            }
            rowLayout->addWidget(makeStageBadge(badgeText, badgeTone, row));

            auto *info = new QWidget(row);
            auto *infoLayout = new QVBoxLayout(info);
            infoLayout->setContentsMargins(0, 0, 0, 0);
            infoLayout->setSpacing(0);

            auto *titleRow = new QWidget(info);
            auto *titleLayout = new QHBoxLayout(titleRow);
            titleLayout->setContentsMargins(0, 0, 0, 0);
            titleLayout->setSpacing(6);
            auto *title = new ElidedLabel(
                task.title.isEmpty() ? QStringLiteral("未命名任务") : task.title,
                titleRow);
            title->setPixelSize(13);
            title->setFontWeight(600);
            title->setTextColor(Theme::Text1);
            titleLayout->addWidget(title, 1);
            titleLayout->addWidget(new Chip(chipText, chipTone, titleRow));
            infoLayout->addWidget(titleRow);
            infoLayout->addSpacing(3);

            if (isRunning) {
                subtitle = task.message;
                const QString started = formatMs(task.createdAtMs);
                if (!started.isEmpty()) {
                    subtitle += subtitle.isEmpty() ? QStringLiteral("开始于 %1").arg(started)
                                                   : QStringLiteral(" · 开始于 %1").arg(started);
                }
            } else if (isCompleted) {
                subtitle = task.message;
                if (subtitle.isEmpty()) {
                    subtitle = QStringLiteral("已生成 %1 条剪映草稿").arg(task.draftCount);
                }
                const QString finished = formatMs(task.finishedAtMs);
                if (!finished.isEmpty()) {
                    subtitle += QStringLiteral(" · %1").arg(finished);
                }
            } else {
                subtitle = task.message.isEmpty() ? QStringLiteral("任务执行失败。")
                                                  : task.message;
                const QString finished = formatMs(task.finishedAtMs);
                if (!finished.isEmpty()) {
                    subtitle += QStringLiteral(" · %1").arg(finished);
                }
            }
            auto *subtitleLabel = Ui::label(subtitle, 11, 400,
                                            isFailed ? Theme::Text2 : Theme::Text3, info);
            subtitleLabel->setWordWrap(true);
            infoLayout->addWidget(subtitleLabel);

            if (!isCompleted) {
                infoLayout->addSpacing(6);
                auto *bar = new ProgressBar(info);
                bar->setFixedWidth(360);
                bar->setValue(overallProgress(task));
                if (isFailed) {
                    bar->setBarColor(Theme::Danger);
                } else if (task.step == 1) {
                    bar->setBarColor(Theme::Ai);
                } else {
                    bar->setBarColor(Theme::Primary);
                }
                infoLayout->addWidget(bar);
            }

            rowLayout->addWidget(info, 1);

            auto *action = new Button(
                isRunning ? QStringLiteral("查看进度")
                          : (isCompleted ? QStringLiteral("查看成片")
                                         : QStringLiteral("重试")),
                isFailed ? Button::Default : Button::Primary, row);
            const qint64 taskId = task.id;
            if (isFailed) {
                connect(action, &QAbstractButton::clicked, this,
                        [this, taskId] { emit retryTaskRequested(taskId); });
            } else {
                connect(action, &QAbstractButton::clicked, this,
                        [this, taskId] { emit viewTaskRequested(taskId); });
            }
            rowLayout->addWidget(action, 0, Qt::AlignVCenter);

            if (isCompleted) {
                auto *effect = new QGraphicsOpacityEffect(row);
                effect->setOpacity(0.78);
                row->setGraphicsEffect(effect);
            }
            m_listLayout->addWidget(row);
        }
    };

    addSection(QStringLiteral("运行中 · %1").arg(running.size()), running);
    addSection(QStringLiteral("失败 · %1").arg(failed.size()), failed);
    addSection(QStringLiteral("最近完成 · %1").arg(completed.size()), completed);
    m_listLayout->addStretch(1);
}
