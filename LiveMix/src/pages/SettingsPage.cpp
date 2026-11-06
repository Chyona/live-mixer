#include "SettingsPage.h"

#include "../core/AppConfig.h"
#include "../model/DemoData.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"

#include <QDir>
#include <QEnterEvent>
#include <QFileDialog>
#include <QHBoxLayout>
#include <QIntValidator>
#include <QLabel>
#include <QLineEdit>
#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QNetworkRequest>
#include <QPainter>
#include <QStackedWidget>
#include <QTimer>
#include <QUrl>
#include <QVBoxLayout>

#include <functional>

namespace {

constexpr int kFieldWidth = 560;

class PaneNavButton : public QAbstractButton
{
public:
    explicit PaneNavButton(const QString &text, QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setText(text);
        setFixedHeight(34);
        setCursor(Qt::PointingHandCursor);
        setAttribute(Qt::WA_Hover, true);
        setFocusPolicy(Qt::NoFocus);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

    void setActive(bool active)
    {
        m_active = active;
        update();
    }

protected:
    void enterEvent(QEnterEvent *event) override
    {
        m_hovered = true;
        update();
        QAbstractButton::enterEvent(event);
    }

    void leaveEvent(QEvent *event) override
    {
        m_hovered = false;
        update();
        QAbstractButton::leaveEvent(event);
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        if (m_active) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Primary, 0.14));
            painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 6, 6);
        } else if (m_hovered) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::Surface2);
            painter.drawRoundedRect(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5), 6, 6);
        }
        painter.setFont(Theme::font(13, m_active ? 500 : 400));
        painter.setPen(m_active ? Theme::Primary : Theme::Text2);
        painter.drawText(QRect(12, 0, width() - 24, height()), Qt::AlignLeft | Qt::AlignVCenter,
                         text());
    }

private:
    bool m_active = false;
    bool m_hovered = false;
};

class AdaptiveStack : public QStackedWidget
{
public:
    explicit AdaptiveStack(QWidget *parent = nullptr)
        : QStackedWidget(parent)
    {
    }

    QSize sizeHint() const override
    {
        if (currentWidget()) {
            return currentWidget()->sizeHint();
        }
        return QStackedWidget::sizeHint();
    }

    QSize minimumSizeHint() const override
    {
        if (currentWidget()) {
            return currentWidget()->minimumSizeHint();
        }
        return QStackedWidget::minimumSizeHint();
    }
};

class StatusDotLine : public QWidget
{
public:
    explicit StatusDotLine(const QColor &color, QWidget *parent = nullptr)
        : QWidget(parent)
        , m_color(color)
    {
        setFixedSize(8, 8);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        painter.setPen(Qt::NoPen);
        painter.setBrush(m_color);
        painter.drawEllipse(QRectF(rect()));
    }

private:
    QColor m_color;
};

QLabel *makeKbd(const QString &text, QWidget *parent)
{
    auto *chip = new QLabel(text, parent);
    chip->setFont(Theme::monoFont(11));
    chip->setStyleSheet(QStringLiteral(
        "color:#E9ECF4;background:#222734;border:1px solid #343B4E;"
        "border-bottom-width:2px;border-radius:4px;padding:1px 6px;"));
    chip->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    return chip;
}

FormField *makeField(const QString &label, QWidget *content, const QString &hint,
                     bool required, QWidget *parent)
{
    auto *field = new FormField(label, content, hint, required, parent);
    field->setFixedWidth(kFieldWidth);
    return field;
}

QWidget *makePathRow(InputField *input, QWidget *parent, const std::function<void()> &browse)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(8);
    layout->addWidget(input, 1);
    auto *button = new Button(QStringLiteral("浏览…"), Button::Default, row);
    QObject::connect(button, &QAbstractButton::clicked, row, [browse] { browse(); });
    layout->addWidget(button);
    return row;
}

