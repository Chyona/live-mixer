#include "PromptPage.h"

#include "../model/DemoData.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QEnterEvent>
#include <QHBoxLayout>
#include <QLabel>
#include <QPainter>
#include <QVBoxLayout>

namespace {

constexpr int kNameWidth = 232;
constexpr int kDateWidth = 162;
constexpr int kUsesWidth = 139;
constexpr int kActionsWidth = 139;

class TableRow : public QWidget
{
public:
    explicit TableRow(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setAttribute(Qt::WA_Hover, true);
    }

protected:
    void enterEvent(QEnterEvent *event) override
    {
        m_hovered = true;
        update();
        QWidget::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = false;
        update();
        QWidget::leaveEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        if (m_hovered) {
            painter.fillRect(rect(), QColor(255, 255, 255, 5));
        }
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.drawLine(QPointF(0, height() - 0.5), QPointF(width(), height() - 0.5));
    }

private:
    bool m_hovered = false;
};

QLabel *dotLabel(QWidget *parent)
{
    return Ui::label(QStringLiteral("·"), 12, 400, Theme::Text3, parent);
}

} // namespace

PromptPage::PromptPage(QWidget *parent)
    : Page(parent)
{
    auto *header = new PageHeader(
        QStringLiteral("提示词管理"),
        QStringLiteral("管理 AI 切片使用的系统提示词，决定 AI 如何从转写稿中识别高价值片段。"),
        this);
    auto *create = new Button(QStringLiteral("+ 新建提示词"), Button::Primary, this);
    connect(create, &QAbstractButton::clicked, this, &PromptPage::newPromptRequested);
    header->addAction(create);
    bodyLayout()->addWidget(header);

    auto *table = new QWidget(this);
    auto *tableLayout = new QVBoxLayout(table);
    tableLayout->setContentsMargins(0, 0, 0, 0);
    tableLayout->setSpacing(0);

    auto *headerRow = new QWidget(table);
    headerRow->setFixedHeight(34);
    auto *headerLayout = new QHBoxLayout(headerRow);
    headerLayout->setContentsMargins(10, 0, 10, 0);
    headerLayout->setSpacing(0);
    auto *nameHeader = Ui::label(QStringLiteral("名称"), 11, 400, Theme::Text3, headerRow);
    nameHeader->setFixedWidth(kNameWidth);
    headerLayout->addWidget(nameHeader);
    auto *summaryHeader =
        Ui::label(QStringLiteral("提示词摘要"), 11, 400, Theme::Text3, headerRow);
    summaryHeader->setContentsMargins(0, 0, 14, 0);
    headerLayout->addWidget(summaryHeader, 1);
    auto *editTime = Ui::label(QStringLiteral("编辑时间"), 11, 400, Theme::Text3, headerRow);
    editTime->setFixedWidth(kDateWidth);
    headerLayout->addWidget(editTime);
    auto *uses = Ui::label(QStringLiteral("使用次数"), 11, 400, Theme::Text3, headerRow);
    uses->setFixedWidth(kUsesWidth);
    headerLayout->addWidget(uses);
    auto *operations = Ui::label(QStringLiteral("操作"), 11, 400, Theme::Text3, headerRow);
    operations->setFixedWidth(kActionsWidth);
    headerLayout->addWidget(operations);
    tableLayout->addWidget(headerRow);
    tableLayout->addWidget(Ui::separator(false, table));

    const QVector<Demo::PromptEntry> prompts = Demo::prompts();
    for (int i = 0; i < prompts.size(); ++i) {
        const Demo::PromptEntry &prompt = prompts[i];
        auto *row = new TableRow(table);
        row->setMinimumHeight(42);
        auto *rowLayout = new QHBoxLayout(row);
        rowLayout->setContentsMargins(10, 8, 10, 8);
        rowLayout->setSpacing(0);

        auto *nameHost = new QWidget(row);
        nameHost->setFixedWidth(kNameWidth);
        auto *nameLayout = new QHBoxLayout(nameHost);
        nameLayout->setContentsMargins(0, 0, 10, 0);
        nameLayout->setSpacing(6);
        auto *name = Ui::label(prompt.name, 13, 600, Theme::Text1, nameHost);
        nameLayout->addWidget(name);
        if (prompt.builtIn) {
            nameLayout->addWidget(new Chip(QStringLiteral("内置"), Chip::Neutral, nameHost));
        }
        nameLayout->addStretch(1);
        rowLayout->addWidget(nameHost);

        auto *summaryHost = new QWidget(row);
        auto *summaryLayout = new QHBoxLayout(summaryHost);
        summaryLayout->setContentsMargins(0, 0, 14, 0);
        summaryLayout->setSpacing(8);
        auto *summary = new ElidedLabel(prompt.summary, summaryHost);
        summary->setPixelSize(13);
        summary->setTextColor(Theme::Text2);
        summaryLayout->addWidget(summary, 1);
        auto *eye = new GlyphButton(Icons::Glyph::Eye, 18, summaryHost);
        eye->setGlyphSize(14);
        eye->setColors(Theme::Text3, Theme::Primary);
        eye->setToolTipText(prompt.builtIn
                                ? QStringLiteral("查看完整内容（只读）")
                                : QStringLiteral("查看 / 编辑提示词"));
        connect(eye, &QAbstractButton::clicked, this,
                [this, i] { emit viewPromptRequested(i); });
        summaryLayout->addWidget(eye);
        rowLayout->addWidget(summaryHost, 1);

        auto *date = Ui::label(prompt.editedAt, 11, 400, Theme::Text2, row);
        date->setFont(Theme::monoFont(11));
        date->setFixedWidth(kDateWidth);
        rowLayout->addWidget(date);

        auto *useCount = Ui::label(QString::number(prompt.uses), 11, 400, Theme::Text2, row);
        useCount->setFont(Theme::monoFont(11));
        useCount->setFixedWidth(kUsesWidth);
        rowLayout->addWidget(useCount);

        auto *actions = new QWidget(row);
        actions->setFixedWidth(kActionsWidth);
        auto *actionsLayout = new QHBoxLayout(actions);
        actionsLayout->setContentsMargins(0, 0, 0, 0);
        actionsLayout->setSpacing(4);
        auto *edit = new LinkLabel(QStringLiteral("编辑"), actions);
        edit->setPixelSize(13);
        auto *remove = new LinkLabel(QStringLiteral("删除"), actions);
        remove->setPixelSize(13);
        remove->setTextColor(Theme::Danger);
        remove->setHoverColor(Theme::Danger);
        if (prompt.builtIn) {
            edit->setTextColor(Theme::Text3);
            edit->setHoverColor(Theme::Text3);
            edit->setDisabledVisual(true);
            remove->setTextColor(Theme::Text3);
            remove->setHoverColor(Theme::Text3);
            remove->setDisabledVisual(true);
        } else {
            connect(edit, &QAbstractButton::clicked, this,
                    [this, i] { emit editPromptRequested(i); });
            connect(remove, &QAbstractButton::clicked, this,
                    [this, i] { emit deletePromptRequested(i); });
        }
        actionsLayout->addWidget(edit);
        actionsLayout->addWidget(dotLabel(actions));
        actionsLayout->addWidget(remove);
        actionsLayout->addStretch(1);
        rowLayout->addWidget(actions);

        tableLayout->addWidget(row);
    }

    bodyLayout()->addWidget(table);
    bodyLayout()->addStretch(1);
}
