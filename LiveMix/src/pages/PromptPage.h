#pragma once

#include "../data/PromptTypes.h"
#include "../ui/WindowChrome.h"

#include <QVector>

class QVBoxLayout;

class PromptPage : public Page
{
    Q_OBJECT

public:
    explicit PromptPage(QWidget *parent = nullptr);

    // 提示词保存 / 删除后由 MainWindow 重新下发列表并重建表格。
    void setPrompts(const QVector<PromptEntry> &prompts);

signals:
    void newPromptRequested();
    void editPromptRequested(int index);
    void viewPromptRequested(int index);
    void deletePromptRequested(int index);

private:
    void rebuildTable();

    QVBoxLayout *m_tableLayout = nullptr;
    QVector<PromptEntry> m_prompts;
};
