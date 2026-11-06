#pragma once

#include "../ui/WindowChrome.h"

class ConfigStore;
class QNetworkAccessManager;
class QWidget;

class SettingsPage : public Page
{
    Q_OBJECT

public:
    explicit SettingsPage(ConfigStore *configStore, QWidget *parent = nullptr);

signals:
    void toastRequested(const QString &message);
    void settingsSaved();

private:
    ConfigStore *m_config = nullptr;
    QNetworkAccessManager *m_network = nullptr;
    QWidget *m_serviceWarning = nullptr;
};