QWidget *makeCheckRow(const QStringList &labels, const QVector<int> &checked,
                      QWidget *parent, QVector<CheckBox *> *boxes = nullptr)
{
    auto *host = new QWidget(parent);
    auto *layout = new QVBoxLayout(host);
    layout->setContentsMargins(0, 3, 0, 0);
    layout->setSpacing(9);
    for (int i = 0; i < labels.size(); ++i) {
        auto *box = new CheckBox(labels[i], host);
        box->setChecked(checked.contains(i));
        layout->addWidget(box);
        if (boxes) {
            boxes->append(box);
        }
    }
    return host;
}

QWidget *makePane(QWidget *parent)
{
    auto *pane = new QWidget(parent);
    pane->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    auto *layout = new QVBoxLayout(pane);
    layout->setContentsMargins(22, 18, 22, 18);
    layout->setSpacing(0);
    layout->setAlignment(Qt::AlignTop);
    return pane;
}

} // namespace

SettingsPage::SettingsPage(ConfigStore *configStore, QWidget *parent)
    : Page(parent)
    , m_config(configStore)
{
    const AppConfig config = m_config ? m_config->config() : ConfigStore::defaults();
    m_network = new QNetworkAccessManager(this);

    auto *header = new PageHeader(
        QStringLiteral("设置"),
        QStringLiteral("桌面客户端本地配置，连接自托管服务与本地输出路径。"), this);
    bodyLayout()->addWidget(header);

    auto *outer = new Card(Theme::Surface1, Theme::Border, 8, this);
    outer->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    auto *outerLayout = new QHBoxLayout;
    outerLayout->setContentsMargins(0, 0, 0, 0);
    outerLayout->setSpacing(0);
    outer->contentLayout()->addLayout(outerLayout);

    auto *side = new QWidget(outer);
    side->setFixedWidth(220);
    auto *sideLayout = new QVBoxLayout(side);
    sideLayout->setContentsMargins(8, 8, 8, 8);
    sideLayout->setSpacing(0);

    auto *stack = new AdaptiveStack(outer);
    stack->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);

    auto fitStack = [stack] {
        QWidget *pane = stack->currentWidget();
        if (!pane) {
            return;
        }
        stack->setFixedHeight(qMax(pane->sizeHint().height(),
                                   pane->minimumSizeHint().height()));
    };

    const QStringList paneLabels = {
        QStringLiteral("服务连接"), QStringLiteral("存储与输出"),
        QStringLiteral("AI 与 ASR"), QStringLiteral("快捷键"), QStringLiteral("通用"),
    };
    QVector<PaneNavButton *> navButtons;
    for (const QString &label : paneLabels) {
        auto *button = new PaneNavButton(label, side);
        navButtons.append(button);
        sideLayout->addWidget(button);
    }
    // 全部按钮创建完成后再接线：每个 handler 都要持有完整列表，才能清掉其余项的高亮。
    for (int index = 0; index < navButtons.size(); ++index) {
        connect(navButtons[index], &QAbstractButton::clicked, this,
                [index, navButtons, stack, fitStack] {
                    for (int i = 0; i < navButtons.size(); ++i) {
                        navButtons[i]->setActive(i == index);
                    }
                    stack->setCurrentIndex(index);
                    fitStack();
                    if (stack->parentWidget()) {
                        stack->parentWidget()->updateGeometry();
                    }
                });
    }
    sideLayout->addStretch(1);
    navButtons.first()->setActive(true);
    outerLayout->addWidget(side);
    outerLayout->addWidget(Ui::separator(true, outer));
    outerLayout->addWidget(stack, 1);

    // ---- Service connection -------------------------------------------------
    auto *servicePane = makePane(stack);
    auto *serviceLayout = qobject_cast<QVBoxLayout *>(servicePane->layout());
    auto *warning = new Card(Theme::alpha(Theme::Danger, 0.13),
                             Theme::alpha(Theme::Danger, 0.30), 8, servicePane);
    warning->setMinimumHeight(40);
    auto *warningLayout = new QHBoxLayout;
    warningLayout->setContentsMargins(14, 10, 14, 10);
    warningLayout->setSpacing(10);
    warning->contentLayout()->addLayout(warningLayout);
    warningLayout->addWidget(new StatusDotLine(Theme::Danger, warning));
    auto *warningText = Ui::label(
        QStringLiteral("尚未配置 API 密钥 —— ASR 转写与 AI 切片暂不可用，配置后自动生效。"),
        12, 400, Theme::Danger, warning);
    warningText->setWordWrap(true);
    warningLayout->addWidget(warningText, 1);
    m_serviceWarning = warning;
    warning->setVisible(config.service.apiKey.trimmed().isEmpty());
    serviceLayout->addWidget(warning);
    serviceLayout->addSpacing(18);

    auto *apiInput = new InputField(servicePane);
    apiInput->setText(config.service.apiUrl);
    auto *apiField = makeField(
        QStringLiteral("API 地址（OpenAI 兼容）"), apiInput,
        QStringLiteral("该网关已聚合 ASR 语音识别与大模型能力，配置一次即可使用全部 AI 功能，"
                       "无需单独填写。"),
        true, servicePane);
    serviceLayout->addWidget(apiField, 0, Qt::AlignLeft);
    serviceLayout->addSpacing(16);

    auto *keyInput = new InputField(servicePane);
    keyInput->setPassword(true);
    keyInput->setPlaceholder(QStringLiteral("sk-..."));
    keyInput->setText(config.service.apiKey);
    auto *keyField = makeField(
        QStringLiteral("API 密钥"), keyInput,
        QStringLiteral("密钥仅存本机，用于 LLM 网关、豆包 ASR 及剪映小助手；"
                       "未配置则「一键成片」不可用，<br>"
                       "前往：<a href=\"https://jcaigc.cn\" "
                       "style=\"color:%1;text-decoration:none;\">"
                       "https://jcaigc.cn</a> 获取 API Key")
            .arg(Theme::Primary.name()),
        true, servicePane);
    keyField->setHintRichText(true);
    // 不使用 AlignLeft：否则布局按单行 sizeHint 分配高度，换行后的提示会被裁掉。
    serviceLayout->addWidget(keyField, 0);
    serviceLayout->addSpacing(16);

    auto *testButton = new Button(QStringLiteral("测试连接并保存"), Button::Primary,
                                  servicePane);
    serviceLayout->addWidget(testButton, 0, Qt::AlignLeft);
    stack->addWidget(servicePane);

    // ---- Storage ------------------------------------------------------------
    auto *storagePane = makePane(stack);
    auto *storageLayout = qobject_cast<QVBoxLayout *>(storagePane->layout());

    auto chooseDirectory = [this](InputField *input) {
        const QString selected = QFileDialog::getExistingDirectory(
            this, QStringLiteral("选择目录"), input->text());
        if (!selected.isEmpty()) {
            input->setText(QDir::toNativeSeparators(selected));
        }
    };
    auto pathRow = [chooseDirectory](InputField *input, QWidget *parent) {
        return makePathRow(input, parent,
                           [chooseDirectory, input] { chooseDirectory(input); });
    };
    auto cleanedPath = [](const QString &text, const QString &fallback) {
        const QString trimmed = text.trimmed();
        return trimmed.isEmpty() ? fallback : QDir::cleanPath(trimmed);
    };
    auto firstInteger = [](const QString &text, bool *ok) {
        QString digits;
        for (const QChar ch : text) {
            if (ch.isDigit()) {
                digits.append(ch);
            } else if (!digits.isEmpty()) {
                break;
            }
        }
        return digits.toInt(ok);
    };

    auto *draftInput = new InputField(storagePane);
    draftInput->setText(QDir::toNativeSeparators(config.storage.draftDir));
    auto *draftField = makeField(
        QStringLiteral("剪映草稿输出目录"), pathRow(draftInput, storagePane),
        QStringLiteral("生成的剪映草稿包输出到此目录，可直接拖入剪映继续后期。"), false,
        storagePane);
    storageLayout->addWidget(draftField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *exportInput = new InputField(storagePane);
    exportInput->setText(QDir::toNativeSeparators(config.storage.exportDir));
    auto *exportField = makeField(
        QStringLiteral("成片导出目录"), pathRow(exportInput, storagePane), QString(), false,
        storagePane);
    storageLayout->addWidget(exportField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *cacheInput = new InputField(storagePane);
    cacheInput->setText(QDir::toNativeSeparators(config.storage.cacheDir));
    auto *cacheField = makeField(
        QStringLiteral("临时缓存目录"), pathRow(cacheInput, storagePane),
        QStringLiteral("ASR 抽音频、切片裁剪的临时文件存放处。"), false, storagePane);
    storageLayout->addWidget(cacheField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *recordingInput = new InputField(storagePane);
    recordingInput->setText(QDir::toNativeSeparators(config.storage.recordingDir));
    auto *recordingField = makeField(
        QStringLiteral("录制/下载输出目录"), pathRow(recordingInput, storagePane),
        QStringLiteral("mp4 直链下载的原文件、ffmpeg 录制的 m3u8 与 ts 分片，"
                       "以及成片时合成的完整 MP4 保存到此目录。"),
        false, storagePane);
    storageLayout->addWidget(recordingField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *segmentInput = new InputField(storagePane);
    segmentInput->setFixedWidth(120);
    segmentInput->lineEdit()->setValidator(new QIntValidator(1, 30, segmentInput));
    segmentInput->setText(QString::number(config.recording.segmentSeconds));
    auto *segmentField = makeField(
        QStringLiteral("HLS 分段长度（秒）"), segmentInput,
        QStringLiteral("录制时按该时长切分 ts 分片，建议 2–6 秒。"), false, storagePane);
    storageLayout->addWidget(segmentField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(16);

    auto *limitRow = new QWidget(storagePane);
    auto *limitLayout = new QHBoxLayout(limitRow);
    limitLayout->setContentsMargins(0, 0, 0, 0);
    limitLayout->setSpacing(10);
    auto *limitInput = new InputField(limitRow);
    limitInput->setFixedWidth(120);
    limitInput->setText(QStringLiteral("%1 GB").arg(config.storage.cacheLimitGb));
    auto *autoClean = new CheckBox(QStringLiteral("超过后自动清理最旧缓存"), limitRow);
    autoClean->setChecked(config.storage.autoCleanCache);
    limitLayout->addWidget(limitInput, 0, Qt::AlignVCenter);
    limitLayout->addWidget(autoClean, 0, Qt::AlignVCenter);
    limitLayout->addStretch(1);
    auto *limitField = makeField(QStringLiteral("缓存上限"), limitRow, QString(), false,
                                 storagePane);
    storageLayout->addWidget(limitField, 0, Qt::AlignLeft);
    storageLayout->addSpacing(18);

    // Saving is handled by the header action so every pane persists together.
    if (m_config) {
        auto *configHint = Ui::label(
            QStringLiteral("配置文件：%1")
                .arg(QDir::toNativeSeparators(m_config->filePath())),
            11, 400, Theme::Text3, storagePane);
        configHint->setWordWrap(true);
        configHint->setTextInteractionFlags(Qt::TextSelectableByMouse);
        storageLayout->addWidget(configHint);
    }
    stack->addWidget(storagePane);

    // ---- AI & ASR -----------------------------------------------------------
    auto *aiPane = makePane(stack);
    auto *aiLayout = qobject_cast<QVBoxLayout *>(aiPane->layout());
    auto *promptSelect = new SelectBox(aiPane);
    const QVector<Demo::PromptEntry> prompts = Demo::prompts();
    for (const Demo::PromptEntry &prompt : prompts) {
        promptSelect->addItem(prompt.name);
    }
    const int defaultPromptIndex = promptSelect->findText(config.ai.defaultPrompt);
    if (defaultPromptIndex >= 0) {
        promptSelect->setCurrentIndex(defaultPromptIndex);
    }
    auto *promptField = makeField(QStringLiteral("新建项目时默认提示词"), promptSelect, QString(),
                                  false, aiPane);
    aiLayout->addWidget(promptField, 0, Qt::AlignLeft);
    aiLayout->addSpacing(16);

    // 选片时长由提示词控制（内置默认提示词要求成片 3–8 分钟），不再单独设置区间。
    auto *durationNote = Ui::label(
        QStringLiteral("成片时长由所选提示词控制；内置默认提示词要求成片 3–8 分钟。"),
        11, 400, Theme::Text3, aiPane);
    durationNote->setWordWrap(true);
    auto *durationField = makeField(QStringLiteral("成片时长"), durationNote, QString(),
                                    false, aiPane);
    aiLayout->addWidget(durationField, 0, Qt::AlignLeft);
    aiLayout->addSpacing(16);

    auto *subtitleCheck = new CheckBox(QStringLiteral("ASR 完成后自动导出 SRT 字幕"), aiPane);
    subtitleCheck->setChecked(config.ai.exportSrt);
    auto *subtitleField = makeField(QStringLiteral("字幕导出"), subtitleCheck, QString(), false,
                                    aiPane);
    aiLayout->addWidget(subtitleField, 0, Qt::AlignLeft);
    stack->addWidget(aiPane);

    // ---- Shortcuts ----------------------------------------------------------
    auto *shortcutPane = makePane(stack);
    auto *shortcutLayout = qobject_cast<QVBoxLayout *>(shortcutPane->layout());
    auto *shortcutRow = new QWidget(shortcutPane);
    auto *shortcutRowLayout = new QHBoxLayout(shortcutRow);
    shortcutRowLayout->setContentsMargins(10, 8, 10, 8);
    shortcutRowLayout->setSpacing(6);
    shortcutRowLayout->addWidget(makeKbd(QStringLiteral("Space"), shortcutRow));
    shortcutRowLayout->addWidget(
        Ui::label(QStringLiteral("播放 / 暂停"), 12, 400, Theme::Text2, shortcutRow));
    shortcutRowLayout->addStretch(1);
    shortcutRowLayout->addWidget(
        Ui::label(QStringLiteral("视频预览"), 12, 400, Theme::Text2, shortcutRow));
    auto *shortcutLine = Ui::separator(false, shortcutPane);
    shortcutLayout->addWidget(shortcutRow);
    shortcutLayout->addWidget(shortcutLine);
    stack->addWidget(shortcutPane);

    // ---- General ------------------------------------------------------------
    auto *generalPane = makePane(stack);
    auto *generalLayout = qobject_cast<QVBoxLayout *>(generalPane->layout());
    auto *themeTabs = new SegmentTabs(
        {QStringLiteral("深色（推荐）"), QStringLiteral("跟随系统")}, SegmentTabs::Segmented,
        generalPane);
    themeTabs->setCurrentIndex(config.general.theme == QLatin1String("system") ? 1 : 0);
    auto *themeField = makeField(QStringLiteral("外观主题"), themeTabs, QString(), false,
                                 generalPane);
    generalLayout->addWidget(themeField, 0, Qt::AlignLeft);
    auto *themeHint = Ui::label(
        QStringLiteral("当前版本仅提供深色主题；跟随系统偏好会保存，后续版本生效。"),
        11, 400, Theme::Text3, generalPane);
    themeHint->setWordWrap(true);
    generalLayout->addWidget(themeHint);
    generalLayout->addSpacing(16);

#if defined(LIVEMIX_PLATFORM_WINDOWS)
    const QString backgroundLabel =
        QStringLiteral("关闭窗口时最小化到系统托盘继续跑任务");
#else
    const QString backgroundLabel =
        QStringLiteral("关闭窗口时最小化到菜单栏继续跑任务");
#endif
    QVector<CheckBox *> generalChecks;
    auto *startup = makeCheckRow({QStringLiteral("开机时自动启动"), backgroundLabel,
                                  QStringLiteral("任务完成时播放提示音"),
                                  QStringLiteral("系统通知")},
                                 {}, generalPane, &generalChecks);
    if (generalChecks.size() == 4) {
        generalChecks[0]->setChecked(config.general.launchAtLogin);
        generalChecks[1]->setChecked(config.general.minimizeToBackground);
        generalChecks[2]->setChecked(config.general.soundOnComplete);
        generalChecks[3]->setChecked(config.general.systemNotifications);
    }
    auto *startupField = makeField(QStringLiteral("启动与后台"), startup, QString(), false,
                                   generalPane);
    generalLayout->addWidget(startupField, 0, Qt::AlignLeft);
    generalLayout->addSpacing(16);

    auto *about = new QWidget(generalPane);
    auto *aboutLayout = new QVBoxLayout(about);
    aboutLayout->setContentsMargins(0, 0, 0, 0);
    aboutLayout->setSpacing(8);
    aboutLayout->addWidget(
        Ui::label(QStringLiteral("LiveMix Studio v%1 · Apache-2.0 开源")
                      .arg(QStringLiteral(LIVEMIX_VERSION)),
                  12, 400, Theme::Text2, about));
    auto *checkUpdate = new Button(QStringLiteral("检查更新"), Button::Default, about);
    connect(checkUpdate, &QAbstractButton::clicked, this,
            [this] { emit toastRequested(QStringLiteral("已是最新版本")); });
    aboutLayout->addWidget(checkUpdate, 0, Qt::AlignLeft);
    auto *aboutField = makeField(QStringLiteral("关于"), about, QString(), false, generalPane);
    generalLayout->addWidget(aboutField, 0, Qt::AlignLeft);
    stack->addWidget(generalPane);

    auto persist = [this, apiInput, keyInput, draftInput, exportInput, cacheInput,
                    recordingInput, segmentInput, limitInput, autoClean, promptSelect,
                    subtitleCheck, themeTabs, generalChecks,
                    cleanedPath, firstInteger](QString *error) -> bool {
        if (!m_config) {
            if (error) {
                *error = QStringLiteral("配置未加载。");
            }
            return false;
        }

        AppConfig updated = m_config->config();
        updated.service.apiUrl = apiInput->text().trimmed();
        updated.service.apiKey = keyInput->text().trimmed();

        updated.storage.draftDir =
            cleanedPath(draftInput->text(), updated.storage.draftDir);
        updated.storage.exportDir =
            cleanedPath(exportInput->text(), updated.storage.exportDir);
        updated.storage.cacheDir =
            cleanedPath(cacheInput->text(), updated.storage.cacheDir);
        updated.storage.recordingDir =
            cleanedPath(recordingInput->text(), updated.storage.recordingDir);
        updated.storage.autoCleanCache = autoClean->isChecked();

        bool limitOk = false;
        const int cacheLimit = firstInteger(limitInput->text(), &limitOk);
        if (limitOk) {
            updated.storage.cacheLimitGb = qBound(1, cacheLimit, 1000);
        }

        bool segmentOk = false;
        const int seconds = segmentInput->text().toInt(&segmentOk);
        updated.recording.segmentSeconds =
            segmentOk ? qBound(1, seconds, 30) : updated.recording.segmentSeconds;

        updated.ai.defaultPrompt = promptSelect->currentText();
        updated.ai.exportSrt = subtitleCheck->isChecked();

        updated.general.theme = themeTabs->currentIndex() == 1
            ? QStringLiteral("system")
            : QStringLiteral("dark");
        if (generalChecks.size() == 4) {
            updated.general.launchAtLogin = generalChecks[0]->isChecked();
            updated.general.minimizeToBackground = generalChecks[1]->isChecked();
            updated.general.soundOnComplete = generalChecks[2]->isChecked();
            updated.general.systemNotifications = generalChecks[3]->isChecked();
        }

        m_config->setConfig(updated);
        return m_config->save(error);
    };

    auto *saveButton = new Button(QStringLiteral("保存设置"), Button::Primary, this);
    header->addAction(saveButton);
    connect(saveButton, &QAbstractButton::clicked, this, [this, persist, keyInput] {
        QString error;
        if (!persist(&error)) {
            emit toastRequested(QStringLiteral("保存设置失败：%1").arg(error));
            return;
        }
        if (m_serviceWarning) {
            m_serviceWarning->setVisible(keyInput->text().trimmed().isEmpty());
        }
        emit toastRequested(QStringLiteral("设置已保存"));
        emit settingsSaved();
    });

    connect(testButton, &QAbstractButton::clicked, this,
            [this, testButton, apiInput, keyInput, persist] {
                const QString key = keyInput->text().trimmed();
                QUrl apiUrl = QUrl::fromUserInput(apiInput->text().trimmed());
                const QString scheme = apiUrl.scheme().toLower();
                if (key.isEmpty() || !apiUrl.isValid()
                    || (scheme != QLatin1String("http")
                        && scheme != QLatin1String("https"))) {
                    emit toastRequested(
                        QStringLiteral("请填写有效的 API 地址和密钥"));
                    return;
                }

                QString error;
                if (!persist(&error)) {
                    emit toastRequested(QStringLiteral("保存设置失败：%1").arg(error));
                    return;
                }
                if (m_serviceWarning) {
                    m_serviceWarning->setVisible(false);
                }
                emit settingsSaved();

                QString path = apiUrl.path();
                if (!path.endsWith(QLatin1Char('/'))) {
                    path += QLatin1Char('/');
                }
                apiUrl.setPath(path + QStringLiteral("models"));

                QNetworkRequest request(apiUrl);
                request.setRawHeader("Authorization",
                                     QByteArray("Bearer ") + key.toUtf8());
                request.setRawHeader("Accept", "application/json");

                testButton->setEnabled(false);
                testButton->setText(QStringLiteral("测试中…"));
                QNetworkReply *reply = m_network->get(request);
                QTimer::singleShot(8000, reply, [reply] {
                    if (reply->isRunning()) {
                        reply->abort();
                    }
                });
                connect(reply, &QNetworkReply::finished, this,
                        [this, reply, testButton] {
                            testButton->setEnabled(true);
                            testButton->setText(QStringLiteral("测试连接并保存"));
                            const QNetworkReply::NetworkError networkError = reply->error();
                            const int status = reply->attribute(
                                QNetworkRequest::HttpStatusCodeAttribute).toInt();
                            if (networkError == QNetworkReply::OperationCanceledError) {
                                emit toastRequested(QStringLiteral(
                                    "连接超时，请检查 API 地址或网络"));
                            } else if (networkError != QNetworkReply::NoError
                                       && status == 0) {
                                emit toastRequested(QStringLiteral("连接失败：%1")
                                                        .arg(reply->errorString()));
                            } else if (status == 200) {
                                emit toastRequested(QStringLiteral(
                                    "连接成功：网关可达，ASR / LLM 均可用"));
                            } else if (status == 401 || status == 403) {
                                emit toastRequested(
                                    QStringLiteral("服务可达，但 API 密钥无效（HTTP %1）")
                                        .arg(status));
                            } else if (status > 0) {
                                emit toastRequested(
                                    QStringLiteral("服务可达（HTTP %1）").arg(status));
                            } else {
                                emit toastRequested(QStringLiteral("连接失败：%1")
                                                        .arg(reply->errorString()));
                            }
                            reply->deleteLater();
                        });
            });

    stack->setCurrentIndex(0);
    bodyLayout()->addWidget(outer);
    bodyLayout()->addStretch(1);
    fitStack();
}
