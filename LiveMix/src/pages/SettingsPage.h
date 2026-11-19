#pragma once

#include "../data/PromptTypes.h"
#include "../ui/WindowChrome.h"

#include <QVector>

class ConfigStore;
class QNetworkAccessManager;
class QWidget;
class SelectBox;

class SettingsPage : public Page
{
    Q_OBJECT

public:
    explicit SettingsPage(ConfigStore *configStore, QWidget *parent = nullptr);

    // 提示词新增 / 删除后刷新「默认提示词」下拉框，保留当前选择。
    void setPrompts(const QVector<PromptEntry> &prompts);

signals:
    void toastRequested(const QString &message);
    void settingsSaved();

private:
    ConfigStore *m_config = nullptr;
    QNetworkAccessManager *m_network = nullptr;
    QWidget *m_serviceWarning = nullptr;
    SelectBox *m_promptSelect = nullptr;
};
