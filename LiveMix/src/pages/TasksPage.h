#pragma once

// 文件作用：任务中心页面，展示持久化的一键成片任务。
// 支持查看运行中任务的进度、查看已完成任务的成片、重试失败任务。

#include "../data/ClipTypes.h"
#include "../ui/WindowChrome.h"

#include <QVector>

class QVBoxLayout;
class QWidget;

class TasksPage : public Page
{
    Q_OBJECT

public:
    explicit TasksPage(QWidget *parent = nullptr);

    void setTasks(const QVector<ClipTask> &tasks);

signals:
    void viewTaskRequested(qint64 taskId);
    void retryTaskRequested(qint64 taskId);
    void refreshRequested();
    void toastRequested(const QString &message);

private:
    void rebuild();

    QWidget *m_listHost = nullptr;
    QVBoxLayout *m_listLayout = nullptr;
    QVector<ClipTask> m_tasks;
};
