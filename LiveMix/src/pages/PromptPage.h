#pragma once

#include "../ui/WindowChrome.h"

class PromptPage : public Page
{
    Q_OBJECT

public:
    explicit PromptPage(QWidget *parent = nullptr);

signals:
    void newPromptRequested();
    void editPromptRequested(int index);
    void viewPromptRequested(int index);
    void deletePromptRequested(int index);
};
