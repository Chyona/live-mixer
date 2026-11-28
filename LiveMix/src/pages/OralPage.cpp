#include "OralPage.h"

#include "../core/AppConfig.h"
#include "../core/MediaUtils.h"
#include "../media/FfmpegLocator.h"
#include "../media/FfmpegUtils.h"
#include "../oral/OralDraftDownloadTask.h"
#include "../oral/OralPipelineService.h"
#include "../ui/Controls.h"
#include "../ui/Icons.h"
#include "../ui/StudioPosters.h"
#include "../ui/StudioWidgets.h"
#include "../ui/WindowChrome.h"

#include <QApplication>
#include <QAudioOutput>
#include <QClipboard>
#include <QDateTime>
#include <QDesktopServices>
#include <QDir>
#include <QDragEnterEvent>
#include <QDragLeaveEvent>
#include <QDragMoveEvent>
#include <QDropEvent>
#include <QFile>
#include <QFileDialog>
#include <QFileInfo>
#include <QFontMetrics>
#include <QEnterEvent>
#include <QGraphicsScene>
#include <QGraphicsVideoItem>
#include <QGraphicsView>
#include <QGridLayout>
#include <QGuiApplication>
#include <QHBoxLayout>
#include <QImageReader>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLabel>
#include <QMediaPlayer>
#include <QMessageBox>
#include <QMimeData>
#include <QPainter>
#include <QPainterPath>
#include <QProcess>
#include <QScrollArea>
#include <QThread>
#include <QUrl>
#include <QRegularExpression>
#include <QSaveFile>
#include <QScrollBar>
#include <QStandardPaths>
#include <QtMath>
#include <QStackedWidget>
#include <QTextEdit>
#include <QToolButton>
#include <QTimer>
#include <QVBoxLayout>

#include <cmath>
#include <functional>

namespace {

QString cssColor(const QColor &color)
{
    return color.name(QColor::HexArgb);
}

const QStringList &imageMaterialSuffixes()
{
    static const QStringList suffixes = {
        QStringLiteral("png"), QStringLiteral("jpg"), QStringLiteral("jpeg"),
        QStringLiteral("webp"), QStringLiteral("gif"), QStringLiteral("bmp"),
    };
    return suffixes;
}

const QStringList &videoMaterialSuffixes()
{
    static const QStringList suffixes = {
        QStringLiteral("mp4"), QStringLiteral("mov"), QStringLiteral("m4v"),
        QStringLiteral("mkv"), QStringLiteral("avi"), QStringLiteral("webm"),
    };
    return suffixes;
}

bool isImageMaterialFile(const QFileInfo &info)
{
    return imageMaterialSuffixes().contains(info.suffix().toLower());
}

bool isVideoMaterialFile(const QFileInfo &info)
{
    return videoMaterialSuffixes().contains(info.suffix().toLower());
}

bool isSupportedMaterialFile(const QFileInfo &info)
{
    return info.isFile() && (isImageMaterialFile(info) || isVideoMaterialFile(info));
}

QString materialFileFilter()
{
    QStringList patterns;
    for (const QString &suffix : imageMaterialSuffixes()) {
        patterns.append(QStringLiteral("*.%1").arg(suffix));
    }
    for (const QString &suffix : videoMaterialSuffixes()) {
        patterns.append(QStringLiteral("*.%1").arg(suffix));
    }
    return QStringLiteral("图片与视频 (%1)").arg(patterns.join(QLatin1Char(' ')));
}

// 按目标区域等比铺满并居中裁剪绘制图片。
void paintCoverImage(QPainter &painter, const QRectF &rect, const QImage &image)
{
    if (image.isNull() || rect.width() < 2 || rect.height() < 2) {
        return;
    }
    painter.setRenderHint(QPainter::SmoothPixmapTransform, true);
    const qreal imageAspect = qreal(image.width()) / image.height();
    const qreal rectAspect = rect.width() / rect.height();
    QRectF source(0, 0, image.width(), image.height());
    if (imageAspect > rectAspect) {
        const qreal width = image.height() * rectAspect;
        source = QRectF((image.width() - width) / 2.0, 0, width, image.height());
    } else {
        const qreal height = image.width() / rectAspect;
        source = QRectF(0, (image.height() - height) / 2.0, image.width(), height);
    }
    painter.drawImage(rect, image, source);
}

QString sourceFileMetaHtml(const StudioData::OralSource &source)
{
    // 与设计稿一致：分辨率使用等宽字体，分隔点用弱化描边色。
    return QStringLiteral(
               "%1 <span style=\"color:%2\">·</span> "
               "<span style=\"font-family:'%3'\">%4</span> "
               "<span style=\"color:%2\">·</span> 本地文件")
        .arg(source.size.toHtmlEscaped(), cssColor(Theme::BorderStrong),
             Theme::monoFont(9).family(), source.resolution.toHtmlEscaped());
}

// 最近口播卡片：只读取已缓存的首帧，不做阻塞截帧；缺失时由后台队列补齐。
QImage cachedResultFramePreview(const StudioData::OralResult &result)
{
    if (!result.previewPath.isEmpty()) {
        const QImage image(result.previewPath);
        if (!image.isNull()) {
            return image;
        }
    }
    if (result.filePath.isEmpty()) {
        return QImage();
    }
    const QString cachePath = FfmpegUtils::posterFrameCachePath(result.filePath);
    if (!QFileInfo::exists(cachePath) || QFileInfo(cachePath).size() <= 0) {
        return QImage();
    }
    return QImage(cachePath);
}

// 结果页预览：优先读流水线缓存的成片 0 秒帧；旧历史记录没有缓存时补截一次。
QImage loadResultFramePreview(const QString &previewPath,
                              const QString &videoPath)
{
    if (!previewPath.isEmpty()) {
        const QImage image(previewPath);
        if (!image.isNull()) {
            return image;
        }
    }
    if (videoPath.isEmpty() || !QFileInfo::exists(videoPath)) {
        return QImage();
    }
    const QString cachePath = FfmpegUtils::posterFrameCachePath(videoPath);
    if (!QFileInfo::exists(cachePath) || QFileInfo(cachePath).size() <= 0) {
        QString error;
        FfmpegUtils::extractThumbnail(videoPath, 0, cachePath, &error);
    }
    return QImage(cachePath);
}

// 重新包装所需的原始输入。优先使用结果记录里持久化的口播路径与素材；
// 旧版本历史没有这些字段时，回退到当时生成的剪辑计划 JSON。
struct OralRestoredInputs
{
    QString sourcePath;
    QString sourceName;
    qint64 sourceDurationMs = 0;
    int sourceWidth = 0;
    int sourceHeight = 0;
    qint64 sourceSizeBytes = 0;
    QVector<StudioData::OralMaterial> materials;
    int missingMaterials = 0;
};

OralRestoredInputs restoreOralInputs(const StudioData::OralResult &result)
{
    OralRestoredInputs inputs;

    // 剪辑计划同时保存口播元数据与素材理解结果，还能为老历史补齐素材。
    Oral::EditPlan plan;
    bool hasPlan = false;
    if (!result.editPlanPath.isEmpty()) {
        QFile file(result.editPlanPath);
        if (file.open(QIODevice::ReadOnly)) {
            const QJsonObject object =
                QJsonDocument::fromJson(file.readAll()).object();
            if (!object.isEmpty()) {
                plan = Oral::EditPlan::fromJson(object);
                hasPlan = !plan.sourcePath.isEmpty()
                    || !plan.materials.isEmpty();
            }
        }
    }

    inputs.sourcePath = result.sourcePath;
    if (inputs.sourcePath.isEmpty() && hasPlan) {
        // 新计划同时保存剪口气后的视频与原始上传路径；重新包装必须回到原片。
        inputs.sourcePath = plan.originalSourcePath.isEmpty()
            ? plan.sourcePath
            : plan.originalSourcePath;
    }
    if (hasPlan) {
        inputs.sourceName = plan.sourceName;
        inputs.sourceDurationMs = plan.durationMs;
        inputs.sourceWidth = plan.sourceWidth;
        inputs.sourceHeight = plan.sourceHeight;
        inputs.sourceSizeBytes = plan.sourceSizeBytes;
    }

    QVector<StudioData::OralMaterial> candidates = result.materials;
    if (candidates.isEmpty() && hasPlan) {
        for (int index = 0; index < plan.materials.size(); ++index) {
            const Oral::MaterialProfile &profile = plan.materials.at(index);
            if (profile.path.trimmed().isEmpty()) {
                continue;
            }
            StudioData::OralMaterial material = StudioData::oralMaterialFromFile(
                profile.path, QFileInfo(profile.path).size(),
                profile.kind == Oral::MaterialKind::Video, index);
            // 保留导入时的 ID 与展示名，重新包装才能命中分析缓存，
            // 避免重复 ASR 与素材理解。
            if (!profile.id.isEmpty()) {
                material.id = profile.id;
            }
            if (!profile.name.isEmpty()) {
                material.name = profile.name;
            }
            candidates.append(material);
        }
    }

    if (!inputs.sourcePath.trimmed().isEmpty()) {
        const QFileInfo info(inputs.sourcePath);
        if (info.isFile()) {
            inputs.sourcePath = info.absoluteFilePath();
        } else {
            inputs.sourcePath.clear();
        }
    }

    for (const StudioData::OralMaterial &material : candidates) {
        const QFileInfo info(material.filePath);
        if (material.filePath.trimmed().isEmpty() || !info.isFile()) {
            ++inputs.missingMaterials;
            continue;
        }
        StudioData::OralMaterial restored = material;
        restored.filePath = info.absoluteFilePath();
        inputs.materials.append(restored);
    }
    return inputs;
}

// 按缩略图尺寸解码图片，避免把原始大图整张读进内存。
QImage loadMaterialPreview(const QString &path)
{
    QImageReader reader(path);
    reader.setAutoTransform(true);
    if (!reader.canRead()) {
        return QImage();
    }
    const QSize sourceSize = reader.size();
    if (sourceSize.isValid()) {
        const QSize target(640, 480);
        QSize decodeSize = sourceSize;
        // 只缩小、不放大：小图保持原分辨率，大图按铺满缩略图的尺寸解码。
        if (sourceSize.width() > target.width()
            || sourceSize.height() > target.height()) {
            decodeSize = sourceSize.scaled(target, Qt::KeepAspectRatioByExpanding);
        }
        // 极端长宽比的图片不让解码尺寸失控，稍后由绘制端再等比裁剪。
        const QSize limit(target.width() * 2, target.height() * 2);
        if (decodeSize.width() > limit.width()
            || decodeSize.height() > limit.height()) {
            const QSize cap(qMin(sourceSize.width(), limit.width()),
                            qMin(sourceSize.height(), limit.height()));
            decodeSize = decodeSize.scaled(cap, Qt::KeepAspectRatio);
        }
        if (decodeSize != sourceSize) {
            reader.setScaledSize(decodeSize);
        }
    }
    return reader.read();
}

QScrollArea *makeScrollArea(QWidget *content, QWidget *parent)
{
    auto *area = new QScrollArea(parent);
    area->setFrameShape(QFrame::NoFrame);
    area->setWidgetResizable(true);
    area->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    area->setVerticalScrollBarPolicy(Qt::ScrollBarAsNeeded);
    area->setStyleSheet(QStringLiteral(
        "QScrollArea { background: transparent; border: none; }"
        "QScrollArea > QWidget > QWidget { background: transparent; }"));
    area->viewport()->setAutoFillBackground(false);
    area->setWidget(content);
    return area;
}

QWidget *makeIconCircle(Icons::Glyph glyph, int size, const QColor &foreground,
                        const QColor &background, const QColor &border,
                        QWidget *parent)
{
    auto *host = new QWidget(parent);
    host->setFixedSize(size, size);
    host->setObjectName(QStringLiteral("oralIconCircle"));
    host->setStyleSheet(
        QStringLiteral("#oralIconCircle { background:%1; border:1px solid %2;"
                       " border-radius:%3px; }")
            .arg(cssColor(background), cssColor(border))
            .arg(size / 2));
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);
    layout->addWidget(Studio::iconLabel(glyph, qMax(10, qRound(size * 0.5)), foreground,
                                        host),
                      0, Qt::AlignCenter);
    return host;
}

QWidget *makeSoftPill(Icons::Glyph glyph, const QString &text, const QColor &foreground,
                      const QColor &background, const QColor &border,
                      QWidget *parent)
{
    auto *host = new QWidget(parent);
    host->setObjectName(QStringLiteral("oralSoftPill"));
    host->setStyleSheet(
        QStringLiteral("#oralSoftPill { background:%1; border:1px solid %2;"
                       " border-radius:9px; }")
            .arg(cssColor(background), cssColor(border)));
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(8, 2, 8, 2);
    layout->setSpacing(4);
    layout->addWidget(Studio::iconLabel(glyph, 10, foreground, host), 0,
                      Qt::AlignVCenter);
    auto *label = Ui::label(text, 9, QFont::Medium, foreground, host);
    layout->addWidget(label, 0, Qt::AlignVCenter);
    host->setSizePolicy(QSizePolicy::Fixed, QSizePolicy::Fixed);
    return host;
}

QWidget *makeBullet(const QString &text, const QColor &color, QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(6);
    layout->addWidget(Studio::iconLabel(Icons::Glyph::Check, 10, color, row), 0,
                      Qt::AlignVCenter);
    auto *label = Ui::label(text, 10, QFont::Normal, Theme::Text3, row);
    layout->addWidget(label, 0, Qt::AlignVCenter);
    layout->addStretch(1);
    return row;
}

QWidget *makeSectionHeading(const QString &title, const QString &hint,
                            QWidget *right, QWidget *parent)
{
    auto *row = new QWidget(parent);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(8);
    layout->addWidget(Ui::label(title, 13, QFont::DemiBold, Theme::Text1, row), 0,
                      Qt::AlignVCenter);
    if (!hint.isEmpty()) {
        layout->addWidget(Ui::label(hint, 10, QFont::Normal, Theme::Text3, row), 0,
                          Qt::AlignVCenter);
    }
    layout->addStretch(1);
    if (right) {
        layout->addWidget(right, 0, Qt::AlignVCenter);
    }
    return row;
}

QLabel *monoLabel(const QString &text, int pixelSize, const QColor &color,
                  QWidget *parent)
{
    auto *label = Ui::label(text, pixelSize, QFont::Normal, color, parent);
    label->setFont(Theme::monoFont(pixelSize));
    return label;
}

QWidget *makeGenerationPill(QLabel **valueLabel, QWidget *parent)
{
    auto *host = new QWidget(parent);
    host->setObjectName(QStringLiteral("oralGenerationPill"));
    host->setStyleSheet(
        QStringLiteral("#oralGenerationPill { background:%1; border:1px solid %2;"
                       " border-radius:5px; }")
            .arg(Theme::Surface2.name(), Theme::Border.name()));
    host->setFixedHeight(21);
    auto *layout = new QHBoxLayout(host);
    layout->setContentsMargins(8, 0, 8, 0);
    layout->setSpacing(6);
    layout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 12, Theme::Text3, host));
    layout->addWidget(Ui::label(QStringLiteral("生成时间"), 10, QFont::Normal,
                                Theme::Text3, host));
    auto *value = monoLabel(QString(), 9, Theme::Text1, host);
    value->setToolTip(QString());
    layout->addWidget(value);
    *valueLabel = value;
    return host;
}


/* ------------------------------------------------------------------ */
/* 可点击的自定义卡片                                                   */
/* ------------------------------------------------------------------ */

class OralHeroButton : public QAbstractButton
{
public:
    explicit OralHeroButton(QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setCursor(Qt::PointingHandCursor);
        setMinimumHeight(244);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Preferred);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        QLinearGradient base(rect.topLeft(), rect.bottomLeft());
        base.setColorAt(0, Theme::Surface1);
        base.setColorAt(1, Theme::Surface1);
        painter.setBrush(base);
        painter.setPen(QPen(m_hovered ? Theme::alpha(Theme::Primary, 0.45)
                                      : Theme::Border,
                            1.0));
        painter.drawRoundedRect(rect, 12, 12);

        QRadialGradient glow(rect.right(), rect.top(), rect.width() * 0.9);
        glow.setColorAt(0, QColor(110, 139, 255, 33));
        glow.setColorAt(0.55, QColor(110, 139, 255, 7));
        glow.setColorAt(1, QColor(110, 139, 255, 0));
        QPainterPath clip;
        clip.addRoundedRect(rect, 12, 12);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }

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

private:
    bool m_hovered = false;
};

class OralHeroArt : public QWidget
{
public:
    explicit OralHeroArt(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setAttribute(Qt::WA_TransparentForMouseEvents, true);
        setMinimumWidth(190);
        setMinimumHeight(208);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
    }

    void setTemplateId(const QString &id)
    {
        m_templateId = id;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF area = QRectF(rect());

        Studio::OralPosterSpec leftSpec;
        leftSpec.templateId = QStringLiteral("magazine");
        leftSpec.caption = StudioData::oralTemplate(QStringLiteral("magazine")).caption;
        leftSpec.ratio = QStringLiteral("9:16");
        leftSpec.badge = StudioData::oralTemplate(QStringLiteral("magazine")).name;
        leftSpec.materials = StudioData::demoMaterials().mid(2, 1);
        leftSpec.timecode.clear();
        painter.save();
        painter.translate(area.left() + area.width() * 0.18,
                          area.top() + 18 + 87);
        painter.rotate(-7);
        painter.setOpacity(0.4);
        Studio::paintOralPoster(painter, QRectF(-49, -87, 98, 174), leftSpec);
        painter.restore();

        const StudioData::OralTemplate tmpl = StudioData::oralTemplate(m_templateId);
        Studio::OralPosterSpec rightSpec;
        rightSpec.templateId = m_templateId;
        rightSpec.headline = tmpl.headline;
        rightSpec.caption = QStringLiteral("记住这三点");
        rightSpec.keywords = {QStringLiteral("油皮"), QStringLiteral("通勤")};
        rightSpec.materials = StudioData::demoMaterials().mid(0, 2);
        rightSpec.timecode = QStringLiteral("00:58");
        rightSpec.progress = 40;
        rightSpec.ratio = QStringLiteral("9:16");
        painter.save();
        painter.translate(area.right() - 60, 0 + 104);
        painter.rotate(3);
        Studio::paintOralPoster(painter, QRectF(-58, -104, 117, 208), rightSpec);
        painter.restore();

        auto drawFloat = [&painter](const QRectF &r, const QColor &dot,
                                    const QString &text, Icons::Glyph glyph,
                                    const QColor &iconColor) {
            painter.setPen(QPen(Theme::BorderStrong, 1.0));
            painter.setBrush(Theme::alpha(Theme::Surface2, 0.95));
            painter.drawRoundedRect(r, 6, 6);
            const QRectF iconRect(r.left() + 7, r.center().y() - 5, 10, 10);
            Q_UNUSED(dot);
            Icons::paint(&painter, iconRect, glyph, iconColor, 2.0);
            painter.setFont(Theme::font(9));
            painter.setPen(Theme::Text2);
            painter.drawText(r.adjusted(21, 0, -6, 0), Qt::AlignLeft | Qt::AlignVCenter,
                             text);
        };
        drawFloat(QRectF(area.left(), area.top() + 62, 120, 22),
                  Theme::Ai, QStringLiteral("素材匹配 8/8"), Icons::Glyph::Sparkle,
                  Theme::Ai);
        const QRectF saveRect(area.right() - 134, area.bottom() - 72, 134, 22);
        painter.setPen(QPen(Theme::BorderStrong, 1.0));
        painter.setBrush(Theme::alpha(Theme::Surface2, 0.95));
        painter.drawRoundedRect(saveRect, 6, 6);
        painter.setPen(Qt::NoPen);
        painter.setBrush(Theme::Success);
        painter.drawEllipse(QPointF(saveRect.left() + 11, saveRect.center().y()), 3,
                            3);
        painter.setFont(Theme::font(9));
        painter.setPen(Theme::Text2);
        painter.drawText(saveRect.adjusted(19, 0, -5, 0),
                         Qt::AlignLeft | Qt::AlignVCenter,
                         QStringLiteral("字幕 · 转场已生成"));
    }

private:
    QString m_templateId = QStringLiteral("clean");
};

class GradientPanel : public QWidget
{
public:
    explicit GradientPanel(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        auto *layout = new QVBoxLayout(this);
        layout->setContentsMargins(16, 16, 16, 16);
        layout->setSpacing(0);
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(Theme::Surface1);
        painter.drawRoundedRect(rect, 12, 12);
        QRadialGradient glow(rect.left(), rect.top(), rect.width());
        glow.setColorAt(0, QColor(255, 178, 36, 18));
        glow.setColorAt(0.6, QColor(255, 178, 36, 3));
        glow.setColorAt(1, QColor(255, 178, 36, 0));
        QPainterPath clip;
        clip.addRoundedRect(rect, 12, 12);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }
};

class SourcePreview : public QAbstractButton
{
public:
    SourcePreview(const StudioData::OralSource &source, QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_source(source)
    {
        setCursor(Qt::PointingHandCursor);
        // 设计稿按 16:9 展示口播原片，宽度变化时高度随之调整。
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        setFixedHeight(315);
        setToolTip(source.filePath.isEmpty()
                       ? QStringLiteral("示例口播（原型演示）")
                       : QStringLiteral("点击播放口播原片；拖入新视频可替换"));
    }

    void setSource(const StudioData::OralSource &source)
    {
        m_source = source;
        update();
    }

    bool hasHeightForWidth() const override { return true; }
    int heightForWidth(int width) const override
    {
        return qMax(120, qRound(width * 9.0 / 16.0));
    }
    QSize sizeHint() const override
    {
        return QSize(560, qRound(560 * 9.0 / 16.0));
    }

protected:
    void resizeEvent(QResizeEvent *event) override
    {
        QAbstractButton::resizeEvent(event);
        const int target = heightForWidth(width());
        if (height() != target) {
            setFixedHeight(target);
        }
    }

    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF frame = QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        QPainterPath clip;
        clip.addRoundedRect(QRectF(rect()), 8, 8);
        painter.setClipPath(clip);
        if (!m_source.preview.isNull()) {
            paintCoverImage(painter, QRectF(rect()), m_source.preview);
        } else {
            Studio::paintStudioScene(painter, QRectF(rect()));
        }

        QLinearGradient shade(0, 0, 0, height());
        shade.setColorAt(0, QColor(4, 5, 9, 115));
        shade.setColorAt(0.36, QColor(4, 5, 9, 0));
        shade.setColorAt(0.58, QColor(4, 5, 9, 0));
        shade.setColorAt(1, QColor(4, 5, 9, 204));
        painter.fillRect(rect(), shade);
        painter.setClipping(false);

        // 左上角标签：口播原片为胶囊，示例素材为小方标，与设计稿一致。
        const QFont pillFont = Theme::font(10, QFont::Medium);
        const QFontMetrics pillMetrics(pillFont);
        const QString pillText = QStringLiteral("口播原片");
        const QRectF pillRect(8, 8, pillMetrics.horizontalAdvance(pillText) + 16,
                              21);
        painter.setPen(QPen(Theme::alpha(Theme::Primary, 0.35), 1.0));
        painter.setBrush(Studio::primarySoft());
        painter.drawRoundedRect(pillRect, pillRect.height() / 2.0,
                                pillRect.height() / 2.0);
        painter.setFont(pillFont);
        painter.setPen(Theme::Primary);
        painter.drawText(pillRect, Qt::AlignCenter, pillText);

        if (m_source.sample) {
            const QFont sampleFont = Theme::font(9);
            const QFontMetrics sampleMetrics(sampleFont);
            const QString sampleText = QStringLiteral("示例素材");
            const QRectF sampleRect(
                pillRect.right() + 6,
                pillRect.center().y()
                    - (sampleMetrics.height() + 4) / 2.0,
                sampleMetrics.horizontalAdvance(sampleText) + 12,
                sampleMetrics.height() + 4);
            painter.setPen(QPen(Theme::BorderStrong, 1.0));
            painter.setBrush(QColor(0, 0, 0, 115));
            painter.drawRoundedRect(sampleRect, 4, 4);
            painter.setFont(sampleFont);
            painter.setPen(QColor(255, 255, 255, 191));
            painter.drawText(sampleRect, Qt::AlignCenter, sampleText);
        }

        const QString resolution = m_source.resolution;
        const QFont resolutionFont = Theme::monoFont(9);
        const QFontMetrics resolutionMetrics(resolutionFont);
        const QRectF resolutionRect(
            width() - 8 - resolutionMetrics.horizontalAdvance(resolution) - 12,
            8, resolutionMetrics.horizontalAdvance(resolution) + 12, 19);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 140));
        painter.drawRoundedRect(resolutionRect, 4, 4);
        painter.setFont(resolutionFont);
        painter.setPen(QColor(255, 255, 255, 217));
        painter.drawText(resolutionRect, Qt::AlignCenter, resolution);

        const QRectF playCircle(width() / 2.0 - 22, height() / 2.0 - 22, 44, 44);
        painter.setPen(QPen(QColor(255, 255, 255, 77), 1.0));
        painter.setBrush(QColor(0, 0, 0, 153));
        painter.drawEllipse(playCircle);
        QPainterPath play;
        play.moveTo(playCircle.center().x() - 4, playCircle.center().y() - 7);
        play.lineTo(playCircle.center().x() + 8, playCircle.center().y());
        play.lineTo(playCircle.center().x() - 4, playCircle.center().y() + 7);
        play.closeSubpath();
        painter.setBrush(QColor(255, 255, 255, 230));
        painter.setPen(Qt::NoPen);
        painter.drawPath(play);

        // 有声波形：2px 细条 + 2px 间距，左侧紧凑排列；时长贴右下角。
        const qreal waveHeight = 16;
        const qreal barWidth = 2;
        const qreal barGap = 2;
        const qreal waveCenterY = height() - 8 - waveHeight / 2.0;
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(255, 255, 255, 153));
        for (int index = 0; index < 34; ++index) {
            const int barHeight = 22 + (index * 37) % 68;
            const qreal barPixelHeight = waveHeight * barHeight / 100.0;
            painter.drawRoundedRect(
                QRectF(12 + index * (barWidth + barGap),
                       waveCenterY - barPixelHeight / 2.0, barWidth,
                       barPixelHeight),
                barWidth / 2.0, barWidth / 2.0);
        }
        painter.setFont(Theme::monoFont(9));
        painter.setPen(QColor(255, 255, 255, 179));
        painter.drawText(QRectF(width() - 60, height() - 24, 48, 16),
                         Qt::AlignRight | Qt::AlignVCenter, m_source.duration);

        painter.setClipping(false);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(Qt::NoBrush);
        painter.drawRoundedRect(frame, 8, 8);
    }

private:
    StudioData::OralSource m_source;
};

class OutputChoiceCard : public QAbstractButton
{
public:
    OutputChoiceCard(const QString &title, const QString &description,
                     const QString &badge, Icons::Glyph glyph, bool selected,
                     QWidget *parent = nullptr)
        : QAbstractButton(parent)
        , m_title(title)
        , m_description(description)
        , m_badge(badge)
        , m_glyph(glyph)
        , m_selected(selected)
    {
        setCursor(Qt::PointingHandCursor);
        setFixedHeight(49);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    }

    void setSelected(bool selected)
    {
        m_selected = selected;
        update();
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(m_selected ? Theme::alpha(Theme::Primary, 0.55)
                                      : Theme::Border,
                            1.0));
        painter.setBrush(m_selected ? Theme::alpha(Theme::Primary, 0.14)
                                    : Theme::alpha(Theme::Surface2, 0.45));
        painter.drawRoundedRect(rect, 7, 7);

        const QRectF iconRect(10, 10, 28, 28);
        painter.setPen(QPen(m_selected ? Theme::alpha(Theme::Primary, 0.35)
                                       : Theme::Border,
                            1.0));
        painter.setBrush(m_selected ? Theme::alpha(Theme::Primary, 0.15)
                                    : Theme::Surface2);
        painter.drawRoundedRect(iconRect, 6, 6);
        Icons::paint(&painter, iconRect.adjusted(8, 8, -8, -8), m_glyph,
                     m_selected ? Theme::Primary : Theme::Text3, 2.0);

        painter.setFont(Theme::font(10, QFont::Medium));
        painter.setPen(Theme::Text1);
        const QRectF titleRect(47, 7, width() - 80, 17);
        painter.drawText(titleRect, Qt::AlignLeft | Qt::AlignVCenter, m_title);
        if (!m_badge.isEmpty()) {
            const QFontMetrics metrics(Theme::font(8));
            const int badgeWidth = metrics.horizontalAdvance(m_badge) + 8;
            const QRectF badgeRect(47 + QFontMetrics(Theme::font(10, QFont::Medium))
                                            .horizontalAdvance(m_title)
                                     + 6,
                                   8, badgeWidth, 14);
            painter.setPen(Qt::NoPen);
            painter.setBrush(Theme::alpha(Theme::Primary, 0.15));
            painter.drawRoundedRect(badgeRect, 3, 3);
            painter.setFont(Theme::font(8));
            painter.setPen(Theme::Primary);
            painter.drawText(badgeRect, Qt::AlignCenter, m_badge);
        }
        painter.setFont(Theme::font(9));
        painter.setPen(Theme::Text3);
        painter.drawText(QRectF(47, 24, width() - 80, 16),
                         Qt::AlignLeft | Qt::AlignVCenter, m_description);

        const QRectF radio(width() - 23, height() / 2.0 - 7, 14, 14);
        painter.setPen(QPen(m_selected ? Theme::Primary : Theme::BorderStrong, 1.0));
        painter.setBrush(m_selected ? Theme::Primary : Qt::NoBrush);
        painter.drawEllipse(radio);
        if (m_selected) {
            Icons::paint(&painter, radio.adjusted(3, 3, -3, -3), Icons::Glyph::Check,
                         Theme::OnAccent, 2.6);
        }
    }

private:
    QString m_title;
    QString m_description;
    QString m_badge;
    Icons::Glyph m_glyph;
    bool m_selected;
};

class MaterialTile : public Card
{
public:
    // 名称行固定高度：2px 上下内边距 + 8px 文字行高，与设计稿的紧凑卡片一致。
    static constexpr int kBottomHeight = 16;

    MaterialTile(const StudioData::OralMaterial &material,
                 std::function<void()> onRemove,
                 std::function<void()> onPreview, QWidget *parent = nullptr)
        : Card(Theme::alpha(Theme::Surface2, 0.6), Theme::Border, 6, parent)
        , m_onRemove(std::move(onRemove))
        , m_onPreview(std::move(onPreview))
    {
        setHoverEnabled(true);
        setHoverColors(Theme::alpha(Theme::Surface2, 0.6), Theme::BorderStrong);
        // 六列网格下缩略图保持 4:3，名称行固定 16px；宽度变化时在
        // resizeEvent 中同步高度，避免素材卡被同行更高的卡片拉伸。
        setFixedHeight(51 + kBottomHeight);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
        contentLayout()->setContentsMargins(0, 0, 0, 0);
        contentLayout()->setSpacing(0);

        if (!material.filePath.isEmpty()) {
            setClickable(true);
            setToolTip(QStringLiteral("点击预览「%1」").arg(material.name));
            connect(this, &Card::clicked, this, [this] {
                if (m_onPreview) {
                    m_onPreview();
                }
            });
        }

        auto *art = new Studio::MaterialVisualWidget(this);
        art->setMaterial(material);
        art->setRoundedFrame(6, Theme::Border);
        m_art = art;
        contentLayout()->addWidget(art, 1);

        auto *bottom = new QWidget(this);
        bottom->setFixedHeight(kBottomHeight);
        auto *bottomLayout = new QHBoxLayout(bottom);
        bottomLayout->setContentsMargins(4, 2, 4, 2);
        bottomLayout->setSpacing(2);
        auto *name = new ElidedLabel(material.name, bottom);
        name->setPixelSize(8);
        name->setTextColor(Theme::Text2);
        bottomLayout->addWidget(name, 1);
        bottomLayout->addWidget(Ui::label(material.kind, 7, QFont::Normal,
                                          Theme::Text3, bottom));
        contentLayout()->addWidget(bottom);
        bottom->show();

        // 六列网格的角标更紧凑：7px 文字、2px 圆角与内边距。
        auto makeMiniTag = [this](const QString &text, const QColor &color,
                                  const QColor &background, bool mono) {
            QLabel *label = Studio::tagLabel(text, color, background, 7, this);
            label->setStyleSheet(
                QStringLiteral("color:%1;background:%2;border-radius:2px;padding:1px 2px;")
                    .arg(color.name(QColor::HexArgb),
                         background.name(QColor::HexArgb)));
            if (mono) {
                label->setFont(Theme::monoFont(7));
            }
            label->adjustSize();
            label->show();
            return label;
        };

        m_tag = makeMiniTag(material.tag, QColor(255, 255, 255, 230),
                            QColor(0, 0, 0, 166), false);
        if (!material.duration.isEmpty()) {
            m_duration = makeMiniTag(material.duration, QColor(255, 255, 255, 217),
                                     QColor(0, 0, 0, 140), true);
        }

        m_remove = new GlyphButton(Icons::Glyph::Trash, 16, this);
        m_remove->setGlyphSize(8);
        m_remove->setCornerRadius(3);
        m_remove->setColors(Qt::white, Qt::white);
        m_remove->setBackgroundColors(QColor(0, 0, 0, 153), Theme::Danger);
        m_remove->setToolTipText(QStringLiteral("移除「%1」").arg(material.name));
        // 与设计稿一致：移除按钮只在悬停素材卡时出现。
        m_remove->hide();
        connect(m_remove, &QAbstractButton::clicked, this, [this] {
            if (m_onRemove) {
                m_onRemove();
            }
        });
    }

    Studio::MaterialVisualWidget *visualWidget() const { return m_art; }

protected:
    void enterEvent(QEnterEvent *event) override
    {
        Card::enterEvent(event);
        if (m_art) {
            m_art->setRoundedFrame(6, Theme::BorderStrong);
        }
        if (m_remove) {
            m_remove->show();
            m_remove->raise();
        }
    }

    void leaveEvent(QEvent *event) override
    {
        if (m_art) {
            m_art->setRoundedFrame(6, Theme::Border);
        }
        if (m_remove) {
            m_remove->hide();
        }
        Card::leaveEvent(event);
    }

    void resizeEvent(QResizeEvent *event) override
    {
        Card::resizeEvent(event);
        const int target = qRound(width() * 3.0 / 4.0) + kBottomHeight;
        if (height() != target) {
            setFixedHeight(target);
        }
        const int tileHeight = qMax(height(), target);
        // 类型角标贴卡片左下角（与设计稿一致，覆盖在名称行左侧）。
        if (m_tag) {
            m_tag->move(2, qMax(0, tileHeight - m_tag->height() - 2));
        }
        if (m_duration) {
            m_duration->move(2, 2);
        }
        if (m_remove) {
            m_remove->move(width() - m_remove->width() - 2, 2);
        }
        if (m_remove) {
            m_remove->raise();
        }
        if (m_tag) {
            m_tag->raise();
        }
        if (m_duration) {
            m_duration->raise();
        }
    }

private:
    std::function<void()> m_onRemove;
    std::function<void()> m_onPreview;
    Studio::MaterialVisualWidget *m_art = nullptr;
    QLabel *m_tag = nullptr;
    QLabel *m_duration = nullptr;
    GlyphButton *m_remove = nullptr;
};

class AddMaterialTile : public QAbstractButton
{
public:
    explicit AddMaterialTile(QWidget *parent = nullptr)
        : QAbstractButton(parent)
    {
        setCursor(Qt::PointingHandCursor);
        // 六列网格下上传入口随行高自适应；空状态保持与素材卡一致的最小高度。
        setMinimumHeight(56);
        setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Expanding);
        setToolTip(QStringLiteral("添加相关素材"));
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        QPen pen(m_hovered ? Theme::alpha(Theme::Primary, 0.5)
                           : Theme::alpha(Theme::BorderStrong, 0.7),
                 1.0, Qt::DashLine);
        pen.setDashPattern({4, 4});
        painter.setPen(pen);
        painter.setBrush(Theme::alpha(Theme::Surface2, 0.2));
        painter.drawRoundedRect(rect, 6, 6);
        // 图标 14px + 间距 2px + 8px 文字，整体垂直居中。
        Icons::paint(&painter, QRectF(width() / 2.0 - 7, height() / 2.0 - 14, 14, 14),
                     Icons::Glyph::Plus,
                     m_hovered ? Theme::Primary : Theme::Text3, 2.0);
        painter.setFont(Theme::font(8));
        painter.setPen(m_hovered ? Theme::Primary : Theme::Text3);
        painter.drawText(QRectF(0, height() / 2.0 + 2, width(), 12),
                         Qt::AlignCenter, QStringLiteral("上传素材"));
    }

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

private:
    bool m_hovered = false;
};

// 可接收文件拖放的区块卡片：口播视频与相关素材共用。
class MediaDropCard : public Studio::SectionCard
{
public:
    MediaDropCard(Icons::Glyph glyph, const QString &title, const QString &meta,
                  const QString &dropHint,
                  std::function<void(const QStringList &)> onFilesDropped,
                  QWidget *parent = nullptr)
        : Studio::SectionCard(glyph, title, meta, parent)
        , m_onFilesDropped(std::move(onFilesDropped))
    {
        setAcceptDrops(true);
        setToolTip(dropHint);
    }

protected:
    void paintEvent(QPaintEvent *event) override
    {
        Studio::SectionCard::paintEvent(event);
        if (!m_dragging) {
            return;
        }
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(1.5, 1.5, -1.5, -1.5);
        QPen pen(Theme::alpha(Theme::Primary, 0.85), 1.5, Qt::DashLine);
        pen.setDashPattern({5, 4});
        painter.setPen(pen);
        painter.setBrush(Theme::alpha(Theme::Primary, 0.07));
        painter.drawRoundedRect(rect, 9, 9);
    }

    void dragEnterEvent(QDragEnterEvent *event) override
    {
        if (hasLocalFiles(event->mimeData())) {
            setDragging(true);
            event->acceptProposedAction();
        } else {
            event->ignore();
        }
    }

    void dragMoveEvent(QDragMoveEvent *event) override
    {
        if (hasLocalFiles(event->mimeData())) {
            event->acceptProposedAction();
        } else {
            event->ignore();
        }
    }

    void dragLeaveEvent(QDragLeaveEvent *event) override
    {
        setDragging(false);
        Studio::SectionCard::dragLeaveEvent(event);
    }

    void dropEvent(QDropEvent *event) override
    {
        setDragging(false);
        QStringList paths;
        if (event->mimeData()) {
            const QList<QUrl> urls = event->mimeData()->urls();
            for (const QUrl &url : urls) {
                if (url.isLocalFile()) {
                    paths.append(url.toLocalFile());
                }
            }
        }
        if (paths.isEmpty()) {
            event->ignore();
            return;
        }
        event->acceptProposedAction();
        if (m_onFilesDropped) {
            m_onFilesDropped(paths);
        }
    }

private:
    static bool hasLocalFiles(const QMimeData *mimeData)
    {
        if (!mimeData) {
            return false;
        }
        const QList<QUrl> urls = mimeData->urls();
        for (const QUrl &url : urls) {
            if (url.isLocalFile()) {
                return true;
            }
        }
        return false;
    }

    void setDragging(bool dragging)
    {
        if (m_dragging == dragging) {
            return;
        }
        m_dragging = dragging;
        update();
    }

    std::function<void(const QStringList &)> m_onFilesDropped;
    bool m_dragging = false;
};

class DarkStage : public QWidget
{
public:
    explicit DarkStage(QWidget *parent = nullptr)
        : QWidget(parent)
    {
        setStyleSheet(QStringLiteral("background:transparent;"));
    }

protected:
    void paintEvent(QPaintEvent *) override
    {
        QPainter painter(this);
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QRectF rect = QRectF(this->rect()).adjusted(0.5, 0.5, -0.5, -0.5);
        painter.setPen(QPen(Theme::Border, 1.0));
        painter.setBrush(QColor(QStringLiteral("#0c0e14")));
        painter.drawRoundedRect(rect, 10, 10);
        QRadialGradient glow(rect.center().x(), rect.top() + rect.height() * 0.1,
                             rect.width() * 0.8);
        glow.setColorAt(0, QColor(QStringLiteral("#1b2130")));
        glow.setColorAt(1, QColor(QStringLiteral("#0c0e14")));
        QPainterPath clip;
        clip.addRoundedRect(rect, 10, 10);
        painter.setClipPath(clip);
        painter.fillRect(rect, glow);
    }
};

QWidget *makeInfoPanel(const QString &title, Icons::Glyph glyph, QWidget *content,
                       QWidget *parent)
{
    auto *box = new QWidget(parent);
    box->setObjectName(QStringLiteral("oralInfoPanel"));
    box->setStyleSheet(QStringLiteral(
        "#oralInfoPanel { background:%1; border:1px solid %2; border-radius:7px; }")
                           .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *layout = new QVBoxLayout(box);
    layout->setContentsMargins(10, 8, 10, 8);
    layout->setSpacing(4);
    auto *header = new QWidget(box);
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(0, 0, 0, 0);
    headerLayout->setSpacing(4);
    headerLayout->addWidget(Studio::iconLabel(glyph, 10, Theme::Ai, header));
    headerLayout->addWidget(Ui::label(title, 9, QFont::Normal, Theme::Text3, header));
    headerLayout->addStretch(1);
    layout->addWidget(header);
    layout->addWidget(content);
    return box;
}

QWidget *makeFileRow(Icons::Glyph glyph, const QString &name, const QString &tag,
                     const QString &description, QWidget *right, QWidget *parent)
{
    auto *row = new QWidget(parent);
    row->setObjectName(QStringLiteral("oralFileRow"));
    row->setStyleSheet(
        QStringLiteral("#oralFileRow { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.45).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(10, 10, 10, 10);
    layout->setSpacing(10);
    layout->addWidget(makeIconCircle(glyph, 32, Theme::Text2, Theme::Surface1,
                                     Theme::Border, row),
                      0, Qt::AlignVCenter);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(2);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(6);
    auto *nameLabel = new ElidedLabel(name, row);
    nameLabel->setPixelSize(11);
    nameLabel->setFontWeight(QFont::Medium);
    nameLabel->setTextColor(Theme::Text1);
    nameRow->addWidget(nameLabel, 1);
    nameRow->addWidget(Studio::tagLabel(tag, Theme::Text2, Theme::Surface3, 8, row), 0);
    texts->addLayout(nameRow);
    auto *descriptionLabel = Ui::label(description, 9, QFont::Normal, Theme::Text3, row);
    descriptionLabel->setWordWrap(false);
    texts->addWidget(descriptionLabel);
    layout->addLayout(texts, 1);
    if (right) {
        layout->addWidget(right, 0, Qt::AlignVCenter);
    }
    return row;
}

void setResultPlayIcon(QToolButton *button, bool playing)
{
    if (!button) {
        return;
    }
    button->setIcon(Icons::glyphIcon(playing ? Icons::Glyph::Pause
                                             : Icons::Glyph::Play,
                                     Qt::white, 18, 2.2));
}

QToolButton *makeResultPlayOverlay(QWidget *parent)
{
    auto *button = new QToolButton(parent);
    button->setFixedSize(44, 44);
    button->setCursor(Qt::PointingHandCursor);
    button->setIconSize(QSize(18, 18));
    button->setStyleSheet(
        QStringLiteral(
            "QToolButton { background:rgba(0,0,0,118);"
            " border:1px solid rgba(255,255,255,76); border-radius:22px; }"
            "QToolButton:hover { background:rgba(0,0,0,163); }"));
    button->setIcon(Icons::glyphIcon(Icons::Glyph::Play, Qt::white, 18, 2.2));
    return button;
}

QWidget *makeCopyButton(const QString &text, const QString &label,
                        std::function<void()> onCopy, QWidget *parent)
{
    auto *button = new GlyphButton(Icons::Glyph::Copy, 18, parent);
    button->setGlyphSize(11);
    button->setColors(Theme::Text3, Theme::Primary);
    button->setToolTipText(QStringLiteral("复制%1").arg(label));
    QObject::connect(button, &QAbstractButton::clicked, button,
                     [onCopy = std::move(onCopy), text] {
                         Q_UNUSED(text);
                         if (onCopy) {
                             onCopy();
                         }
                     });
    return button;
}

} // namespace

/* ------------------------------------------------------------------ */
/* OralPage                                                             */
/* ------------------------------------------------------------------ */

OralPage::OralPage(ConfigStore *config, QWidget *parent)
    : QWidget(parent)
    , m_config(config)
{
    m_project = StudioData::defaultOralProject();
    loadHistory();

    m_runTimer = new QTimer(this);
    m_runTimer->setInterval(100);
    // 真实进度由工作线程推送；这里只负责让「已用时 / 预计还需」持续走动。
    connect(m_runTimer, &QTimer::timeout, this, [this] {
        if (!m_hasRun) {
            return;
        }
        m_run.elapsedMs = QDateTime::currentMSecsSinceEpoch() - m_runStartedAt;
        updateProcessing();
        const int percent = qMin(99, static_cast<int>(std::floor(m_run.progress)));
        emit taskStatusChanged(
            QStringLiteral("智能口播中 %1% · %2")
                .arg(percent)
                .arg(StudioData::formatElapsed(m_run.elapsedMs)),
            true);
    });

    m_playTimer = new QTimer(this);
    m_playTimer->setInterval(100);
    connect(m_playTimer, &QTimer::timeout, this, &OralPage::updatePlayback);

    m_clockTimer = new QTimer(this);
    m_clockTimer->setInterval(10 * 1000);
    connect(m_clockTimer, &QTimer::timeout, this, &OralPage::updateGenerationClock);
    m_clockTimer->start();

    buildUi();
}

OralPage::~OralPage()
{
    // 页面销毁时请求取消并给工作线程一点收尾时间；仍在进行的网络请求无法
    // 强行中断，因此超时后让线程自行结束，避免阻塞应用退出。
    destroyPipeline();
    resetResultPlayer();
    if (m_draftThread) {
        m_draftThread->quit();
        if (!m_draftThread->wait(3000)) {
            // 网络请求可能仍在进行；断开界面回调，线程结束后自行回收。
            disconnect(m_draftThread, nullptr, this, nullptr);
        }
        m_draftThread = nullptr;
    }
}

void OralPage::buildUi()
{
    auto *root = new QVBoxLayout(this);
    root->setContentsMargins(0, 0, 0, 0);
    root->setSpacing(0);

    m_stack = new QStackedWidget(this);
    m_stack->setStyleSheet(QStringLiteral("QStackedWidget { background: transparent; }"));
    root->addWidget(m_stack);

    m_hubScreen = new QWidget(m_stack);
    auto *hubRoot = new QVBoxLayout(m_hubScreen);
    hubRoot->setContentsMargins(0, 0, 0, 0);
    hubRoot->setSpacing(0);
    m_hubContent = new QWidget;
    m_hubLayout = new QVBoxLayout(m_hubContent);
    m_hubLayout->setContentsMargins(22, 18, 22, 24);
    m_hubLayout->setSpacing(0);
    m_hubLayout->setAlignment(Qt::AlignTop);
    m_hubScroll = makeScrollArea(m_hubContent, m_hubScreen);
    hubRoot->addWidget(m_hubScroll, 1);
    m_stack->addWidget(m_hubScreen);

    m_setupScreen = new QWidget(m_stack);
    m_stack->addWidget(m_setupScreen);

    m_processingScreen = new QWidget(m_stack);
    m_stack->addWidget(m_processingScreen);
    buildProcessingScreen();

    m_resultScreen = new QWidget(m_stack);
    m_stack->addWidget(m_resultScreen);

    rebuildHub();
}

bool OralPage::isRunning() const
{
    return m_hasRun && !m_run.done;
}

/* ------------------------------------------------------------------ */
/* 首页                                                                 */
/* ------------------------------------------------------------------ */

void OralPage::showHub()
{
    // 离开结果页时停止播放，避免成片声音在其它页面继续播放。
    if (m_screen == Screen::Result) {
        resetResultPlayer();
    }
    m_screen = Screen::Hub;
    rebuildHub();
    m_stack->setCurrentWidget(m_hubScreen);
    // 返回首页时恢复离开前的列表滚动位置；新成片生成后该值为 0，回到顶部。
    const int scrollTop = m_hubScrollTop;
    QTimer::singleShot(0, this, [this, scrollTop] {
        if (m_hubScroll && m_hubScroll->verticalScrollBar()) {
            m_hubScroll->verticalScrollBar()->setValue(scrollTop);
        }
    });
}

void OralPage::rememberHubScroll()
{
    if (m_screen == Screen::Hub && m_hubScroll
        && m_hubScroll->verticalScrollBar()) {
        m_hubScrollTop = m_hubScroll->verticalScrollBar()->value();
    }
}

void OralPage::showSetup()
{
    if (m_screen == Screen::Result) {
        resetResultPlayer();
    }
    rememberHubScroll();
    m_screen = Screen::Setup;
    rebuildSetup();
    m_stack->setCurrentWidget(m_setupScreen);
}

void OralPage::showProcessing()
{
    if (m_screen == Screen::Result) {
        resetResultPlayer();
    }
    m_screen = Screen::Processing;
    m_stack->setCurrentWidget(m_processingScreen);
}

void OralPage::showResult()
{
    rememberHubScroll();
    m_screen = Screen::Result;
    rebuildResult();
    m_stack->setCurrentWidget(m_resultScreen);
}

void OralPage::rebuildHub()
{
    // 旧卡片海报即将随布局清空，先取消登记避免异步截帧回调更新悬空指针。
    m_recentPosterWidgets.clear();
    Studio::clearLayout(m_hubLayout);
    m_hubLayout->addWidget(buildHubHeader());

    auto *stats = new QWidget(m_hubContent);
    auto *statsLayout = new QHBoxLayout(stats);
    statsLayout->setContentsMargins(0, 0, 0, 16);
    statsLayout->setSpacing(10);
    auto addStat = [&](const QString &html) {
        statsLayout->addWidget(Ui::richLabel(html, 10, Theme::Text3, stats));
    };
    addStat(QStringLiteral("本月已生成 <b style=\"color:%1\">24</b> 条口播")
                .arg(Theme::Text1.name()));
    statsLayout->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                     Theme::BorderStrong, stats));
    addStat(QStringLiteral("平均包装用时 <b style=\"color:%1\">3 分 12 秒</b>")
                .arg(Theme::Text1.name()));
    statsLayout->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                     Theme::BorderStrong, stats));
    addStat(QStringLiteral("素材匹配率 <b style=\"color:%1\">92%</b>")
                .arg(Theme::Success.name()));
    statsLayout->addStretch(1);
    auto *readyDot = new StatusDot(6, Theme::Success, stats);
    statsLayout->addWidget(readyDot);
    statsLayout->addWidget(Ui::label(QStringLiteral("本地素材库已就绪 · 8 个相关素材"),
                                     10, QFont::Normal, Theme::Text3, stats));
    m_hubLayout->addWidget(stats);

    auto *heroRow = new QWidget(m_hubContent);
    heroRow->setMinimumHeight(244);
    heroRow->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    auto *heroLayout = new QHBoxLayout(heroRow);
    heroLayout->setContentsMargins(0, 0, 0, 0);
    heroLayout->setSpacing(14);
    heroLayout->addWidget(buildHubHero(), 146);
    heroLayout->addWidget(buildHubProcessPanel(), 100);
    m_hubLayout->addWidget(heroRow);

    m_hubLayout->addSpacing(20);
    auto *recentMeta = new QWidget(m_hubContent);
    auto *recentMetaLayout = new QHBoxLayout(recentMeta);
    recentMetaLayout->setContentsMargins(0, 0, 0, 0);
    recentMetaLayout->setSpacing(4);
    recentMetaLayout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 10,
                                                  Theme::Text3, recentMeta));
    recentMetaLayout->addWidget(Ui::label(QStringLiteral("自动保存生成记录"), 10,
                                          QFont::Normal, Theme::Text3, recentMeta));
    auto *recentHeading = makeSectionHeading(
        QStringLiteral("最近口播"),
        m_history.isEmpty() ? QStringLiteral("暂无记录")
                            : QStringLiteral("%1 个项目").arg(m_history.size()),
        recentMeta, m_hubContent);
    auto *recentHeadingRow = new QWidget(m_hubContent);
    auto *recentHeadingLayout = new QHBoxLayout(recentHeadingRow);
    recentHeadingLayout->setContentsMargins(0, 0, 0, 8);
    recentHeadingLayout->setSpacing(0);
    recentHeadingLayout->addWidget(recentHeading, 1);
    m_hubLayout->addWidget(recentHeadingRow);

    if (!m_history.isEmpty()) {
        auto *recentGrid = new QWidget(m_hubContent);
        auto *recentLayout = new QGridLayout(recentGrid);
        recentLayout->setContentsMargins(0, 0, 0, 0);
        recentLayout->setHorizontalSpacing(12);
        recentLayout->setVerticalSpacing(12);
        for (int index = 0; index < m_history.size(); ++index) {
            recentLayout->addWidget(createRecentCard(m_history.at(index), recentGrid),
                                    index / 3, index % 3);
        }
        for (int column = 0; column < 3; ++column) {
            recentLayout->setColumnStretch(column, 1);
        }
        m_hubLayout->addWidget(recentGrid);
    } else {
        auto *empty = new Studio::EmptyRecent(
            QStringLiteral("还没有口播记录"),
            QStringLiteral("完成第一个口播包装后，成片、发布文案与剪映草稿会自动保存到这里，随时回看或重新包装。"),
            {QStringLiteral("成片自动下载"), QStringLiteral("发布文案可复制"),
             QStringLiteral("可回看与重新包装")},
            QStringLiteral("用示例口播试包装"),
            QStringLiteral("开始第一个口播"), m_hubContent);
        connect(empty, &Studio::EmptyRecent::startRequested, this,
                &OralPage::startProject);
        connect(empty, &Studio::EmptyRecent::sampleRequested, this,
                &OralPage::startSampleProject);
        m_hubLayout->addWidget(empty);
    }

    m_hubLayout->addSpacing(16);
    auto *tip = new QWidget(m_hubContent);
    tip->setObjectName(QStringLiteral("oralHubTip"));
    tip->setStyleSheet(
        QStringLiteral("#oralHubTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *tipLayout = new QHBoxLayout(tip);
    tipLayout->setContentsMargins(12, 9, 12, 9);
    tipLayout->setSpacing(8);
    tipLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 12, Theme::Ai, tip));
    auto *tipLabel = Ui::label(
        QStringLiteral("小提示：开始包装后可在「包装设置」里选择模板，AI 会自动按模板节奏调整字幕、素材插入密度和音乐风格。"),
        10, QFont::Normal, Theme::Text3, tip);
    tipLabel->setWordWrap(true);
    tipLayout->addWidget(tipLabel, 1);
    auto *ratio = new QWidget(tip);
    auto *ratioLayout = new QHBoxLayout(ratio);
    ratioLayout->setContentsMargins(0, 0, 0, 0);
    ratioLayout->setSpacing(4);
    ratioLayout->addWidget(Studio::iconLabel(Icons::Glyph::Palette, 10,
                                              Theme::Text2, ratio));
    ratioLayout->addWidget(Ui::label(QStringLiteral("支持 9:16 / 16:9"), 10,
                                      QFont::Normal, Theme::Text2, ratio));
    tipLayout->addWidget(ratio, 0, Qt::AlignVCenter);
    m_hubLayout->addWidget(tip);
}

QWidget *OralPage::buildHubHeader()
{
    auto *header = new PageHeader(
        QStringLiteral("智能口播"),
        QStringLiteral("上传真人口播与相关素材，选择包装模板，一键成片。"),
        m_hubContent);
    header->layout()->setContentsMargins(0, 0, 0, 16);
    auto *newChip = new Chip(QStringLiteral("新功能"), Chip::Ai, header);
    header->setTitleMeta(newChip);
    auto *start = new Button(QStringLiteral("开始口播包装"), Button::Primary, header);
    start->setLeadingGlyph(Icons::Glyph::Sparkle);
    connect(start, &QAbstractButton::clicked, this, &OralPage::startProject);
    header->addAction(start);
    return header;
}

QWidget *OralPage::buildHubHero()
{
    auto *card = new OralHeroButton(m_hubContent);
    auto *layout = new QHBoxLayout(card);
    layout->setContentsMargins(16, 16, 16, 16);
    layout->setSpacing(12);

    auto *left = new QWidget(card);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(0);
    leftLayout->addWidget(
        makeSoftPill(Icons::Glyph::Sparkle, QStringLiteral("AI 口播 · 素材 · 成片"),
                     Theme::Ai, Studio::aiSoft(),
                     Theme::alpha(Theme::Ai, 0.30), left),
        0, Qt::AlignLeft);
    leftLayout->addSpacing(9);
    leftLayout->addWidget(Ui::label(QStringLiteral("真人口播 · 一键包装"), 17,
                                    QFont::DemiBold, Theme::Text1, left));
    leftLayout->addSpacing(4);
    auto *description = Ui::label(
        QStringLiteral("上传一段真人口播和相关素材，AI 理解内容后自动插入画面、字幕与节奏包装，直接生成可发布的成片。"),
        11, QFont::Normal, Theme::Text2, left);
    description->setWordWrap(true);
    description->setMaximumWidth(360);
    leftLayout->addWidget(description);
    leftLayout->addSpacing(10);
    leftLayout->addWidget(makeBullet(QStringLiteral("智能素材匹配，B-Roll 自动插入"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("模板、字幕、转场、BGM 一次到位"),
                                     Theme::Ai, left));
    leftLayout->addWidget(makeBullet(QStringLiteral("成片生成后自动下载"), Theme::Ai,
                                     left));
    leftLayout->addWidget(makeBullet(
        QStringLiteral("可同时生成可编辑的剪映草稿"), Theme::Ai, left));
    leftLayout->addStretch(1);

    auto *action = new QWidget(left);
    auto *actionLayout = new QHBoxLayout(action);
    actionLayout->setContentsMargins(0, 0, 0, 0);
    actionLayout->setSpacing(4);
    auto *actionLabel = Ui::label(QStringLiteral("开始口播包装"), 11,
                                  QFont::Medium, Theme::Primary, action);
    actionLayout->addWidget(actionLabel);
    actionLayout->addWidget(Studio::iconLabel(Icons::Glyph::ChevronRight, 11,
                                               Theme::Primary, action));
    actionLayout->addStretch(1);
    leftLayout->addWidget(action);

    layout->addWidget(left, 108);
    auto *art = new OralHeroArt(card);
    art->setTemplateId(m_project.templateId);
    layout->addWidget(art, 92);
    connect(card, &QAbstractButton::clicked, this, &OralPage::startProject);
    return card;
}

QWidget *OralPage::buildHubProcessPanel()
{
    auto *panel = new GradientPanel(m_hubContent);
    auto *layout = qobject_cast<QVBoxLayout *>(panel->layout());
    layout->setSpacing(0);

    auto *header = new QWidget(panel);
    auto *headerLayout = new QHBoxLayout(header);
    headerLayout->setContentsMargins(0, 0, 0, 0);
    headerLayout->setSpacing(8);
    headerLayout->addWidget(makeIconCircle(Icons::Glyph::Wand, 24, Theme::Ai,
                                           Studio::aiSoft(),
                                           Theme::alpha(Theme::Ai, 0.3), header),
                            0, Qt::AlignVCenter);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(1);
    titles->addWidget(Ui::label(QStringLiteral("上传素材，三步成片"), 12,
                                QFont::DemiBold, Theme::Text1, header));
    titles->addWidget(Ui::label(QStringLiteral("全程无需手动剪辑，AI 自动完成"), 9,
                                QFont::Normal, Theme::Text3, header));
    headerLayout->addLayout(titles, 1);
    layout->addWidget(header);
    layout->addSpacing(12);

    struct StepInfo {
        QString title;
        QString description;
        Icons::Glyph glyph;
    };
    const QVector<StepInfo> steps = {
        {QStringLiteral("上传素材"),
         QStringLiteral("一段真人口播，加上产品、场景等相关素材。"),
         Icons::Glyph::Upload},
        {QStringLiteral("包装设置"),
         QStringLiteral("6 套模板任选，字幕、B-Roll 与音乐自动适配。"),
         Icons::Glyph::Palette},
        {QStringLiteral("一键成片"),
         QStringLiteral("AI 理解内容、匹配画面并渲染，完成后自动下载。"),
         Icons::Glyph::Wand},
    };
    for (int index = 0; index < steps.size(); ++index) {
        auto *row = new QWidget(panel);
        auto *rowLayout = new QHBoxLayout(row);
        rowLayout->setContentsMargins(0, 0, 0, 0);
        rowLayout->setSpacing(10);
        auto *number = new QLabel(QString::number(index + 1), row);
        number->setFixedSize(24, 24);
        number->setAlignment(Qt::AlignCenter);
        number->setFont(Theme::monoFont(9));
        number->setStyleSheet(
            QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                           "border-radius:12px;")
                .arg(Theme::Text2.name(), Theme::Surface2.name(),
                     Theme::BorderStrong.name()));
        rowLayout->addWidget(number, 0, Qt::AlignTop);
        auto *texts = new QVBoxLayout;
        texts->setContentsMargins(0, 0, 0, 0);
        texts->setSpacing(1);
        auto *titleRow = new QHBoxLayout;
        titleRow->setContentsMargins(0, 0, 0, 0);
        titleRow->setSpacing(5);
        titleRow->addWidget(Studio::iconLabel(steps.at(index).glyph, 10,
                                               Theme::Text3, row));
        titleRow->addWidget(Ui::label(steps.at(index).title, 10, QFont::Medium,
                                       Theme::Text1, row));
        titleRow->addStretch(1);
        texts->addLayout(titleRow);
        auto *desc = Ui::label(steps.at(index).description, 9, QFont::Normal,
                               Theme::Text3, row);
        desc->setWordWrap(true);
        texts->addWidget(desc);
        rowLayout->addLayout(texts, 1);
        layout->addWidget(row);
        if (index < steps.size() - 1) {
            layout->addSpacing(8);
        }
    }
    layout->addStretch(1);

    auto *note = new QWidget(panel);
    note->setObjectName(QStringLiteral("oralProcessNote"));
    note->setStyleSheet(
        QStringLiteral("#oralProcessNote { background:%1; border:1px solid %2;"
                       " border-radius:6px; }")
            .arg(Theme::alpha(Theme::Surface2, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *noteLayout = new QHBoxLayout(note);
    noteLayout->setContentsMargins(10, 7, 10, 7);
    noteLayout->setSpacing(6);
    noteLayout->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 9, Theme::Text3,
                                             note));
    auto *noteLabel = Ui::label(
        QStringLiteral("可切到其他页面，任务在后台继续，完成后自动下载"), 9,
        QFont::Normal, Theme::Text3, note);
    noteLabel->setWordWrap(true);
    noteLayout->addWidget(noteLabel, 1);
    layout->addSpacing(8);
    layout->addWidget(note);
    return panel;
}

QWidget *OralPage::createTemplateCard(const QString &templateId, bool selected,
                                      bool compact, QWidget *parent)
{
    const StudioData::OralTemplate tmpl = StudioData::oralTemplate(templateId);
    auto *card = new Card(
        selected ? Theme::mix(Theme::Surface1, Theme::Primary, 0.08) : Theme::Surface1,
        selected ? Theme::alpha(Theme::Primary, 0.5) : Theme::Border, 9, parent);
    card->setHoverEnabled(true);
    card->setClickable(true);
    card->setHoverColors(selected ? Theme::mix(Theme::Surface1, Theme::Primary, 0.10)
                                  : Theme::alpha(Theme::Surface2, 0.4),
                         selected ? Theme::alpha(Theme::Primary, 0.6)
                                  : Theme::BorderStrong);
    card->contentLayout()->setContentsMargins(10, 10, 10, 10);
    card->contentLayout()->setSpacing(10);

    auto *row = new QWidget(card);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(10);

    auto *poster = new Studio::OralPosterWidget(row);
    poster->setFixedSize(compact ? 61 : 70, compact ? 108 : 124);
    Studio::OralPosterSpec spec;
    spec.templateId = templateId;
    spec.headline = tmpl.headline;
    spec.caption = tmpl.caption;
    spec.materials = StudioData::demoMaterials().mid(0, 1);
    spec.ratio = QStringLiteral("9:16");
    spec.badge = tmpl.name;
    spec.timecode.clear();
    poster->setSpec(spec);
    layout->addWidget(poster, 0, Qt::AlignTop);

    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 1, 0, 0);
    texts->setSpacing(0);
    auto *nameRow = new QHBoxLayout;
    nameRow->setContentsMargins(0, 0, 0, 0);
    nameRow->setSpacing(6);
    auto *dot = new QWidget(row);
    dot->setFixedSize(6, 6);
    dot->setStyleSheet(QStringLiteral("background:%1;border-radius:3px;")
                           .arg(cssColor(tmpl.accent)));
    nameRow->addWidget(dot, 0, Qt::AlignVCenter);
    nameRow->addWidget(Ui::label(tmpl.name, 11, QFont::Medium, Theme::Text1, row), 0);
    nameRow->addStretch(1);
    auto *check = new QWidget(row);
    check->setFixedSize(15, 15);
    check->setObjectName(QStringLiteral("oralTemplateCheck"));
    check->setStyleSheet(
        QStringLiteral("#oralTemplateCheck { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(selected ? Theme::Primary.name() : QStringLiteral("transparent"),
                 selected ? Theme::Primary.name() : Theme::BorderStrong.name()));
    auto *checkLayout = new QHBoxLayout(check);
    checkLayout->setContentsMargins(0, 0, 0, 0);
    checkLayout->addWidget(
        Studio::iconLabel(Icons::Glyph::Check, 8,
                          selected ? Theme::OnAccent : QColor(0, 0, 0, 0), check),
        0, Qt::AlignCenter);
    nameRow->addWidget(check, 0, Qt::AlignVCenter);
    texts->addLayout(nameRow);
    texts->addSpacing(2);
    texts->addWidget(Ui::label(tmpl.tagline, 9, QFont::Normal, Theme::Text3, row));
    texts->addSpacing(4);
    auto *description = Ui::label(tmpl.description, 9, QFont::Normal, Theme::Text3,
                                  row);
    description->setWordWrap(true);
    description->setMaximumHeight(compact ? 34 : 44);
    texts->addWidget(description);
    texts->addStretch(1);

    auto *tags = new QWidget(row);
    auto *tagsLayout = new QHBoxLayout(tags);
    tagsLayout->setContentsMargins(0, 0, 0, 0);
    tagsLayout->setSpacing(4);
    const int tagCount = compact ? 2 : tmpl.tags.size();
    for (int index = 0; index < qMin(tagCount, tmpl.tags.size()); ++index) {
        tagsLayout->addWidget(Studio::tagLabel(tmpl.tags.at(index), Theme::Text2,
                                                Theme::Surface3, 8, tags),
                              0, Qt::AlignLeft);
    }
    tagsLayout->addStretch(1);
    texts->addWidget(tags);
    layout->addLayout(texts, 1);
    card->contentLayout()->addWidget(row);
    if (templateId != QLatin1String("clean")) {
        card->setToolTip(QStringLiteral(
            "当前版本仅开放「清透讲解」模板，其它模板即将上线"));
    }
    connect(card, &Card::clicked, this,
            [this, templateId] { chooseTemplate(templateId); });
    return card;
}

QWidget *OralPage::createRecentCard(const StudioData::OralResult &result,
                                    QWidget *parent)
{
    auto *card = new Card(Theme::Surface1, Theme::Border, 10, parent);
    card->setFixedHeight(142);
    card->setHoverEnabled(true);
    card->setClickable(true);
    card->setHoverColors(Theme::Surface1, Theme::BorderStrong);
    card->contentLayout()->setContentsMargins(10, 10, 10, 10);
    card->contentLayout()->setSpacing(0);

    auto *row = new QWidget(card);
    auto *layout = new QHBoxLayout(row);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(12);

    auto *poster = new Studio::OralPosterWidget(row);
    poster->setFixedSize(66, 118);
    const StudioData::OralTemplate tmpl = StudioData::oralTemplate(result.templateId);
    Studio::OralPosterSpec spec;
    spec.templateId = result.templateId;
    spec.headline = result.projectName.split(QStringLiteral(" · ")).value(0);
    const QString caption = result.tags.value(1, result.tags.value(0)).mid(1);
    spec.caption = caption.isEmpty() ? tmpl.caption : caption;
    spec.materials = result.materials.isEmpty()
        ? StudioData::demoMaterials().mid(0, 1)
        : result.materials.mid(0, 1);
    spec.ratio = result.ratio;
    spec.badge = QStringLiteral("成片");
    spec.timecode = result.durationLabel;
    // 预览图使用成片首帧；首帧尚未缓存时先展示设计稿海报，后台截帧后就地替换。
    spec.frameImage = cachedResultFramePreview(result);
    poster->setSpec(spec);
    if (spec.frameImage.isNull() && !result.filePath.isEmpty()
        && QFileInfo::exists(result.filePath)) {
        m_recentPosterWidgets[result.filePath].append(poster);
        requestPosterFrame(result.filePath);
    }
    layout->addWidget(poster, 0, Qt::AlignTop);

    auto *texts = new QWidget(row);
    auto *textsLayout = new QVBoxLayout(texts);
    textsLayout->setContentsMargins(0, 1, 0, 0);
    textsLayout->setSpacing(0);
    auto *top = new QHBoxLayout;
    top->setContentsMargins(0, 0, 0, 0);
    top->setSpacing(6);
    top->addWidget(new Chip(QStringLiteral("已完成"), Chip::Ok, texts), 0);
    top->addStretch(1);
    auto *time = Ui::label(StudioData::formatGenerationTime(result.generatedAt), 9,
                           QFont::Normal, Theme::Text3, texts);
    time->setToolTip(QStringLiteral("生成时间：%1")
                         .arg(StudioData::formatGenerationFullTime(result.generatedAt)));
    top->addWidget(time, 0);
    textsLayout->addLayout(top);
    textsLayout->addSpacing(6);
    auto *name = new ElidedLabel(result.projectName, texts);
    name->setPixelSize(12);
    name->setFontWeight(QFont::Medium);
    name->setTextColor(Theme::Text1);
    textsLayout->addWidget(name);
    textsLayout->addSpacing(2);
    auto *meta = Ui::label(QStringLiteral("%1 · %2 · %3")
                               .arg(tmpl.name, result.durationLabel, result.ratio),
                           9, QFont::Normal, Theme::Text3, texts);
    textsLayout->addWidget(meta);
    textsLayout->addStretch(1);
    auto *bottom = new QHBoxLayout;
    bottom->setContentsMargins(0, 0, 0, 0);
    bottom->setSpacing(4);
    auto *title = new ElidedLabel(result.title, texts);
    title->setPixelSize(10);
    title->setTextColor(Theme::Text2);
    bottom->addWidget(title, 1);
    bottom->addWidget(Studio::iconLabel(Icons::Glyph::ChevronRight, 11,
                                         Theme::Text3, texts));
    textsLayout->addLayout(bottom);
    layout->addWidget(texts, 1);
    card->contentLayout()->addWidget(row);
    connect(card, &Card::clicked, this,
            [this, result] { openRecent(result); });
    return card;
}

/* ------------------------------------------------------------------ */
/* 创建流程                                                             */
/* ------------------------------------------------------------------ */

void OralPage::startProject()
{
    openNewProject(false);
}

void OralPage::startSampleProject()
{
    openNewProject(true);
}

void OralPage::openNewProject(bool sample)
{
    if (isRunning()) {
        const auto answer = QMessageBox::question(
            this, QStringLiteral("开始新项目"),
            QStringLiteral("当前有正在生成的智能口播，开始新项目会取消它，是否继续？"),
            QMessageBox::Yes | QMessageBox::No, QMessageBox::No);
        if (answer != QMessageBox::Yes) {
            return;
        }
        m_runTimer->stop();
        m_hasRun = false;
        destroyPipeline();
        emit taskStatusChanged(QString(), false);
    }

    if (m_playTimer) {
        m_playTimer->stop();
    }
    m_playing = false;
    m_playProgress = 0;
    m_draftState = 0;
    m_draftProgress = 0;
    resetResultPlayer();

    const QString templateId = m_project.templateId;
    m_project = StudioData::defaultOralProject();
    m_project.templateId = templateId;
    if (sample) {
        m_project.source = StudioData::demoOralSource();
        m_project.hasSource = true;
        m_project.materials = StudioData::demoMaterials();
    }
    showSetup();
}

void OralPage::rebuildSetup()
{
    auto *root = qobject_cast<QVBoxLayout *>(m_setupScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_setupScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    int scrollValue = 0;
    if (auto *oldScroll = m_setupScreen->findChild<QScrollArea *>()) {
        if (oldScroll->verticalScrollBar()) {
            scrollValue = oldScroll->verticalScrollBar()->value();
        }
    }
    // 旧步骤里的缩略图控件即将重建，先取消登记避免异步回调更新悬空指针。
    m_previewWidgets.clear();
    m_sourcePreviewUpdater = {};
    m_sourceMetaLabel = nullptr;
    Studio::clearLayout(root);

    // 进入素材步骤时补齐真实缩略图；视频缩略图在后台生成。
    ensureMaterialPreviews();

    root->addWidget(buildSetupHeader());
    root->addWidget(Ui::separator(false, m_setupScreen));

    auto *scrollContent = new QWidget;
    auto *body = new QVBoxLayout(scrollContent);
    body->setContentsMargins(22, 14, 22, 16);
    body->setSpacing(0);
    body->setAlignment(Qt::AlignTop);
    body->addWidget(m_project.step == 1 ? buildSetupStepOne()
                                        : buildSetupStepTwo());
    auto *scroll = makeScrollArea(scrollContent, m_setupScreen);
    // 上传页布局按窗口高度调校：保留滚动能力，但不绘制滚动条遮挡内容。
    scroll->setVerticalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
    root->addWidget(scroll, 1);
    root->addWidget(Ui::separator(false, m_setupScreen));
    root->addWidget(buildSetupFooter());

    QTimer::singleShot(0, scroll, [scroll, scrollValue] {
        if (scroll->verticalScrollBar()) {
            scroll->verticalScrollBar()->setValue(scrollValue);
        }
    });
}

QWidget *OralPage::buildSetupHeader()
{
    auto *header = new QWidget(m_setupScreen);
    auto *layout = new QVBoxLayout(header);
    layout->setContentsMargins(22, 12, 22, 12);
    layout->setSpacing(0);

    auto *breadcrumb = new QWidget(header);
    auto *breadcrumbLayout = new QHBoxLayout(breadcrumb);
    breadcrumbLayout->setContentsMargins(0, 0, 0, 0);
    breadcrumbLayout->setSpacing(6);
    auto *back = new LinkLabel(QStringLiteral("智能口播"), breadcrumb);
    back->setPixelSize(10);
    back->setTextColor(Theme::Text3);
    back->setHoverColor(Theme::Text1);
    back->setLeadingGlyph(Icons::Glyph::ChevronLeft, 10);
    back->setUnderlineOnHover(false);
    connect(back, &QAbstractButton::clicked, this, &OralPage::showHub);
    breadcrumbLayout->addWidget(back);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("真人口播"), 10,
                                           QFont::Normal, Theme::Text2, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(
        Ui::label(m_project.step == 1 ? QStringLiteral("上传素材")
                                      : QStringLiteral("包装设置"),
                  10, QFont::Normal, Theme::Text2, breadcrumb));
    auto *savedDot = new StatusDot(6, Theme::Success, breadcrumb);
    breadcrumbLayout->addSpacing(4);
    breadcrumbLayout->addWidget(savedDot);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("草稿已自动保存"), 9,
                                           QFont::Normal, Theme::Text3, breadcrumb));
    breadcrumbLayout->addStretch(1);
    auto *exit = new Button(QStringLiteral("退出编辑"), Button::Ghost, breadcrumb);
    exit->setCompact(true);
    connect(exit, &QAbstractButton::clicked, this, &OralPage::showHub);
    breadcrumbLayout->addWidget(exit);
    layout->addWidget(breadcrumb);
    layout->addSpacing(6);

    auto *titleRow = new QWidget(header);
    auto *titleLayout = new QHBoxLayout(titleRow);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(10);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(2);
    auto *titleLine = new QHBoxLayout;
    titleLine->setContentsMargins(0, 0, 0, 0);
    titleLine->setSpacing(8);
    titleLine->addWidget(Ui::label(QStringLiteral("真人口播包装"), 16,
                                    QFont::DemiBold, Theme::Text1, header));
    if (m_project.step == 1 && m_project.hasSource && m_project.source.sample) {
        titleLine->addWidget(new Chip(QStringLiteral("示例项目"), Chip::Ai, header));
    }
    titleLine->addStretch(1);
    titles->addLayout(titleLine);
    auto *description = Ui::label(
        m_project.step == 1
            ? QStringLiteral("添加一段真人口播和相关素材，AI 会先理解内容，再按模板完成包装。")
            : QStringLiteral("选择包装模板和细节，AI 会按口播节奏自动生成字幕、B-Roll 与音乐。"),
        11, QFont::Normal, Theme::Text2, header);
    description->setWordWrap(true);
    titles->addWidget(description);
    titleLayout->addLayout(titles, 1);

    auto *flow = new QWidget(header);
    auto *flowLayout = new QHBoxLayout(flow);
    flowLayout->setContentsMargins(0, 2, 0, 0);
    flowLayout->setSpacing(5);
    auto addFlow = [&](const QString &text) {
        auto *chip = new QLabel(text, flow);
        chip->setFont(Theme::font(9));
        chip->setStyleSheet(
            QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                           "border-radius:4px;padding:1px 6px;")
                .arg(Theme::Text3.name(), Theme::Surface2.name(),
                     Theme::Border.name()));
        flowLayout->addWidget(chip);
    };
    addFlow(QStringLiteral("口播原片"));
    flowLayout->addWidget(Ui::label(QStringLiteral("→"), 9, QFont::Normal,
                                    Theme::BorderStrong, flow));
    addFlow(QStringLiteral("AI 包装"));
    flowLayout->addWidget(Ui::label(QStringLiteral("→"), 9, QFont::Normal,
                                    Theme::BorderStrong, flow));
    addFlow(QStringLiteral("成片"));
    titleLayout->addWidget(flow, 0, Qt::AlignTop);
    layout->addWidget(titleRow);
    layout->addSpacing(10);

    auto *rail = new Studio::StepRail(header);
    rail->setSteps({QStringLiteral("上传素材"), QStringLiteral("包装设置"),
                    QStringLiteral("生成成片")},
                   {QStringLiteral("口播 + 相关素材"), QStringLiteral("模板与输出"),
                    QStringLiteral("AI 自动包装")});
    rail->setActiveStep(m_project.step);
    connect(rail, &Studio::StepRail::stepSelected, this, [this](int step) {
        if (step == 1) {
            m_project.step = 1;
            rebuildSetup();
        }
    });
    layout->addWidget(rail);
    return header;
}

QWidget *OralPage::buildSetupStepOne()
{
    auto *content = new QWidget;
    auto *layout = new QVBoxLayout(content);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(0);

    if (m_project.hasSource && m_project.source.sample) {
        auto *note = new Studio::Note(
            QStringLiteral(
                "<b style=\"color:%1\">已载入示例口播</b>：这段口播已经过 ASR 识别；"
                "添加至少 1 个相关素材即可体验 AI 包装，替换口播后会重新识别。")
                .arg(Theme::Text1.name()),
            Studio::Note::Ai);
        auto *clear = new LinkLabel(QStringLiteral("清空示例"), note);
        clear->setPixelSize(10);
        clear->setTextColor(Theme::Ai);
        clear->setHoverColor(Theme::Text1);
        clear->setUnderlineOnHover(true);
        connect(clear, &QAbstractButton::clicked, this, [this] {
            clearSource();
            emit toastRequested(QStringLiteral("已清空示例口播，请上传自己的口播视频"));
        });
        note->rowLayout()->addWidget(clear, 0, Qt::AlignTop);
        layout->addWidget(note);
        layout->addSpacing(12);
    }

    auto *grid = new QWidget(content);
    auto *gridLayout = new QHBoxLayout(grid);
    gridLayout->setContentsMargins(0, 0, 0, 0);
    gridLayout->setSpacing(14);

    auto *sourceCard = new MediaDropCard(
        Icons::Glyph::Film, QStringLiteral("口播视频"), QStringLiteral("必填"),
        m_project.hasSource ? QStringLiteral("可将新视频拖到这里替换口播原片")
                            : QStringLiteral("可将口播视频拖到这里上传"),
        [this](const QStringList &paths) { importVideoFromPaths(paths); }, grid);
    auto *sourceAction = new Button(
        m_project.hasSource ? QStringLiteral("替换视频") : QStringLiteral("上传视频"),
        Button::Ghost, sourceCard);
    sourceAction->setCompact(true);
    sourceAction->setLeadingGlyph(Icons::Glyph::Upload);
    connect(sourceAction, &QAbstractButton::clicked, this, &OralPage::openVideoFile);
    sourceCard->headerLayout()->addWidget(sourceAction);
    sourceCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *sourceBody = sourceCard->bodyLayout();
    sourceBody->setSpacing(0);

    if (!m_project.hasSource) {
        auto *drop = new Studio::DropZone(sourceCard);
        drop->setMinimumHeight(268);
        auto *dropLayout = drop->contentLayout();
        dropLayout->setAlignment(Qt::AlignCenter);
        dropLayout->addWidget(makeIconCircle(Icons::Glyph::Upload, 44, Theme::Text2,
                                              Theme::Surface2, Theme::BorderStrong,
                                              drop),
                              0, Qt::AlignHCenter);
        dropLayout->addSpacing(2);
        dropLayout->addWidget(Ui::label(QStringLiteral("拖入口播视频，或上传本地文件"),
                                        12, QFont::Medium, Theme::Text1, drop),
                              0, Qt::AlignHCenter);
        auto *hint = Ui::label(
            QStringLiteral("支持 MP4 / MOV，建议 1080p 以上，1–10 分钟；人声越清晰，识别越准。"),
            10, QFont::Normal, Theme::Text3, drop);
        hint->setWordWrap(true);
        hint->setAlignment(Qt::AlignCenter);
        hint->setMaximumWidth(430);
        // QLabel 自动换行时布局只按单行高度预留，显式给出两行空间避免压住按钮。
        hint->setMinimumHeight(hint->fontMetrics().lineSpacing() * 2);
        dropLayout->addWidget(hint, 0, Qt::AlignHCenter);
        auto *buttons = new QWidget(drop);
        auto *buttonsLayout = new QHBoxLayout(buttons);
        buttonsLayout->setContentsMargins(0, 2, 0, 0);
        buttonsLayout->setSpacing(8);
        auto *upload = new Button(QStringLiteral("上传口播视频"), Button::Primary,
                                  buttons);
        upload->setLeadingGlyph(Icons::Glyph::Upload);
        connect(upload, &QAbstractButton::clicked, this, &OralPage::openVideoFile);
        buttonsLayout->addWidget(upload);
        dropLayout->addWidget(buttons, 0, Qt::AlignHCenter);
        connect(drop, &Studio::DropZone::filesDropped, this,
                [this](const QStringList &paths) { importVideoFromPaths(paths); });
        sourceBody->addWidget(drop);
    } else {
        auto *preview = new SourcePreview(m_project.source, sourceCard);
        m_sourcePreviewUpdater = [preview](const StudioData::OralSource &source) {
            preview->setSource(source);
        };
        connect(preview, &QAbstractButton::clicked, this, [this] {
            if (m_project.source.filePath.isEmpty()) {
                emit toastRequested(QStringLiteral("正在播放口播原片（原型演示）"));
                return;
            }
            if (!QFileInfo::exists(m_project.source.filePath)) {
                emit toastRequested(QStringLiteral("口播视频文件不存在，请重新导入"));
                return;
            }
            emit previewRequested(m_project.source.name,
                                  m_project.source.filePath, true);
        });
        sourceBody->addWidget(preview);
        sourceBody->addSpacing(8);

        auto *fileRow = new QWidget(sourceCard);
        auto *fileLayout = new QHBoxLayout(fileRow);
        fileLayout->setContentsMargins(0, 0, 0, 0);
        fileLayout->setSpacing(8);
        auto *fileTexts = new QVBoxLayout;
        fileTexts->setContentsMargins(0, 0, 0, 0);
        fileTexts->setSpacing(1);
        auto *name = new ElidedLabel(m_project.source.name, fileRow);
        name->setPixelSize(11);
        name->setFontWeight(QFont::Medium);
        name->setTextColor(Theme::Text1);
        fileTexts->addWidget(name);
        auto *meta = Ui::richLabel(sourceFileMetaHtml(m_project.source),
                                   9, Theme::Text3, fileRow);
        m_sourceMetaLabel = meta;
        fileTexts->addWidget(meta);
        fileLayout->addLayout(fileTexts, 1);
        auto *remove = new LinkLabel(QStringLiteral("移除"), fileRow);
        remove->setPixelSize(9);
        remove->setTextColor(Theme::Text3);
        remove->setHoverColor(Theme::Danger);
        remove->setLeadingGlyph(Icons::Glyph::Trash, 9);
        remove->setUnderlineOnHover(false);
        connect(remove, &QAbstractButton::clicked, this, [this] {
            m_project.hasSource = false;
            m_project.source = StudioData::OralSource();
            rebuildSetup();
        });
        fileLayout->addWidget(remove, 0, Qt::AlignVCenter);
        sourceBody->addWidget(fileRow);
    }
    // 两张卡片在同一行会被拉成等高：把多余高度留在卡片底部，
    // 避免文件名、素材等内容被均匀撑开。
    sourceBody->addStretch(1);

    auto *materialsCard = new MediaDropCard(
        Icons::Glyph::Image, QStringLiteral("相关素材"),
        QStringLiteral("%1 个").arg(m_project.materials.size()),
        QStringLiteral("可将图片或视频拖到这里添加素材"),
        [this](const QStringList &paths) { addMaterials(paths); }, grid);
    auto *addButton = new Button(QStringLiteral("添加"), Button::Default, materialsCard);
    addButton->setCompact(true);
    addButton->setLeadingGlyph(Icons::Glyph::Plus);
    connect(addButton, &QAbstractButton::clicked, this,
            &OralPage::openMaterialFiles);
    materialsCard->headerLayout()->addWidget(addButton);
    materialsCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *materialsBody = materialsCard->bodyLayout();
    materialsBody->setSpacing(0);
    auto *materialsHint = Ui::label(
        QStringLiteral("添加与口播内容相关的产品、场景或细节素材，"
                       "AI 会按语义自动匹配并插入画面。"),
        10, QFont::Normal, Theme::Text3, materialsCard);
    materialsHint->setWordWrap(true);
    materialsBody->addWidget(materialsHint);
    materialsBody->addSpacing(10);

    auto *tileHost = new QWidget(materialsCard);
    auto *tileGrid = new QGridLayout(tileHost);
    tileGrid->setContentsMargins(0, 0, 0, 0);
    tileGrid->setHorizontalSpacing(6);
    tileGrid->setVerticalSpacing(6);
    int tileIndex = 0;
    for (const StudioData::OralMaterial &material : m_project.materials) {
        auto *tile = new MaterialTile(
            material, [this, id = material.id] { removeMaterial(id); },
            [this, material] { previewMaterial(material); }, tileHost);
        if (!material.filePath.isEmpty() && tile->visualWidget()) {
            m_previewWidgets[material.filePath].append(tile->visualWidget());
        }
        tileGrid->addWidget(tile, tileIndex / 6, tileIndex % 6);
        ++tileIndex;
    }
    auto *addTile = new AddMaterialTile(tileHost);
    connect(addTile, &QAbstractButton::clicked, this, &OralPage::openMaterialFiles);
    // 与设计稿一致：上传入口作为网格中的普通单元格，随行高自适应。
    tileGrid->addWidget(addTile, tileIndex / 6, tileIndex % 6);
    for (int column = 0; column < 6; ++column) {
        tileGrid->setColumnStretch(column, 1);
    }
    materialsBody->addWidget(tileHost);
    // 素材较少时把多余高度留白在底部，保持卡片内容紧凑。
    materialsBody->addStretch(1);

    gridLayout->addWidget(sourceCard, 142);
    gridLayout->addWidget(materialsCard, 100);
    layout->addWidget(grid);
    layout->addSpacing(12);
    layout->addWidget(new Studio::Note(
        QStringLiteral("上传后 AI 会先识别口播，再按语义选择素材位置。你可以在下一步选择包装模板、字幕样式与 B-Roll 密度，全部设置都可在生成前调整。")));
    // 多余高度留在页面底部，让两张卡片保持内容高度，不被拉满整页。
    layout->addStretch(1);
    return content;
}

QWidget *OralPage::buildSetupStepTwo()
{
    auto *content = new QWidget;
    auto *layout = new QHBoxLayout(content);
    layout->setContentsMargins(0, 0, 0, 0);
    layout->setSpacing(14);

    auto *left = new QWidget(content);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(14);

    const StudioData::OralTemplate tmpl =
        StudioData::oralTemplate(m_project.templateId);
    auto *templateCard = new Studio::SectionCard(
        Icons::Glyph::Sparkle, QStringLiteral("包装模板"),
        QStringLiteral("已选：%1").arg(tmpl.name), left);
    templateCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *templateGridHost = new QWidget(templateCard);
    auto *templateGrid = new QGridLayout(templateGridHost);
    templateGrid->setContentsMargins(0, 0, 0, 0);
    templateGrid->setHorizontalSpacing(8);
    templateGrid->setVerticalSpacing(8);
    const QVector<StudioData::OralTemplate> &templates = StudioData::oralTemplates();
    for (int index = 0; index < templates.size(); ++index) {
        templateGrid->addWidget(
            createTemplateCard(templates.at(index).id,
                               templates.at(index).id == m_project.templateId, true,
                               templateGridHost),
            index / 3, index % 3);
    }
    templateCard->bodyLayout()->addWidget(templateGridHost);
    leftLayout->addWidget(templateCard);

    auto *detailCard = new Studio::SectionCard(
        Icons::Glyph::Film, QStringLiteral("包装细节"), left);
    detailCard->setBodyMargins(QMargins(14, 14, 14, 14));
    auto *detailLayout = new QVBoxLayout;
    detailLayout->setContentsMargins(0, 0, 0, 0);
    detailLayout->setSpacing(0);
    auto *options = new QWidget(detailCard);
    auto *optionsLayout = new QHBoxLayout(options);
    optionsLayout->setContentsMargins(0, 0, 0, 0);
    optionsLayout->setSpacing(16);

    auto *subtitleColumn = new QVBoxLayout;
    subtitleColumn->setContentsMargins(0, 0, 0, 0);
    subtitleColumn->setSpacing(4);
    auto *subtitleHead = new QHBoxLayout;
    subtitleHead->setContentsMargins(0, 0, 0, 0);
    subtitleHead->addWidget(Ui::label(QStringLiteral("字幕样式"), 10, QFont::Medium,
                                       Theme::Text2, options));
    subtitleHead->addStretch(1);
    subtitleHead->addWidget(Ui::label(QStringLiteral("关键词自动高亮"), 9,
                                       QFont::Normal, Theme::Text3, options));
    subtitleColumn->addLayout(subtitleHead);
    auto *subtitle = new Studio::SegmentedControl(options);
    subtitle->setOptions({QStringLiteral("智能关键词"), QStringLiteral("大字强调"),
                          QStringLiteral("极简白字")});
    subtitle->setCurrentIndex(m_project.subtitleStyle
                                      == StudioData::OralSubtitleStyle::Keyword
                                  ? 0
                                  : (m_project.subtitleStyle
                                             == StudioData::OralSubtitleStyle::Big
                                         ? 1
                                         : 2));
    connect(subtitle, &Studio::SegmentedControl::currentChanged, this, [this](int index) {
        m_project.subtitleStyle = index == 0 ? StudioData::OralSubtitleStyle::Keyword
            : (index == 1 ? StudioData::OralSubtitleStyle::Big
                          : StudioData::OralSubtitleStyle::Minimal);
        rebuildSetup();
    });
    subtitleColumn->addWidget(subtitle);
    auto *subtitleHint = Ui::label(
        m_project.subtitleStyle == StudioData::OralSubtitleStyle::Keyword
            ? QStringLiteral("自动识别重点词并高亮，适合知识与测评内容。")
            : (m_project.subtitleStyle == StudioData::OralSubtitleStyle::Big
                   ? QStringLiteral("短句放大、节奏更快，适合带货与强观点内容。")
                   : QStringLiteral("统一的简洁白字，不抢画面。")),
        9, QFont::Normal, Theme::Text3, options);
    subtitleHint->setWordWrap(true);
    subtitleColumn->addWidget(subtitleHint);
    optionsLayout->addLayout(subtitleColumn, 1);

    auto *densityColumn = new QVBoxLayout;
    densityColumn->setContentsMargins(0, 0, 0, 0);
    densityColumn->setSpacing(4);
    auto *densityHead = new QHBoxLayout;
    densityHead->setContentsMargins(0, 0, 0, 0);
    densityHead->addWidget(Ui::label(QStringLiteral("B-Roll 密度"), 10,
                                       QFont::Medium, Theme::Text2, options));
    densityHead->addStretch(1);
    densityHead->addWidget(monoLabel(
        QStringLiteral("%1 处").arg(StudioData::oralBrollCount(m_project.density)), 9,
        Theme::Text3, options));
    densityColumn->addLayout(densityHead);
    auto *density = new Studio::SegmentedControl(options);
    density->setOptions({QStringLiteral("少"), QStringLiteral("适中"),
                         QStringLiteral("丰富")},
                        {QStringLiteral("8 处"), QStringLiteral("12 处"),
                         QStringLiteral("16 处")});
    density->setCompact(true);
    density->setCurrentIndex(m_project.density == StudioData::OralDensity::Low
                                 ? 0
                                 : (m_project.density
                                            == StudioData::OralDensity::High
                                        ? 2
                                        : 1));
    connect(density, &Studio::SegmentedControl::currentChanged, this, [this](int index) {
        m_project.density = index == 0 ? StudioData::OralDensity::Low
            : (index == 2 ? StudioData::OralDensity::High
                          : StudioData::OralDensity::Medium);
        rebuildSetup();
    });
    densityColumn->addWidget(density);
    auto *densityHint = Ui::label(
        m_project.density == StudioData::OralDensity::Low
            ? QStringLiteral("保留更多口播原画面，适合强信任内容。")
            : (m_project.density == StudioData::OralDensity::High
                   ? QStringLiteral("更多素材画面，节奏更紧凑，适合带货。")
                   : QStringLiteral("口播与素材穿插，兼顾信息密度与真实感。")),
        9, QFont::Normal, Theme::Text3, options);
    densityHint->setWordWrap(true);
    densityColumn->addWidget(densityHint);
    optionsLayout->addLayout(densityColumn, 1);

    detailLayout->addWidget(options);
    detailLayout->addSpacing(14);

    auto *toggles = new QWidget(detailCard);
    auto *toggleGrid = new QGridLayout(toggles);
    toggleGrid->setContentsMargins(0, 0, 0, 0);
    toggleGrid->setHorizontalSpacing(6);
    toggleGrid->setVerticalSpacing(6);
    auto makeSwitch = [this, toggles](const QString &title, const QString &description,
                                      bool checked, std::function<void(bool)> setter) {
        auto *row = new Studio::SwitchRow(title, description, checked, toggles);
        connect(row, &Studio::SwitchRow::toggled, this, [this, setter](bool value) {
            setter(value);
            rebuildSetup();
        });
        return row;
    };
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("动态转场"),
                              QStringLiteral("片段衔接更自然"), m_project.transitions,
                              [this](bool value) { m_project.transitions = value; }),
                          0, 0);
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("花字标题"),
                              QStringLiteral("重点句自动生成花字"), m_project.titleFx,
                              [this](bool value) { m_project.titleFx = value; }),
                          0, 1);
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("背景音乐"),
                              QStringLiteral("按情绪匹配，人声自动闪避"), m_project.bgm,
                              [this](bool value) { m_project.bgm = value; }),
                          1, 0);
    toggleGrid->addWidget(makeSwitch(
                              QStringLiteral("音效点缀"),
                              QStringLiteral("转场与重点处补充音效"), m_project.sfx,
                              [this](bool value) { m_project.sfx = value; }),
                          1, 1);
    detailLayout->addWidget(toggles);
    detailCard->bodyLayout()->addLayout(detailLayout);
    leftLayout->addWidget(detailCard);
    leftLayout->addStretch(1);

    auto *right = new QWidget(content);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(12);

    auto *previewCard = new Studio::SectionCard(
        Icons::Glyph::Play, QStringLiteral("实时预览"), tmpl.name, right);
    auto *ratioChip = new QLabel(m_project.ratio, previewCard);
    ratioChip->setFont(Theme::monoFont(9));
    ratioChip->setStyleSheet(
        QStringLiteral("color:%1;background:%2;border:1px solid %3;"
                       "border-radius:4px;padding:1px 5px;")
            .arg(Theme::Text3.name(), Theme::Surface2.name(), Theme::Border.name()));
    previewCard->headerLayout()->addWidget(ratioChip);
    previewCard->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *previewBody = previewCard->bodyLayout();
    previewBody->setSpacing(0);

    auto *stage = new DarkStage(previewCard);
    stage->setFixedHeight(392);
    auto *stageLayout = new QHBoxLayout(stage);
    stageLayout->setContentsMargins(12, 12, 12, 12);
    stageLayout->setAlignment(Qt::AlignCenter);
    const int previewHeight = m_project.ratio == QLatin1String("9:16") ? 392 : 212;
    const int previewWidth = m_project.ratio == QLatin1String("9:16")
        ? qRound(previewHeight * 9.0 / 16.0)
        : qRound(previewHeight * 16.0 / 9.0);
    auto *poster = new Studio::OralPosterWidget(stage);
    poster->setFixedSize(previewWidth, previewHeight);
    Studio::OralPosterSpec spec;
    spec.templateId = m_project.templateId;
    spec.headline = tmpl.headline;
    spec.caption = m_project.hasSource && !m_project.source.transcript.isEmpty()
        ? m_project.source.transcript.first().text
        : tmpl.caption;
    spec.keywords = m_project.hasSource ? m_project.source.topics.mid(0, 3)
                                        : QStringList();
    spec.materials = m_project.materials.mid(
        0, m_project.density == StudioData::OralDensity::Low ? 1 : 2);
    spec.ratio = m_project.ratio;
    spec.subtitleStyle = m_project.subtitleStyle;
    // 预览时间码使用真实口播时长；示例项目仍显示设计稿的 00:58。
    spec.timecode = m_project.hasSource && !m_project.source.duration.isEmpty()
        ? m_project.source.duration
        : QStringLiteral("00:58");
    poster->setSpec(spec);
    stageLayout->addWidget(poster, 0, Qt::AlignCenter);
    previewBody->addWidget(stage);
    previewBody->addSpacing(12);

    auto *ratioHead = new QHBoxLayout;
    ratioHead->setContentsMargins(0, 0, 0, 0);
    ratioHead->addWidget(Ui::label(QStringLiteral("成片画幅"), 10, QFont::Medium,
                                    Theme::Text2, previewCard));
    ratioHead->addStretch(1);
    ratioHead->addWidget(Ui::label(
        m_project.ratio == QLatin1String("9:16") ? QStringLiteral("适合抖音 / 视频号")
                                                  : QStringLiteral("适合 B 站 / 横屏"),
        9, QFont::Normal, Theme::Text3, previewCard));
    previewBody->addLayout(ratioHead);
    previewBody->addSpacing(5);
    auto *ratio = new Studio::SegmentedControl(previewCard);
    ratio->setOptions({QStringLiteral("9:16"), QStringLiteral("16:9")});
    ratio->setCurrentIndex(m_project.ratio == QLatin1String("9:16") ? 0 : 1);
    connect(ratio, &Studio::SegmentedControl::currentChanged, this,
            [this](int index) {
                applyRatio(index == 0 ? QStringLiteral("9:16")
                                      : QStringLiteral("16:9"));
            });
    previewBody->addWidget(ratio);
    previewBody->addSpacing(12);

    auto *outputHead = new QHBoxLayout;
    outputHead->setContentsMargins(0, 0, 0, 0);
    outputHead->addWidget(Ui::label(QStringLiteral("输出内容"), 10, QFont::Medium,
                                     Theme::Text2, previewCard));
    outputHead->addStretch(1);
    outputHead->addWidget(Ui::label(QStringLiteral("默认直出成片"), 9, QFont::Normal,
                                     Theme::Text3, previewCard));
    previewBody->addLayout(outputHead);
    previewBody->addSpacing(5);
    auto *videoOption = new OutputChoiceCard(
        QStringLiteral("直接生成成片"),
        QStringLiteral("MP4 · 1080p，生成后自动下载"), QStringLiteral("默认"),
        Icons::Glyph::Film, !m_project.outputBoth, previewCard);
    connect(videoOption, &QAbstractButton::clicked, this, [this] {
        if (!m_project.outputBoth) {
            return;
        }
        applyOutput(false);
    });
    previewBody->addWidget(videoOption);
    previewBody->addSpacing(6);
    auto *bothOption = new OutputChoiceCard(
        QStringLiteral("成片 + 剪映草稿"),
        QStringLiteral("成片自动下载，并生成可编辑草稿"), QString(),
        Icons::Glyph::Scissors, m_project.outputBoth, previewCard);
    connect(bothOption, &QAbstractButton::clicked, this, [this] {
        if (m_project.outputBoth) {
            return;
        }
        applyOutput(true);
    });
    previewBody->addWidget(bothOption);

    auto *note = new Studio::Note(
        QStringLiteral(
            "开始后展示「理解口播 → 匹配素材 → 智能包装 → 渲染」全过程，可后台运行，完成后通知你。"),
        Studio::Note::Primary);
    rightLayout->addWidget(previewCard);
    rightLayout->addWidget(note);
    rightLayout->addStretch(1);

    layout->addWidget(left, 147);
    layout->addWidget(right, 100);
    return content;
}

QWidget *OralPage::buildSetupFooter()
{
    auto *footer = new QWidget(m_setupScreen);
    footer->setFixedHeight(52);
    footer->setObjectName(QStringLiteral("oralFooter"));
    footer->setStyleSheet(
        QStringLiteral("#oralFooter { background:%1; }").arg(Theme::Surface1.name()));
    auto *layout = new QHBoxLayout(footer);
    layout->setContentsMargins(22, 0, 22, 0);
    layout->setSpacing(12);

    const bool ready = m_project.hasSource && !m_project.materials.isEmpty();
    auto *texts = new QVBoxLayout;
    texts->setContentsMargins(0, 0, 0, 0);
    texts->setSpacing(1);
    auto *summary = new ElidedLabel(
        m_project.step == 1
            ? (ready ? QStringLiteral("素材已就绪，AI 将先识别口播，再按语义匹配素材")
                     : QStringLiteral("至少需要 1 段口播视频和 1 个相关素材"))
            : QStringLiteral("预计成片 %1 · %2 处 B-Roll · %3 · %4")
                  .arg(m_project.hasSource && !m_project.source.duration.isEmpty()
                           ? m_project.source.duration
                           : QStringLiteral("00:58"),
                       QString::number(
                           StudioData::oralBrollCount(m_project.density)),
                       m_project.ratio,
                       m_project.outputBoth ? QStringLiteral("成片 + 剪映草稿")
                                            : QStringLiteral("成片 MP4")),
        footer);
    summary->setPixelSize(10);
    summary->setTextColor(Theme::Text2);
    texts->addWidget(summary);
    auto *hint = Ui::label(
        m_project.step == 1
            ? QStringLiteral("设置会随项目自动保存，可随时返回修改")
            : QStringLiteral("生成约 2–4 分钟；完成后自动下载，可后台运行"),
        9, QFont::Normal, Theme::Text3, footer);
    texts->addWidget(hint);
    layout->addLayout(texts, 1);

    if (m_project.step == 2) {
        auto *previous = new Button(QStringLiteral("上一步"), Button::Default, footer);
        connect(previous, &QAbstractButton::clicked, this, [this] {
            m_project.step = 1;
            rebuildSetup();
        });
        layout->addWidget(previous);
        auto *start = new Button(QStringLiteral("一键包装"), Button::Primary, footer);
        start->setLeadingGlyph(Icons::Glyph::Sparkle);
        start->setEnabled(ready);
        start->setToolTip(ready ? QStringLiteral("开始 AI 自动包装")
                                : QStringLiteral("请先补齐口播与素材"));
        connect(start, &QAbstractButton::clicked, this, &OralPage::startGeneration);
        layout->addWidget(start);
    } else {
        auto *next = new Button(QStringLiteral("下一步：包装设置"), Button::Primary,
                                footer);
        next->setEnabled(ready);
        next->setToolTip(ready ? QStringLiteral("选择包装模板与输出设置")
                               : QStringLiteral("请先添加口播视频和相关素材"));
        connect(next, &QAbstractButton::clicked, this, [this] {
            m_project.step = 2;
            rebuildSetup();
        });
        layout->addWidget(next);
    }
    return footer;
}

void OralPage::openVideoFile()
{
    const QString path = QFileDialog::getOpenFileName(
        window(), QStringLiteral("选择口播视频"), QString(),
        QStringLiteral("视频文件 (*.mp4 *.mov *.m4v *.mkv *.avi *.webm)"));
    if (!path.isEmpty()) {
        importVideoFile(path);
    }
}

void OralPage::importVideoFromPaths(const QStringList &paths)
{
    for (const QString &path : paths) {
        const QFileInfo info(path);
        if (info.isFile() && isVideoMaterialFile(info)) {
            importVideoFile(info.absoluteFilePath());
            return;
        }
    }
    emit toastRequested(QStringLiteral("请拖入口播视频文件：MP4 / MOV / MKV 等"));
}

void OralPage::importVideoFile(const QString &path)
{
    const QFileInfo info(path);
    if (!info.isFile() || !isVideoMaterialFile(info)) {
        emit toastRequested(QStringLiteral("仅支持视频文件：MP4 / MOV / MKV 等"));
        return;
    }

    const QString absolutePath = info.absoluteFilePath();
    m_project.source = StudioData::demoOralSource();
    m_project.source.name = info.fileName();
    m_project.source.size = MediaUtils::formatByteSize(info.size());
    // 真实时长与分辨率由 ffprobe 异步补齐，先避免展示示例数据的假值。
    m_project.source.duration = QStringLiteral("—");
    m_project.source.resolution = QStringLiteral("—");
    m_project.source.filePath = absolutePath;
    m_project.source.preview = QImage();
    m_project.source.sample = false;
    m_project.hasSource = true;
    // 重新导入同一路径时允许重试曾失败的截帧。
    m_failedPreviewPaths.remove(absolutePath);
    rebuildSetup();
    emit toastRequested(QStringLiteral("已导入口播视频，AI 正在识别画面与口播内容"));
    probeSourceMetadata(absolutePath);
}

void OralPage::probeSourceMetadata(const QString &path)
{
    const QString ffprobe = FfmpegLocator::ffprobePath();
    if (ffprobe.isEmpty()) {
        return;
    }
    auto *process = new QProcess(this);
    connect(process, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished),
            this,
            [this, process, path](int exitCode, QProcess::ExitStatus status) {
                if (exitCode != 0 || status != QProcess::NormalExit) {
                    process->deleteLater();
                    return;
                }
                const QByteArray payload = process->readAllStandardOutput();
                process->deleteLater();
                if (!m_project.hasSource || m_project.source.filePath != path) {
                    return;
                }

                const QJsonObject root =
                    QJsonDocument::fromJson(payload).object();
                int width = 0;
                int height = 0;
                const QJsonArray streams =
                    root.value(QStringLiteral("streams")).toArray();
                if (!streams.isEmpty()) {
                    const QJsonObject stream = streams.first().toObject();
                    width = stream.value(QStringLiteral("width")).toInt();
                    height = stream.value(QStringLiteral("height")).toInt();
                }
                bool durationOk = false;
                double seconds = 0.0;
                const QJsonValue durationValue =
                    root.value(QStringLiteral("format"))
                        .toObject()
                        .value(QStringLiteral("duration"));
                if (durationValue.isString()) {
                    seconds = durationValue.toString().toDouble(&durationOk);
                } else if (durationValue.isDouble()) {
                    seconds = durationValue.toDouble();
                    durationOk = true;
                }

                if (width > 0 && height > 0) {
                    m_project.source.resolution =
                        QStringLiteral("%1×%2").arg(width).arg(height);
                }
                if (durationOk && seconds > 0.0) {
                    m_project.source.duration =
                        MediaUtils::formatDuration(qRound64(seconds * 1000.0));
                }
                if (m_sourceMetaLabel) {
                    m_sourceMetaLabel->setText(sourceFileMetaHtml(m_project.source));
                }
                if (m_sourcePreviewUpdater) {
                    m_sourcePreviewUpdater(m_project.source);
                }
            });
    connect(process, &QProcess::errorOccurred, this,
            [process](QProcess::ProcessError error) {
                if (error == QProcess::FailedToStart) {
                    process->deleteLater();
                }
            });
    process->start(ffprobe,
                   {QStringLiteral("-v"), QStringLiteral("error"),
                    QStringLiteral("-select_streams"), QStringLiteral("v:0"),
                    QStringLiteral("-show_entries"),
                    QStringLiteral("stream=width,height:format=duration"),
                    QStringLiteral("-of"), QStringLiteral("json"), path});
}

void OralPage::openMaterialFiles()
{
    const QStringList paths = QFileDialog::getOpenFileNames(
        window(), QStringLiteral("添加相关素材"), QString(), materialFileFilter());
    if (!paths.isEmpty()) {
        addMaterials(paths);
    }
}

void OralPage::addMaterials(const QStringList &paths)
{
    constexpr int kMaterialLimit = 24;
    int added = 0;
    int skipped = 0;
    bool limitReached = false;
    for (const QString &path : paths) {
        if (m_project.materials.size() >= kMaterialLimit) {
            limitReached = true;
            break;
        }
        const QFileInfo info(path);
        if (!isSupportedMaterialFile(info)) {
            ++skipped;
            continue;
        }
        m_project.materials.append(StudioData::oralMaterialFromFile(
            info.absoluteFilePath(), info.size(), isVideoMaterialFile(info),
            m_project.materials.size()));
        ++added;
    }

    if (added == 0) {
        if (skipped > 0) {
            emit toastRequested(QStringLiteral(
                "仅支持图片或视频素材：PNG / JPG / WEBP / MP4 / MOV 等"));
        } else if (limitReached) {
            emit toastRequested(
                QStringLiteral("素材最多添加 24 个，请先移除部分素材"));
        }
        return;
    }

    rebuildSetup();
    QString message =
        QStringLiteral("已添加 %1 个素材，AI 正在建立匹配").arg(added);
    if (skipped > 0) {
        message += QStringLiteral("；已跳过 %1 个不支持的文件").arg(skipped);
    }
    if (limitReached) {
        message += QStringLiteral("；最多可添加 24 个素材");
    }
    emit toastRequested(message);
}

void OralPage::removeMaterial(const QString &id)
{
    for (int index = 0; index < m_project.materials.size(); ++index) {
        if (m_project.materials.at(index).id == id) {
            m_project.materials.removeAt(index);
            break;
        }
    }
    rebuildSetup();
}

void OralPage::previewMaterial(const StudioData::OralMaterial &material)
{
    const QFileInfo info(material.filePath);
    if (material.filePath.isEmpty() || !info.isFile()) {
        emit toastRequested(QStringLiteral("素材文件不存在，无法预览"));
        return;
    }
    emit previewRequested(material.name, info.absoluteFilePath(),
                          isVideoMaterialFile(info));
}

void OralPage::ensureMaterialPreviews()
{
    // 口播原片同样使用首帧缩略图；与素材共用截帧队列和磁盘缓存。
    if (m_project.hasSource && !m_project.source.filePath.isEmpty()
        && m_project.source.preview.isNull()) {
        const QFileInfo sourceInfo(m_project.source.filePath);
        if (sourceInfo.isFile()) {
            const QString path = sourceInfo.absoluteFilePath();
            if (!m_failedPreviewPaths.contains(path)) {
                const QImage cached(FfmpegUtils::thumbnailCachePath(path));
                if (!cached.isNull()) {
                    m_project.source.preview = cached;
                } else {
                    requestVideoThumbnail(path);
                }
            }
        }
    }

    for (StudioData::OralMaterial &material : m_project.materials) {
        if (material.filePath.isEmpty() || !material.preview.isNull()) {
            continue;
        }
        const QFileInfo info(material.filePath);
        if (!info.isFile()) {
            continue;
        }
        const QString path = info.absoluteFilePath();
        if (m_failedPreviewPaths.contains(path)) {
            continue;
        }
        if (isImageMaterialFile(info)) {
            const QImage preview = loadMaterialPreview(path);
            if (preview.isNull()) {
                m_failedPreviewPaths.insert(path);
            } else {
                material.preview = preview;
            }
        } else if (isVideoMaterialFile(info)) {
            const QImage cached(FfmpegUtils::thumbnailCachePath(path));
            if (!cached.isNull()) {
                material.preview = cached;
            } else {
                requestVideoThumbnail(path);
            }
        }
    }
}

void OralPage::requestVideoThumbnail(const QString &path)
{
    requestPreviewFrame(path, true);
}

void OralPage::requestPosterFrame(const QString &path)
{
    requestPreviewFrame(path, false);
}

void OralPage::requestPreviewFrame(const QString &path, bool materialFrame)
{
    const PreviewFrameKind kind =
        materialFrame ? PreviewFrameKind::Material : PreviewFrameKind::Poster;
    if ((m_thumbnailInFlight.path == path && m_thumbnailInFlight.kind == kind)
        || m_failedPreviewPaths.contains(path)) {
        return;
    }
    for (const PreviewFrameJob &job : m_thumbnailQueue) {
        if (job.path == path && job.kind == kind) {
            return;
        }
    }
    m_thumbnailQueue.append({path, kind});
    startNextThumbnailJob();
}

void OralPage::startNextThumbnailJob()
{
    if (m_thumbnailProcess || m_thumbnailQueue.isEmpty()) {
        return;
    }
    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()) {
        m_thumbnailQueue.clear();
        return;
    }

    const PreviewFrameJob job = m_thumbnailQueue.takeFirst();
    const QString path = job.path;
    const bool posterFrame = job.kind == PreviewFrameKind::Poster;
    const QString cachePath = posterFrame
        ? FfmpegUtils::posterFrameCachePath(path)
        : FfmpegUtils::thumbnailCachePath(path);
    if (!QDir().mkpath(QFileInfo(cachePath).absolutePath())) {
        m_failedPreviewPaths.insert(path);
        startNextThumbnailJob();
        return;
    }

    auto *process = new QProcess(this);
    m_thumbnailProcess = process;
    m_thumbnailInFlight = job;
    connect(process, QOverload<int, QProcess::ExitStatus>::of(&QProcess::finished),
            this,
            [this, process, path, cachePath, posterFrame](
                int exitCode, QProcess::ExitStatus status) {
                if (m_thumbnailProcess != process) {
                    return;
                }
                process->deleteLater();
                m_thumbnailProcess = nullptr;
                m_thumbnailInFlight = PreviewFrameJob();
                QImage image;
                if (exitCode == 0 && status == QProcess::NormalExit) {
                    image = QImage(cachePath);
                }
                if (image.isNull()) {
                    m_failedPreviewPaths.insert(path);
                } else if (posterFrame) {
                    applyPosterFrame(path, image);
                } else {
                    applyMaterialThumbnail(path, image);
                }
                startNextThumbnailJob();
            });
    connect(process, &QProcess::errorOccurred, this,
            [this, process, path](QProcess::ProcessError error) {
                if (error != QProcess::FailedToStart || m_thumbnailProcess != process) {
                    return;
                }
                process->deleteLater();
                m_thumbnailProcess = nullptr;
                m_thumbnailInFlight = PreviewFrameJob();
                m_failedPreviewPaths.insert(path);
                startNextThumbnailJob();
            });
    // 素材缩略图取 0.5 秒帧避开片头黑场；成片封面固定取 0 秒首帧。
    process->start(ffmpeg,
                   {QStringLiteral("-y"), QStringLiteral("-nostdin"),
                    QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
                    QStringLiteral("error"), QStringLiteral("-ss"),
                    posterFrame ? QStringLiteral("0.000")
                                : QStringLiteral("0.500"),
                    QStringLiteral("-i"), path,
                    QStringLiteral("-frames:v"), QStringLiteral("1"),
                    QStringLiteral("-vf"), QStringLiteral("scale=640:-2"),
                    cachePath});
}

void OralPage::applyMaterialThumbnail(const QString &path, const QImage &image)
{
    // 首帧同时回填给口播原片（若当前原片就是刚截帧的视频）。
    if (m_project.hasSource && m_project.source.preview.isNull()
        && m_project.source.filePath == path) {
        m_project.source.preview = image;
        if (m_sourcePreviewUpdater) {
            m_sourcePreviewUpdater(m_project.source);
        }
    }

    bool changed = false;
    for (StudioData::OralMaterial &material : m_project.materials) {
        if (material.preview.isNull() && material.filePath == path) {
            material.preview = image;
            changed = true;
        }
    }
    if (!changed) {
        return;
    }
    const QVector<Studio::MaterialVisualWidget *> widgets =
        m_previewWidgets.value(path);
    if (widgets.isEmpty()) {
        return;
    }
    // 同一路径可能被多个素材引用，用更新后的素材刷新对应的缩略图控件。
    for (const StudioData::OralMaterial &material : m_project.materials) {
        if (material.filePath != path) {
            continue;
        }
        for (Studio::MaterialVisualWidget *widget : widgets) {
            widget->setMaterial(material);
        }
        break;
    }
}

void OralPage::applyPosterFrame(const QString &path, const QImage &image)
{
    // 首页可能已重建，登记表只保留当前仍存在的卡片海报。
    const QVector<Studio::OralPosterWidget *> posters =
        m_recentPosterWidgets.value(path);
    for (Studio::OralPosterWidget *poster : posters) {
        poster->spec().frameImage = image;
        poster->update();
    }
}

void OralPage::clearSource()
{
    // 只清空口播视频，已添加的相关素材保留。
    m_project.hasSource = false;
    m_project.source = StudioData::OralSource();
    rebuildSetup();
}

void OralPage::chooseTemplate(const QString &templateId)
{
    // 当前版本只实现「清透讲解」模板；其它模板保持展示，但不进入流水线。
    if (templateId != QLatin1String("clean")) {
        emit toastRequested(QStringLiteral(
            "当前版本仅开放「清透讲解」模板，其它模板即将上线"));
        return;
    }
    m_project.templateId = templateId;
    if (m_screen == Screen::Hub) {
        rebuildHub();
    } else {
        rebuildSetup();
    }
    emit toastRequested(QStringLiteral("已选择「%1」模板，开始包装时自动套用")
                            .arg(StudioData::oralTemplateName(templateId)));
}

void OralPage::applyRatio(const QString &ratio)
{
    if (m_project.ratio == ratio) {
        return;
    }
    m_project.ratio = ratio;
    rebuildSetup();
}

void OralPage::applyOutput(bool both)
{
    if (m_project.outputBoth == both) {
        return;
    }
    m_project.outputBoth = both;
    rebuildSetup();
}

/* ------------------------------------------------------------------ */
/* 生成过程                                                             */
/* ------------------------------------------------------------------ */

void OralPage::startGeneration()
{
    if (!m_project.hasSource || m_project.materials.isEmpty()) {
        emit toastRequested(QStringLiteral("请先添加口播视频和相关素材"));
        return;
    }
    // 当前版本只开放「清透讲解」模板；其它模板保持可见但不进入流水线。
    m_project.templateId = QStringLiteral("clean");
    if (m_project.source.filePath.trimmed().isEmpty()
        || !QFileInfo::exists(m_project.source.filePath)) {
        emit toastRequested(QStringLiteral("口播视频文件不存在，请重新上传"));
        return;
    }
    // 剪映小助手单文件上传上限 500MB；提前拦截，避免跑到合成阶段才失败。
    if (QFileInfo(m_project.source.filePath).size() > 500LL * 1024 * 1024) {
        emit toastRequested(QStringLiteral(
            "口播视频超过 500MB，请压缩或缩短到 10 分钟以内后再试"));
        return;
    }
    QVector<StudioData::OralMaterial> materials;
    for (const StudioData::OralMaterial &material : m_project.materials) {
        if (!material.filePath.trimmed().isEmpty()
            && QFileInfo::exists(material.filePath)) {
            materials.append(material);
        }
    }
    if (materials.isEmpty()) {
        emit toastRequested(QStringLiteral("相关素材文件不存在，请重新添加"));
        return;
    }

    const AppConfig config = m_config ? m_config->config() : AppConfig();
    const QString apiKey = config.service.apiKey.trimmed();
    if (apiKey.isEmpty()) {
        emit toastRequested(QStringLiteral("请先在设置中填写 API Key，再开始包装"));
        emit navigateRequested(QStringLiteral("settings"));
        return;
    }

    if (m_playTimer) {
        m_playTimer->stop();
    }
    m_playing = false;
    m_playProgress = 0;
    m_draftState = 0;
    m_draftProgress = 0;
    resetResultPlayer();

    // 真实工作目录与 UI 结果页展示的路径保持一致。
    const QString movies = QStandardPaths::writableLocation(
        QStandardPaths::MoviesLocation);
    const QString outputDir =
        QDir(movies).filePath(QStringLiteral("LiveMix/口播成片"));
    const QString draftRoot =
        QDir(movies).filePath(QStringLiteral("LiveMix/CapDraft"));
    QString cacheDir = QStandardPaths::writableLocation(
        QStandardPaths::CacheLocation);
    if (cacheDir.isEmpty()) {
        cacheDir = QDir::tempPath();
    }
    cacheDir = QDir(cacheDir).filePath(QStringLiteral("oral"));

    Oral::PipelineRequest request;
    request.runId = m_runId + 1;
    request.apiKey = apiKey;
    request.gatewayBaseUrl = config.service.apiUrl;
    request.project = m_project;
    request.project.materials = materials;
    request.cacheDir = cacheDir;
    request.outputDir = outputDir;
    request.draftRoot = draftRoot;

    destroyPipeline();
    buildProcessingScreen();
    m_runId = request.runId;
    m_runStartedAt = QDateTime::currentMSecsSinceEpoch();
    m_run = StudioData::OralRun();
    m_run.runId = m_runId;
    m_run.stage = StudioData::OralStage::Analyze;
    const QStringList labels = {
        QStringLiteral("理解口播"), QStringLiteral("匹配素材"),
        QStringLiteral("智能包装"), QStringLiteral("渲染成片")};
    const QVector<StudioData::OralStage> keys = {
        StudioData::OralStage::Analyze, StudioData::OralStage::Match,
        StudioData::OralStage::Compose, StudioData::OralStage::Render};
    for (int index = 0; index < keys.size(); ++index) {
        StudioData::OralStageState stage;
        stage.key = keys.at(index);
        stage.label = labels.at(index);
        m_run.stages.append(stage);
    }
    m_hasRun = true;
    m_pipelineRunning = true;
    m_logCount = -1;
    m_lastKeywordCount = -1;
    m_lastMatchedCount = -1;
    updateProcessing();
    showProcessing();
    m_runTimer->start();
    emit taskStatusChanged(
        QStringLiteral("智能口播中 0% · 00:00"), true);

    auto *service = new Oral::PipelineService(request);
    auto *thread = new QThread;
    service->moveToThread(thread);
    connect(thread, &QThread::started, service, &Oral::PipelineService::run);
    connect(service, &Oral::PipelineService::runUpdated, this,
            &OralPage::onPipelineRunUpdated);
    connect(service, &Oral::PipelineService::finished, this,
            &OralPage::onPipelineFinished);
    connect(thread, &QThread::finished, service, &QObject::deleteLater);
    connect(thread, &QThread::finished, thread, &QObject::deleteLater);
    m_pipelineService = service;
    m_pipelineThread = thread;
    thread->start();
}

void OralPage::buildProcessingScreen()
{
    ensureMaterialPreviews();

    auto *root = qobject_cast<QVBoxLayout *>(m_processingScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_processingScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    Studio::clearLayout(root);
    m_stageCards.clear();
    m_processingPoster = nullptr;
    m_processingProjectLabel = nullptr;
    m_keywordStatus = nullptr;
    m_keywordLayout = nullptr;
    m_matchedLayout = nullptr;
    m_matchedCount = nullptr;
    m_logCount = -1;

    auto *top = new QWidget(m_processingScreen);
    auto *topLayout = new QVBoxLayout(top);
    topLayout->setContentsMargins(22, 14, 22, 12);
    topLayout->setSpacing(0);

    auto *titleRow = new QWidget(top);
    auto *titleLayout = new QHBoxLayout(titleRow);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(8);
    auto *titleTexts = new QVBoxLayout;
    titleTexts->setContentsMargins(0, 0, 0, 0);
    titleTexts->setSpacing(2);
    auto *titleLine = new QHBoxLayout;
    titleLine->setContentsMargins(0, 0, 0, 0);
    titleLine->setSpacing(8);
    titleLine->addWidget(Ui::label(QStringLiteral("AI 正在包装你的口播"), 16,
                                     QFont::DemiBold, Theme::Text1, top));
    titleLine->addWidget(new Chip(QStringLiteral("智能口播中"), Chip::Run, top));
    auto *ratioLabel = monoLabel(
        QStringLiteral("%1 · %2")
            .arg(m_project.ratio, m_project.outputBoth
                                      ? QStringLiteral("成片 + 剪映草稿")
                                      : QStringLiteral("成片 MP4")),
        10, Theme::Text3, top);
    titleLine->addWidget(ratioLabel);
    titleLine->addStretch(1);
    titleTexts->addLayout(titleLine);
    auto *metaLine = new QHBoxLayout;
    metaLine->setContentsMargins(0, 0, 0, 0);
    metaLine->setSpacing(8);
    auto *projectLabel = Ui::label(
        QStringLiteral("正在理解口播内容"), 10, QFont::Normal, Theme::Text2, top);
    m_processingProjectLabel = projectLabel;
    metaLine->addWidget(projectLabel);
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    metaLine->addWidget(Ui::label(
        QStringLiteral("模板：%1").arg(StudioData::oralTemplateName(m_project.templateId)),
        10, QFont::Normal, Theme::Text3, top));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    m_processingMeta = Ui::label(QStringLiteral("已用时 00:00"), 10, QFont::Normal,
                                 Theme::Text3, top);
    metaLine->addWidget(m_processingMeta);
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, top));
    m_etaLabel = Ui::label(QStringLiteral("预计还需 24 秒"), 10, QFont::Normal,
                           Theme::Text3, top);
    metaLine->addWidget(m_etaLabel);
    metaLine->addStretch(1);
    titleTexts->addLayout(metaLine);
    titleLayout->addLayout(titleTexts, 1);

    auto *progressHost = new QWidget(top);
    auto *progressLayout = new QHBoxLayout(progressHost);
    progressLayout->setContentsMargins(0, 0, 0, 0);
    progressLayout->setSpacing(12);
    auto *percentBox = new QVBoxLayout;
    percentBox->setContentsMargins(0, 0, 0, 0);
    percentBox->setSpacing(2);
    m_progressLabel = Ui::richLabel(QString(), 23, Theme::Primary, progressHost);
    m_progressLabel->setAlignment(Qt::AlignRight);
    percentBox->addWidget(m_progressLabel);
    auto *percentLabel = Ui::label(QStringLiteral("总进度"), 9, QFont::Normal,
                                    Theme::Text3, progressHost);
    percentLabel->setAlignment(Qt::AlignRight);
    percentBox->addWidget(percentLabel);
    progressLayout->addLayout(percentBox);
    auto *runInBackground = new Button(QStringLiteral("后台运行"), Button::Default,
                                       progressHost);
    connect(runInBackground, &QAbstractButton::clicked, this, [this] {
        emit toastRequested(QStringLiteral("已转后台运行，完成后自动下载并通知你"));
        emit navigateRequested(QStringLiteral("dash"));
    });
    progressLayout->addWidget(runInBackground, 0, Qt::AlignVCenter);
    titleLayout->addWidget(progressHost, 0, Qt::AlignTop);
    topLayout->addWidget(titleRow);
    topLayout->addSpacing(12);

    auto *stages = new QWidget(top);
    auto *stagesLayout = new QHBoxLayout(stages);
    stagesLayout->setContentsMargins(0, 0, 0, 0);
    stagesLayout->setSpacing(8);
    for (int index = 0; index < 4; ++index) {
        auto *card = new Studio::StageSummaryCard(stages);
        m_stageCards.append(card);
        stagesLayout->addWidget(card, 1);
    }
    topLayout->addWidget(stages);
    root->addWidget(top);
    root->addWidget(Ui::separator(false, m_processingScreen));

    auto *body = new QWidget(m_processingScreen);
    auto *bodyLayout = new QHBoxLayout(body);
    bodyLayout->setContentsMargins(22, 12, 22, 12);
    bodyLayout->setSpacing(14);

    auto *left = new QWidget(body);
    left->setFixedWidth(360);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(8);

    auto *stageHost = new DarkStage(left);
    auto *stageLayout = new QHBoxLayout(stageHost);
    stageLayout->setContentsMargins(12, 12, 12, 12);
    stageLayout->setAlignment(Qt::AlignCenter);
    const int previewHeight = m_project.ratio == QLatin1String("9:16") ? 388 : 212;
    const int previewWidth = m_project.ratio == QLatin1String("9:16")
        ? qRound(previewHeight * 9.0 / 16.0)
        : qRound(previewHeight * 16.0 / 9.0);
    m_processingPoster = new Studio::OralPosterWidget(stageHost);
    m_processingPoster->setFixedSize(previewWidth, previewHeight);
    Studio::OralPosterSpec spec;
    spec.templateId = m_project.templateId;
    spec.headline = StudioData::oralTemplate(m_project.templateId).headline;
    spec.caption = QStringLiteral("正在理解口播内容…");
    spec.ratio = m_project.ratio;
    spec.timecode = QStringLiteral("00:00");
    spec.progress = 0;
    spec.subtitleStyle = m_project.subtitleStyle;
    m_processingPoster->setSpec(spec);
    stageLayout->addWidget(m_processingPoster, 0, Qt::AlignCenter);
    stageHost->setMinimumHeight(previewHeight + 24);
    leftLayout->addWidget(stageHost, 1);

    auto *infoRow = new QWidget(left);
    auto *infoLayout = new QHBoxLayout(infoRow);
    infoLayout->setContentsMargins(0, 0, 0, 0);
    infoLayout->setSpacing(8);
    auto *keywordContent = new QWidget;
    m_keywordLayout = new QHBoxLayout(keywordContent);
    m_keywordLayout->setContentsMargins(0, 0, 0, 0);
    m_keywordLayout->setSpacing(4);
    m_keywordStatus = Ui::label(QStringLiteral("正在读取口播内容…"), 8, QFont::Normal,
                                 Theme::Text3, keywordContent);
    m_keywordLayout->addWidget(m_keywordStatus);
    m_keywordLayout->addStretch(1);
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("AI 理解"),
                                        Icons::Glyph::Sparkle, keywordContent,
                                        infoRow),
                          1);

    auto *matchedContent = new QWidget;
    auto *matchedBox = new QVBoxLayout(matchedContent);
    matchedBox->setContentsMargins(0, 0, 0, 0);
    matchedBox->setSpacing(3);
    auto *matchedTop = new QHBoxLayout;
    matchedTop->setContentsMargins(0, 0, 0, 0);
    matchedTop->addStretch(1);
    m_matchedCount = monoLabel(QStringLiteral("0/0"), 8, Theme::Text2, matchedContent);
    matchedTop->addWidget(m_matchedCount);
    matchedBox->addLayout(matchedTop);
    auto *matchedThumbsHost = new QWidget(matchedContent);
    m_matchedLayout = new QHBoxLayout(matchedThumbsHost);
    m_matchedLayout->setContentsMargins(0, 0, 0, 0);
    m_matchedLayout->setSpacing(4);
    auto *waiting = Ui::label(QStringLiteral("等待口播理解完成"), 8, QFont::Normal,
                              Theme::Text3, matchedThumbsHost);
    m_matchedLayout->addWidget(waiting);
    m_matchedLayout->addStretch(1);
    matchedBox->addWidget(matchedThumbsHost);
    infoLayout->addWidget(makeInfoPanel(QStringLiteral("素材匹配"),
                                        Icons::Glyph::Image, matchedContent,
                                        infoRow),
                          1);
    leftLayout->addWidget(infoRow);

    auto *sourceRow = new QWidget(left);
    sourceRow->setObjectName(QStringLiteral("oralProcessingSource"));
    sourceRow->setStyleSheet(
        QStringLiteral("#oralProcessingSource { background:%1; border:1px solid %2;"
                       " border-radius:7px; }")
            .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *sourceLayout = new QHBoxLayout(sourceRow);
    sourceLayout->setContentsMargins(10, 7, 10, 7);
    sourceLayout->setSpacing(8);
    auto *wave = new Studio::Waveform(sourceRow);
    wave->setBars(18);
    wave->setColor(StudioData::oralTemplate(m_project.templateId).accent);
    wave->setAnimated(true);
    wave->setFixedWidth(64);
    sourceLayout->addWidget(wave, 0, Qt::AlignVCenter);
    auto *sourceLabel = new ElidedLabel(
        QStringLiteral("%1 · %2")
            .arg(m_project.source.name, m_project.source.duration),
        sourceRow);
    sourceLabel->setPixelSize(9);
    sourceLabel->setTextColor(Theme::Text3);
    sourceLayout->addWidget(sourceLabel, 1);
    m_processingSourceMeta = monoLabel(QString(), 8, Theme::Text3, sourceRow);
    sourceLayout->addWidget(m_processingSourceMeta, 0);
    leftLayout->addWidget(sourceRow);
    bodyLayout->addWidget(left);

    auto *logPanel = new QWidget(body);
    logPanel->setObjectName(QStringLiteral("oralLogPanel"));
    logPanel->setStyleSheet(
        QStringLiteral("#oralLogPanel { background:%1; border:1px solid %2;"
                       " border-radius:10px; }")
            .arg(Theme::Surface1.name(), Theme::Border.name()));
    auto *logLayout = new QVBoxLayout(logPanel);
    logLayout->setContentsMargins(0, 0, 0, 0);
    logLayout->setSpacing(0);

    auto *logHeader = new QWidget(logPanel);
    auto *logHeaderLayout = new QHBoxLayout(logHeader);
    logHeaderLayout->setContentsMargins(14, 10, 14, 10);
    logHeaderLayout->setSpacing(8);
    logHeaderLayout->addWidget(makeIconCircle(Icons::Glyph::Sparkle, 20,
                                               Theme::Primary,
                                               Theme::alpha(Theme::Primary, 0.14),
                                               Theme::alpha(Theme::Primary, 0.35),
                                               logHeader),
                               0, Qt::AlignVCenter);
    auto *logTitles = new QVBoxLayout;
    logTitles->setContentsMargins(0, 0, 0, 0);
    logTitles->setSpacing(1);
    m_logStageTitle = Ui::label(QStringLiteral("实时过程 · 理解口播"), 11,
                                QFont::Medium, Theme::Text1, logHeader);
    m_logStageDetail = new ElidedLabel(QStringLiteral("正在准备…"), logHeader);
    m_logStageDetail->setPixelSize(9);
    m_logStageDetail->setTextColor(Theme::Text3);
    logTitles->addWidget(m_logStageTitle);
    logTitles->addWidget(m_logStageDetail);
    logHeaderLayout->addLayout(logTitles, 1);
    auto *logCounts = new QHBoxLayout;
    logCounts->setContentsMargins(0, 0, 0, 0);
    logCounts->setSpacing(10);
    m_logElapsedLabel = monoLabel(QStringLiteral("00:00"), 9, Theme::Text3,
                                  logHeader);
    logCounts->addWidget(m_logElapsedLabel);
    m_logStepsLabel = monoLabel(QStringLiteral("0/4 步"), 9, Theme::Text3,
                                logHeader);
    logCounts->addWidget(m_logStepsLabel);
    logHeaderLayout->addLayout(logCounts);
    logLayout->addWidget(logHeader);
    logLayout->addWidget(Ui::separator(false, logPanel));

    auto *logProgressHost = new QWidget(logPanel);
    auto *logProgressLayout = new QVBoxLayout(logProgressHost);
    logProgressLayout->setContentsMargins(14, 8, 14, 8);
    logProgressLayout->setSpacing(4);
    auto *logProgressTop = new QHBoxLayout;
    logProgressTop->setContentsMargins(0, 0, 0, 0);
    logProgressTop->addWidget(Ui::label(QStringLiteral("理解口播"), 10, QFont::Normal,
                                         Theme::Text2, logProgressHost));
    logProgressTop->addStretch(1);
    m_logStageProgress = monoLabel(QStringLiteral("0%"), 9, Theme::Primary,
                                   logProgressHost);
    logProgressTop->addWidget(m_logStageProgress);
    logProgressLayout->addLayout(logProgressTop);
    m_logStageBar = new ProgressBar(logProgressHost);
    m_logStageBar->setBarColor(Theme::Primary);
    m_logStageBar->setValue(0);
    m_logStageBar->setFixedHeight(4);
    logProgressLayout->addWidget(m_logStageBar);
    logLayout->addWidget(logProgressHost);
    logLayout->addWidget(Ui::separator(false, logPanel));

    m_logContent = new QWidget;
    m_logLayout = new QVBoxLayout(m_logContent);
    m_logLayout->setContentsMargins(14, 8, 14, 8);
    m_logLayout->setSpacing(2);
    m_logLayout->setAlignment(Qt::AlignTop);
    m_logScroll = makeScrollArea(m_logContent, logPanel);
    logLayout->addWidget(m_logScroll, 1);
    logLayout->addWidget(Ui::separator(false, logPanel));

    auto *logFooter = new QWidget(logPanel);
    auto *logFooterLayout = new QHBoxLayout(logFooter);
    logFooterLayout->setContentsMargins(14, 5, 14, 5);
    logFooterLayout->setSpacing(8);
    auto *statusRow = new QHBoxLayout;
    statusRow->setContentsMargins(0, 0, 0, 0);
    statusRow->setSpacing(5);
    statusRow->addWidget(Studio::iconLabel(Icons::Glyph::Clock, 9, Theme::Text3,
                                            logFooter));
    auto *statusLabel = Ui::label(QStringLiteral("可切到其他页面，完成后自动下载"), 8,
                                  QFont::Normal, Theme::Text3, logFooter);
    statusRow->addWidget(statusLabel);
    logFooterLayout->addLayout(statusRow);
    logFooterLayout->addStretch(1);
    m_logFooter = monoLabel(QStringLiteral("字幕 0/0"), 8, Theme::Text3, logFooter);
    logFooterLayout->addWidget(m_logFooter);
    logLayout->addWidget(logFooter);
    bodyLayout->addWidget(logPanel, 1);

    root->addWidget(body, 1);
}

void OralPage::updateProcessing()
{
    if (!m_hasRun) {
        return;
    }
    const int percent = m_run.done ? 100
        : qMin(99, static_cast<int>(std::floor(m_run.progress)));
    if (m_progressLabel) {
        m_progressLabel->setText(
            QStringLiteral("<span style=\"font-size:23px;font-weight:650;color:%1;\">%2"
                           "</span><span style=\"font-size:10px;font-weight:400;"
                           "color:%3;\">%</span>")
                .arg(Theme::Primary.name())
                .arg(percent)
                .arg(Theme::Text3.name()));
    }
    if (m_processingProjectLabel && !m_run.previewCaption.isEmpty()) {
        // 语义分析完成后把真实标题写进过程页副标题，替换占位文案。
        m_processingProjectLabel->setText(
            QStringLiteral("%1 · 智能口播").arg(m_run.previewCaption.left(20)));
    }
    if (m_processingMeta) {
        m_processingMeta->setText(
            QStringLiteral("已用时 %1").arg(StudioData::formatElapsed(m_run.elapsedMs)));
    }
    if (m_etaLabel) {
        m_etaLabel->setText(m_run.done
                                ? QStringLiteral("全部步骤已完成")
                                : QStringLiteral("预计还需 %1")
                                      .arg(StudioData::formatEta(m_run.etaSeconds)));
        m_etaLabel->setStyleSheet(
            QStringLiteral("color:%1;background:transparent;")
                .arg((m_run.done ? Theme::Success : Theme::Text3).name()));
    }

    const QVector<Icons::Glyph> glyphs = {
        Icons::Glyph::Mic, Icons::Glyph::Image, Icons::Glyph::Wand,
        Icons::Glyph::Film,
    };
    const QStringList statusTexts = {QStringLiteral("等待"), QStringLiteral("进行中"),
                                     QStringLiteral("完成")};
    for (int index = 0; index < m_stageCards.size() && index < m_run.stages.size();
         ++index) {
        const StudioData::OralStageState &stage = m_run.stages.at(index);
        const int status = stage.status == StudioData::StageStatus::Done
            ? 2
            : (stage.status == StudioData::StageStatus::Running ? 1 : 0);
        m_stageCards.at(index)->setData(
            index + 1, glyphs.value(index, Icons::Glyph::Sparkle), stage.label,
            status, statusTexts.value(status), stage.detail,
            qRound(stage.progress), stage.status == StudioData::StageStatus::Running);
    }

    const int materialTotal = qMax(1, int(m_project.materials.size()));
    // 真实流水线返回已匹配素材数；示例过程按 8 个素材模拟时同样取真实值。
    const int matchedMaterials = qMin(materialTotal, m_run.matchedMaterials);
    if (m_processingPoster) {
        Studio::OralPosterSpec spec = m_processingPoster->spec();
        spec.caption = m_run.previewCaption;
        spec.keywords = m_run.previewKeywords;
        spec.materials = m_project.materials.mid(
            0, qMin(2, static_cast<int>(
                          std::ceil(matchedMaterials * 1.0 / materialTotal * 2))));
        spec.timecode = StudioData::formatElapsed(m_run.elapsedMs);
        spec.progress = qRound(m_run.progress);
        m_processingPoster->setSpec(spec);
    }

    if (m_keywordLayout && m_run.previewKeywords.size() != m_lastKeywordCount) {
        m_lastKeywordCount = m_run.previewKeywords.size();
        Studio::clearLayout(m_keywordLayout);
        if (m_run.previewKeywords.isEmpty()) {
            m_keywordStatus = Ui::label(QStringLiteral("正在读取口播内容…"), 8,
                                        QFont::Normal, Theme::Text3,
                                        m_keywordLayout->parentWidget());
            m_keywordLayout->addWidget(m_keywordStatus);
        } else {
            for (const QString &keyword : m_run.previewKeywords) {
                if (m_keywordLayout->count() >= 4) {
                    break;
                }
                m_keywordLayout->addWidget(Studio::tagLabel(
                    keyword, Theme::Ai, Theme::alpha(Theme::Ai, 0.13), 8,
                    m_keywordLayout->parentWidget()));
            }
        }
        m_keywordLayout->addStretch(1);
    }

    if (m_matchedCount) {
        m_matchedCount->setText(
            QStringLiteral("%1/%2").arg(matchedMaterials).arg(materialTotal));
    }
    const int thumbCount = qMin(
        2, static_cast<int>(std::ceil(matchedMaterials * 1.0 / materialTotal * 2)));
    if (m_matchedLayout && thumbCount != m_lastMatchedCount) {
        m_lastMatchedCount = thumbCount;
        Studio::clearLayout(m_matchedLayout);
        if (thumbCount == 0) {
            m_matchedLayout->addWidget(
                Ui::label(QStringLiteral("等待口播理解完成"), 8, QFont::Normal,
                          Theme::Text3, m_matchedLayout->parentWidget()));
        } else {
            for (const StudioData::OralMaterial &material :
                 m_project.materials.mid(0, thumbCount)) {
                auto *thumb = new Studio::MaterialVisualWidget(
                    m_matchedLayout->parentWidget());
                thumb->setMaterial(material);
                thumb->setFixedSize(28, 20);
                m_matchedLayout->addWidget(thumb);
            }
            auto *eq = new QWidget(m_matchedLayout->parentWidget());
            auto *eqLayout = new QHBoxLayout(eq);
            eqLayout->setContentsMargins(0, 0, 0, 0);
            eqLayout->setSpacing(2);
            for (int index = 0; index < 5; ++index) {
                auto *bar = new QWidget(eq);
                bar->setFixedSize(2, 5 + (index * 3) % 8);
                bar->setStyleSheet(QStringLiteral("background:%1;border-radius:1px;")
                                       .arg(Theme::alpha(Theme::Ai, 0.7).name()));
                eqLayout->addWidget(bar, 0, Qt::AlignBottom);
            }
            m_matchedLayout->addStretch(1);
            m_matchedLayout->addWidget(eq, 0, Qt::AlignVCenter);
        }
    }

    if (m_processingSourceMeta) {
        if (m_run.stage == StudioData::OralStage::Render) {
            m_processingSourceMeta->setText(
                QStringLiteral("%1/%2 帧")
                    .arg(StudioData::formatNumber(m_run.renderedFrames),
                         StudioData::formatNumber(m_run.totalFrames)));
        } else if (m_run.stage == StudioData::OralStage::Compose) {
            m_processingSourceMeta->setText(
                QStringLiteral("%1 条字幕").arg(m_run.subtitleCount));
        } else if (m_run.stage == StudioData::OralStage::Match) {
            m_processingSourceMeta->setText(
                QStringLiteral("%1 个素材").arg(materialTotal));
        } else {
            m_processingSourceMeta->setText(
                QStringLiteral("%1 字口播")
                    .arg(m_project.source.words > 0 ? m_project.source.words : 486));
        }
    }

    const StudioData::OralStageState *current = nullptr;
    for (const StudioData::OralStageState &stage : m_run.stages) {
        if (stage.key == m_run.stage) {
            current = &stage;
            break;
        }
    }
    if (!current && !m_run.stages.isEmpty()) {
        current = &m_run.stages.first();
    }
    if (current) {
        if (m_logStageTitle) {
            m_logStageTitle->setText(QStringLiteral("实时过程 · %1").arg(current->label));
        }
        if (m_logStageDetail) {
            m_logStageDetail->setText(current->detail);
        }
        if (m_logStageProgress) {
            m_logStageProgress->setText(
                QStringLiteral("%1%").arg(qFloor(current->progress)));
        }
        if (m_logStageBar) {
            m_logStageBar->setValue(qRound(current->progress));
        }
    }
    if (m_logElapsedLabel) {
        m_logElapsedLabel->setText(StudioData::formatElapsed(m_run.elapsedMs));
    }
    if (m_logStepsLabel) {
        int doneCount = 0;
        for (const StudioData::OralStageState &stage : m_run.stages) {
            if (stage.status == StudioData::StageStatus::Done) {
                ++doneCount;
            }
        }
        m_logStepsLabel->setText(
            QStringLiteral("%1/%2 步").arg(doneCount).arg(m_run.stages.size()));
    }
    if (m_logFooter) {
        const int sfxCount = m_project.sfx
            ? qMin(4, qRound(m_run.subtitleCount / 6.0))
            : 0;
        m_logFooter->setText(
            QStringLiteral("字幕 %1 · 音效 %2")
                .arg(m_run.subtitleCount)
                .arg(sfxCount));
    }

    if (m_logLayout && m_run.logs.size() != m_logCount) {
        m_logCount = m_run.logs.size();
        Studio::clearLayout(m_logLayout);
        if (m_run.logs.isEmpty()) {
            auto *row = new QWidget;
            auto *rowLayout = new QHBoxLayout(row);
            rowLayout->setContentsMargins(0, 6, 0, 6);
            rowLayout->setSpacing(6);
            rowLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 9,
                                                    Theme::Ai, row));
            rowLayout->addWidget(Ui::label(QStringLiteral("正在连接 AI 服务，准备开始…"),
                                            10, QFont::Normal, Theme::Text3, row));
            rowLayout->addStretch(1);
            m_logLayout->addWidget(row);
        } else {
            QString lastStage;
            for (const StudioData::OralLogEntry &log : m_run.logs) {
                const QString stageLabel = [&] {
                    for (const StudioData::OralStageState &stage : m_run.stages) {
                        if (stage.key == log.stage) {
                            return stage.label;
                        }
                    }
                    return QString();
                }();
                if (stageLabel != lastStage) {
                    lastStage = stageLabel;
                    auto *sep = new QWidget;
                    auto *sepLayout = new QHBoxLayout(sep);
                    sepLayout->setContentsMargins(0, 6, 0, 2);
                    sepLayout->setSpacing(6);
                    auto *line1 = new QWidget(sep);
                    line1->setFixedHeight(1);
                    line1->setStyleSheet(
                        QStringLiteral("background:%1;").arg(Theme::Border.name()));
                    line1->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
                    sepLayout->addWidget(line1, 1);
                    sepLayout->addWidget(Ui::label(stageLabel, 8, QFont::Normal,
                                                     Theme::Text3, sep));
                    auto *line2 = new QWidget(sep);
                    line2->setFixedHeight(1);
                    line2->setStyleSheet(
                        QStringLiteral("background:%1;").arg(Theme::Border.name()));
                    line2->setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
                    sepLayout->addWidget(line2, 1);
                    m_logLayout->addWidget(sep);
                }
                auto *row = new QWidget;
                auto *rowLayout = new QHBoxLayout(row);
                rowLayout->setContentsMargins(6, 2, 6, 2);
                rowLayout->setSpacing(7);
                auto *dot = new QWidget(row);
                dot->setFixedSize(4, 4);
                QColor dotColor = Theme::BorderStrong;
                QColor textColor = Theme::Text3;
                if (log.tone == StudioData::Tone::Ai) {
                    dotColor = Theme::Ai;
                    textColor = Theme::Ai;
                } else if (log.tone == StudioData::Tone::Ok) {
                    dotColor = Theme::Success;
                    textColor = Theme::Success;
                } else if (log.tone == StudioData::Tone::Info) {
                    dotColor = Theme::Primary;
                    textColor = Theme::Text2;
                }
                dot->setStyleSheet(QStringLiteral("background:%1;border-radius:2px;")
                                       .arg(dotColor.name()));
                rowLayout->addWidget(dot, 0, Qt::AlignTop);
                auto *text = Ui::label(log.text, 9, QFont::Normal, textColor, row);
                text->setWordWrap(true);
                rowLayout->addWidget(text, 1);
                rowLayout->addWidget(monoLabel(StudioData::formatElapsed(log.atMs), 8,
                                                Theme::Text3, row),
                                     0, Qt::AlignTop);
                m_logLayout->addWidget(row);
            }
        }
        m_logLayout->addStretch(1);
        QTimer::singleShot(0, this, [this] {
            if (m_logScroll && m_logScroll->verticalScrollBar()) {
                m_logScroll->verticalScrollBar()->setValue(
                    m_logScroll->verticalScrollBar()->maximum());
            }
        });
    }
}

void OralPage::onPipelineRunUpdated(const StudioData::OralRun &run)
{
    if (!m_hasRun || run.runId != m_runId) {
        return;
    }
    m_run = run;
    updateProcessing();
    const int percent = qMin(99, static_cast<int>(std::floor(m_run.progress)));
    emit taskStatusChanged(
        QStringLiteral("智能口播中 %1% · %2")
            .arg(percent)
            .arg(StudioData::formatElapsed(m_run.elapsedMs)),
        true);
}

void OralPage::onPipelineFinished(bool success, const QString &message,
                                  const StudioData::OralResult &result)
{
    // 取消旧项目后旧线程可能晚到；只接受当前服务实例的结果。
    if (sender() && m_pipelineService && sender() != m_pipelineService) {
        return;
    }
    if (!m_hasRun) {
        return;
    }
    m_runTimer->stop();
    m_hasRun = false;
    m_pipelineRunning = false;
    m_pipelineService = nullptr;
    if (m_pipelineThread) {
        m_pipelineThread->quit();
        m_pipelineThread = nullptr;
    }
    emit taskStatusChanged(QString(), false);

    if (!success) {
        m_run.done = true;
        updateProcessing();
        QMessageBox::warning(this, QStringLiteral("智能口播生成失败"),
                             message);
        emit toastRequested(QStringLiteral("智能口播生成失败：%1").arg(message));
        m_project.step = 2;
        showSetup();
        return;
    }

    m_run.done = true;
    m_run.progress = 100.0;
    updateProcessing();
    m_result = result;
    m_hasResult = true;
    // 真实生成记录持久化，演示数据只作为历史列表的补充。
    m_history.prepend(m_result);
    m_persistedHistory.prepend(m_result);
    saveHistory();
    m_hubScrollTop = 0;
    showResult();
    if (!isVisible()) {
        emit toastRequested(QStringLiteral(
            "智能口播成片已生成并自动下载，可预览查看"));
    } else {
        emit toastRequested(message);
    }
}

void OralPage::destroyPipeline()
{
    if (m_pipelineService) {
        // cancel() 只写原子标记，可从界面线程直接调用。
        m_pipelineService->cancel();
    }
    if (m_pipelineThread) {
        m_pipelineThread->quit();
        // 阻塞网络请求无法立即中断；最多等 3 秒，之后让线程自行结束并回收。
        if (!m_pipelineThread->wait(3000)) {
            disconnect(m_pipelineThread, nullptr, this, nullptr);
        }
    }
    m_pipelineService = nullptr;
    m_pipelineThread = nullptr;
    m_pipelineRunning = false;
}

QString oralHistoryFilePath()
{
    QString dir = QStandardPaths::writableLocation(QStandardPaths::AppDataLocation);
    if (dir.isEmpty()) {
        dir = QDir::homePath() + QStringLiteral("/.livemix");
    }
    return QDir(dir).filePath(QStringLiteral("oral_history.json"));
}

void OralPage::loadHistory()
{
    m_persistedHistory.clear();
    QFile file(oralHistoryFilePath());
    if (!file.exists() || !file.open(QIODevice::ReadOnly)) {
        return;
    }
    const QJsonDocument document = QJsonDocument::fromJson(file.readAll());
    QJsonArray results;
    if (document.isArray()) {
        results = document.array();
    } else if (document.isObject()) {
        results = document.object().value(QStringLiteral("results")).toArray();
    }
    for (const QJsonValue &value : results) {
        const StudioData::OralResult result =
            Oral::oralResultFromJson(value.toObject());
        // 只保留有真实成片路径的记录，避免把损坏的历史当成可播放结果。
        if (!result.id.isEmpty() && !result.filePath.isEmpty()) {
            m_persistedHistory.append(result);
        }
    }
    m_history = m_persistedHistory;
}

void OralPage::saveHistory()
{
    QString dir = QFileInfo(oralHistoryFilePath()).absolutePath();
    if (!QDir().mkpath(dir)) {
        return;
    }
    QJsonArray results;
    for (const StudioData::OralResult &result : m_persistedHistory) {
        results.append(Oral::oralResultToJson(result));
    }
    QJsonObject root;
    root.insert(QStringLiteral("version"), 1);
    root.insert(QStringLiteral("results"), results);
    QSaveFile file(oralHistoryFilePath());
    if (!file.open(QIODevice::WriteOnly)) {
        return;
    }
    file.write(QJsonDocument(root).toJson(QJsonDocument::Indented));
    file.commit();
}

/* ------------------------------------------------------------------ */
/* 结果                                                                 */
/* ------------------------------------------------------------------ */

void OralPage::rebuildResult()
{
    // 必须在清空旧布局前停止并释放播放器：视频画面是结果页的子控件，
    // 布局清空后指针会悬空，先 reset 可以避免重复析构。
    resetResultPlayer();
    auto *root = qobject_cast<QVBoxLayout *>(m_resultScreen->layout());
    if (!root) {
        root = new QVBoxLayout(m_resultScreen);
        root->setContentsMargins(0, 0, 0, 0);
        root->setSpacing(0);
    }
    Studio::clearLayout(root);

    m_resultPoster = nullptr;
    m_resultGenerationLabel = nullptr;
    m_resultPlayClock = nullptr;
    m_resultPlayButton = nullptr;
    m_resultPlayBar = nullptr;
    m_resultDraftButton = nullptr;
    m_resultDraftHint = nullptr;
    m_resultCenterPlay = nullptr;
    m_resultPlayToggle = nullptr;

    if (!m_hasResult) {
        auto *empty = new QWidget(m_resultScreen);
        auto *emptyLayout = new QVBoxLayout(empty);
        emptyLayout->setContentsMargins(40, 40, 40, 40);
        emptyLayout->setSpacing(10);
        emptyLayout->setAlignment(Qt::AlignCenter);
        auto *icon = new QWidget(empty);
        Q_UNUSED(icon);
        emptyLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 20,
                                                  Theme::Ai, empty),
                               0, Qt::AlignHCenter);
        emptyLayout->addWidget(Ui::label(QStringLiteral("还没有可查看的口播成片"), 15,
                                          QFont::Medium, Theme::Text1, empty),
                               0, Qt::AlignHCenter);
        auto *hint = Ui::label(
            QStringLiteral("从智能口播首页开始一个项目，成片会自动下载，并在这里预览、复制发布文案或生成剪映草稿。"),
            11, QFont::Normal, Theme::Text3, empty);
        hint->setWordWrap(true);
        hint->setAlignment(Qt::AlignCenter);
        hint->setMaximumWidth(380);
        emptyLayout->addWidget(hint, 0, Qt::AlignHCenter);
        auto *back = new Button(QStringLiteral("返回智能口播"), Button::Primary, empty);
        connect(back, &QAbstractButton::clicked, this, &OralPage::showHub);
        emptyLayout->addWidget(back, 0, Qt::AlignHCenter);
        root->addWidget(empty, 1);
        return;
    }

    m_playing = false;
    m_playProgress = 0;
    m_draftState = m_result.draftGenerated ? 2 : 0;
    m_draftProgress = 100 * (m_draftState == 2 ? 1 : 0);
    if (m_playTimer) {
        m_playTimer->stop();
    }

    const StudioData::OralTemplate tmpl =
        StudioData::oralTemplate(m_result.templateId);
    QString fileName = m_result.projectName;
    fileName.replace(QStringLiteral(" · "), QStringLiteral("_"));
    const QString movies = QStandardPaths::writableLocation(
        QStandardPaths::MoviesLocation);
    // 真实结果直接使用流水线返回的路径；示例历史退回设计稿路径。
    m_resultFilePath = m_result.filePath.isEmpty()
        ? QDir(movies).filePath(
              QStringLiteral("LiveMix/口播成片/%1.mp4").arg(fileName))
        : m_result.filePath;
    m_resultDraftPath = !m_result.draftPath.isEmpty()
        ? m_result.draftPath
        : QDir(movies).filePath(
              QStringLiteral("LiveMix/CapDraft/%1_%2").arg(fileName, m_result.id));
    const QString videoFileName = QFileInfo(m_resultFilePath).fileName().isEmpty()
        ? fileName + QStringLiteral(".mp4")
        : QFileInfo(m_resultFilePath).fileName();

    auto *header = new QWidget(m_resultScreen);
    auto *headerLayout = new QVBoxLayout(header);
    headerLayout->setContentsMargins(22, 10, 22, 10);
    headerLayout->setSpacing(0);

    auto *breadcrumb = new QWidget(header);
    auto *breadcrumbLayout = new QHBoxLayout(breadcrumb);
    breadcrumbLayout->setContentsMargins(0, 0, 0, 0);
    breadcrumbLayout->setSpacing(6);
    auto *back = new LinkLabel(QStringLiteral("智能口播"), breadcrumb);
    back->setPixelSize(10);
    back->setTextColor(Theme::Text3);
    back->setHoverColor(Theme::Text1);
    back->setLeadingGlyph(Icons::Glyph::ChevronLeft, 10);
    back->setUnderlineOnHover(false);
    connect(back, &QAbstractButton::clicked, this, &OralPage::showHub);
    breadcrumbLayout->addWidget(back);
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("/"), 10, QFont::Normal,
                                           Theme::BorderStrong, breadcrumb));
    breadcrumbLayout->addWidget(Ui::label(QStringLiteral("成片结果"), 10,
                                           QFont::Normal, Theme::Text2, breadcrumb));
    breadcrumbLayout->addSpacing(4);
    auto *saved = new QWidget(breadcrumb);
    auto *savedLayout = new QHBoxLayout(saved);
    savedLayout->setContentsMargins(0, 0, 0, 0);
    savedLayout->setSpacing(4);
    savedLayout->addWidget(new StatusDot(6, Theme::Success, saved));
    savedLayout->addWidget(Ui::label(QStringLiteral("已自动保存"), 9,
                                      QFont::Normal, Theme::Success, saved));
    breadcrumbLayout->addWidget(saved);
    breadcrumbLayout->addStretch(1);
    headerLayout->addWidget(breadcrumb);
    headerLayout->addSpacing(4);

    auto *titleRow = new QWidget(header);
    auto *titleLayout = new QHBoxLayout(titleRow);
    titleLayout->setContentsMargins(0, 0, 0, 0);
    titleLayout->setSpacing(10);
    auto *titles = new QVBoxLayout;
    titles->setContentsMargins(0, 0, 0, 0);
    titles->setSpacing(2);
    auto *titleLine = new QHBoxLayout;
    titleLine->setContentsMargins(0, 0, 0, 0);
    titleLine->setSpacing(8);
    auto *projectName = new ElidedLabel(m_result.projectName, header);
    projectName->setSizePolicy(QSizePolicy::Preferred, QSizePolicy::Fixed);
    projectName->setPixelSize(17);
    projectName->setFontWeight(QFont::DemiBold);
    projectName->setTextColor(Theme::Text1);
    titleLine->addWidget(projectName, 0);
    titleLine->addWidget(new Chip(QStringLiteral("已生成并自动下载"), Chip::Ok,
                                   header));
    titleLine->addWidget(makeGenerationPill(&m_resultGenerationLabel, header));
    titleLine->addStretch(1);
    titles->addLayout(titleLine);
    auto *metaLine = new QHBoxLayout;
    metaLine->setContentsMargins(0, 0, 0, 0);
    metaLine->setSpacing(8);
    metaLine->addWidget(Ui::label(m_result.sourceLabel, 10, QFont::Normal,
                                    Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(Ui::label(QStringLiteral("模板：%1").arg(tmpl.name), 10,
                                    QFont::Normal, Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(Ui::label(
        QStringLiteral("%1 · %2").arg(m_result.ratio,
                                       StudioData::oralRatioDimensions(m_result.ratio)),
        10, QFont::Normal, Theme::Text3, header));
    metaLine->addWidget(Ui::label(QStringLiteral("·"), 10, QFont::Normal,
                                    Theme::BorderStrong, header));
    metaLine->addWidget(monoLabel(m_result.durationLabel, 10, Theme::Text3, header));
    metaLine->addStretch(1);
    titles->addLayout(metaLine);
    titleLayout->addLayout(titles, 1);

    auto *backToHub = new Button(QStringLiteral("返回"), Button::Ghost, header);
    backToHub->setLeadingGlyph(Icons::Glyph::ChevronLeft);
    backToHub->setToolTip(QStringLiteral("返回智能口播"));
    connect(backToHub, &QAbstractButton::clicked, this, &OralPage::showHub);
    titleLayout->addWidget(backToHub);
    auto *restart = new Button(QStringLiteral("重新包装"), Button::Default, header);
    restart->setLeadingGlyph(Icons::Glyph::Reset);
    connect(restart, &QAbstractButton::clicked, this, &OralPage::restartFromResult);
    titleLayout->addWidget(restart);
    m_resultPlayButton = new Button(QStringLiteral("播放成片"), Button::Primary,
                                    header);
    m_resultPlayButton->setLeadingGlyph(Icons::Glyph::Play);
    connect(m_resultPlayButton, &QAbstractButton::clicked, this,
            &OralPage::togglePlayback);
    titleLayout->addWidget(m_resultPlayButton);
    headerLayout->addWidget(titleRow);
    root->addWidget(header);
    root->addWidget(Ui::separator(false, m_resultScreen));

    auto *scrollContent = new QWidget;
    auto *scrollLayout = new QVBoxLayout(scrollContent);
    scrollLayout->setContentsMargins(22, 14, 22, 16);
    scrollLayout->setSpacing(0);
    auto *columns = new QWidget(scrollContent);
    auto *columnsLayout = new QHBoxLayout(columns);
    columnsLayout->setContentsMargins(0, 0, 0, 0);
    columnsLayout->setSpacing(16);

    auto *left = new QWidget(columns);
    auto *leftLayout = new QVBoxLayout(left);
    leftLayout->setContentsMargins(0, 0, 0, 0);
    leftLayout->setSpacing(12);

    auto *previewPanel = new QWidget(left);
    previewPanel->setObjectName(QStringLiteral("oralResultPreview"));
    previewPanel->setStyleSheet(
        QStringLiteral("#oralResultPreview { background:%1; border:1px solid %2;"
                       " border-radius:10px; }")
            .arg(QColor(QStringLiteral("#0c0e14")).name(), Theme::Border.name()));
    auto *previewLayout = new QVBoxLayout(previewPanel);
    previewLayout->setContentsMargins(16, 16, 16, 16);
    previewLayout->setSpacing(0);
    previewLayout->setAlignment(Qt::AlignHCenter);
    const bool portrait = m_result.ratio == QLatin1String("9:16");
    const int posterHeight = portrait ? 494 : 242;
    const int posterWidth = portrait ? qRound(posterHeight * 9.0 / 16.0)
                                     : qRound(posterHeight * 16.0 / 9.0);
    auto *poster = new Studio::OralPosterWidget(previewPanel);
    poster->setFixedSize(posterWidth, posterHeight);
    Studio::OralPosterSpec spec;
    spec.templateId = m_result.templateId;
    spec.ratio = m_result.ratio;
    spec.headline = m_result.projectName.split(QStringLiteral(" · ")).value(0);
    spec.caption = tmpl.layout == StudioData::OralLayout::Punch
        ? m_result.tags.value(0).mid(1)
        : StudioData::shortOralCaption(m_result.script);
    if (spec.caption.isEmpty()) {
        spec.caption = tmpl.caption;
    }
    spec.keywords = m_result.tags.mid(0, 2);
    for (QString &keyword : spec.keywords) {
        keyword = keyword.mid(1);
    }
    spec.materials = m_result.materials.isEmpty()
        ? StudioData::demoMaterials().mid(0, 2)
        : m_result.materials.mid(0, 2);
    // 生成完成后用真实成片首帧（封面海报）替换设计稿插画。
    spec.frameImage = loadResultFramePreview(m_result.previewPath,
                                             m_resultFilePath);
    spec.timecode = m_result.durationLabel;
    spec.progress = 0;
    poster->setSpec(spec);
    m_resultPoster = poster;
    auto *posterHost = new QWidget(previewPanel);
    posterHost->setFixedSize(posterWidth, posterHeight);
    auto *posterHostLayout = new QGridLayout(posterHost);
    posterHostLayout->setContentsMargins(0, 0, 0, 0);
    posterHostLayout->addWidget(poster, 0, 0);
    m_resultCenterPlay = makeResultPlayOverlay(posterHost);
    connect(m_resultCenterPlay, &QToolButton::clicked, this,
            &OralPage::togglePlayback);
    posterHostLayout->addWidget(m_resultCenterPlay, 0, 0, Qt::AlignCenter);

    // 海报与真实视频放在同一个堆叠控件：未播放时展示设计稿海报，
    // 点击播放后切到 QMediaPlayer 画面，进度条与时间码随播放器更新。
    m_resultMediaStack = new QStackedWidget(previewPanel);
    m_resultMediaStack->setFixedSize(posterWidth, posterHeight);
    m_resultMediaStack->addWidget(posterHost);
    m_resultHasVideo = QFileInfo::exists(m_resultFilePath);
    if (m_resultHasVideo) {
        auto *videoHost = new QWidget(m_resultMediaStack);
        videoHost->setStyleSheet(QStringLiteral("background:#000;"));
        auto *videoLayout = new QVBoxLayout(videoHost);
        videoLayout->setContentsMargins(0, 0, 0, 0);
        videoLayout->setSpacing(0);
        m_resultVideoView = new QGraphicsView(videoHost);
        m_resultVideoView->setFrameShape(QFrame::NoFrame);
        m_resultVideoView->setHorizontalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_resultVideoView->setVerticalScrollBarPolicy(Qt::ScrollBarAlwaysOff);
        m_resultVideoView->setAlignment(Qt::AlignCenter);
        m_resultVideoView->setStyleSheet(
            QStringLiteral("background:#000;border:none;"));
        auto *scene = new QGraphicsScene(m_resultVideoView);
        scene->setBackgroundBrush(Qt::black);
        m_resultVideoItem = new QGraphicsVideoItem;
        m_resultVideoItem->setAspectRatioMode(Qt::KeepAspectRatio);
        m_resultVideoItem->setSize(QSizeF(posterWidth, posterHeight));
        scene->addItem(m_resultVideoItem);
        scene->setSceneRect(0, 0, posterWidth, posterHeight);
        m_resultVideoView->setScene(scene);
        videoLayout->addWidget(m_resultVideoView);
        m_resultPlayer = new QMediaPlayer(this);
        m_resultAudio = new QAudioOutput(this);
        m_resultAudio->setVolume(0.9);
        m_resultPlayer->setAudioOutput(m_resultAudio);
        m_resultPlayer->setVideoOutput(m_resultVideoItem);
        m_resultPlayer->setSource(QUrl::fromLocalFile(m_resultFilePath));
        connect(m_resultPlayer, &QMediaPlayer::errorOccurred, this,
                [this](QMediaPlayer::Error, const QString &errorText) {
                    if (!errorText.isEmpty()) {
                        emit toastRequested(
                            QStringLiteral("成片播放失败：%1").arg(errorText));
                    }
                });
        // 以播放器的真实结束信号为准；定时器轮询只作为兜底。
        connect(m_resultPlayer, &QMediaPlayer::mediaStatusChanged, this,
                [this](QMediaPlayer::MediaStatus status) {
                    if (status == QMediaPlayer::EndOfMedia) {
                        finishPlayback();
                    }
                });
        m_resultMediaStack->addWidget(videoHost);
    }
    previewLayout->addWidget(m_resultMediaStack, 0, Qt::AlignHCenter);
    previewLayout->addSpacing(12);

    auto *playRow = new QWidget(previewPanel);
    auto *playLayout = new QHBoxLayout(playRow);
    playLayout->setContentsMargins(0, 0, 0, 0);
    playLayout->setSpacing(8);
    m_resultPlayToggle = new GlyphButton(Icons::Glyph::Play, 28, playRow);
    m_resultPlayToggle->setGlyphSize(11);
    m_resultPlayToggle->setColors(Theme::Text1, Theme::Primary);
    m_resultPlayToggle->setBackgroundColors(Theme::Surface2, Theme::Surface3);
    m_resultPlayToggle->setToolTipText(QStringLiteral("播放或暂停"));
    connect(m_resultPlayToggle, &QAbstractButton::clicked, this,
            &OralPage::togglePlayback);
    playLayout->addWidget(m_resultPlayToggle);
    auto *playBarHost = new QWidget(playRow);
    auto *playBarLayout = new QVBoxLayout(playBarHost);
    playBarLayout->setContentsMargins(0, 0, 0, 0);
    playBarLayout->setSpacing(2);
    m_resultPlayBar = new ProgressBar(playBarHost);
    m_resultPlayBar->setBarColor(Theme::Primary);
    m_resultPlayBar->setValue(0);
    m_resultPlayBar->setFixedHeight(4);
    playBarLayout->addWidget(m_resultPlayBar);
    auto *clockRow = new QHBoxLayout;
    clockRow->setContentsMargins(0, 0, 0, 0);
    auto *timeLabel = monoLabel(QStringLiteral("00:00"), 8, Theme::Text3, playBarHost);
    m_resultPlayClock = timeLabel;
    clockRow->addWidget(timeLabel);
    clockRow->addStretch(1);
    clockRow->addWidget(monoLabel(m_result.durationLabel, 8, Theme::Text3,
                                  playBarHost));
    playBarLayout->addLayout(clockRow);
    playLayout->addWidget(playBarHost, 1);
    auto *ratioTag = Studio::tagLabel(m_result.ratio, QColor(255, 255, 255, 191),
                                      QColor(0, 0, 0, 128), 8, playRow);
    playLayout->addWidget(ratioTag, 0, Qt::AlignVCenter);
    previewLayout->addWidget(playRow);
    previewLayout->addSpacing(12);

    auto *statsGridHost = new QWidget(previewPanel);
    auto *statsGrid = new QGridLayout(statsGridHost);
    statsGrid->setContentsMargins(0, 0, 0, 0);
    statsGrid->setHorizontalSpacing(6);
    statsGrid->setVerticalSpacing(6);
    statsGrid->addWidget(new Studio::StatTile(QStringLiteral("文件大小"),
                                              m_result.fileSize, statsGridHost),
                         0, 0);
    statsGrid->addWidget(new Studio::StatTile(
                             QStringLiteral("成片画幅"),
                             StudioData::oralRatioDimensions(m_result.ratio),
                             statsGridHost),
                         0, 1);
    statsGrid->addWidget(new Studio::StatTile(QStringLiteral("包装模板"), tmpl.name,
                                              statsGridHost),
                         1, 0);
    statsGrid->addWidget(new Studio::StatTile(
                             QStringLiteral("B-Roll"),
                             QStringLiteral("%1 处").arg(m_result.brollCount),
                             statsGridHost),
                         1, 1);
    previewLayout->addWidget(statsGridHost);
    leftLayout->addWidget(previewPanel);

    auto *pathNote = new Studio::Note(
        QStringLiteral("成片已保存到本地输出目录：<br><span style=\"font-family:monospace;"
                       "color:%1;\">%2</span>")
            .arg(Theme::Text2.name(), Studio::formatClipPath(m_resultFilePath, 64)));
    leftLayout->addWidget(pathNote);
    leftLayout->addStretch(1);
    columnsLayout->addWidget(left, 84);

    auto *right = new QWidget(columns);
    auto *rightLayout = new QVBoxLayout(right);
    rightLayout->setContentsMargins(0, 0, 0, 0);
    rightLayout->setSpacing(12);

    auto *publish = new Studio::SectionCard(
        Icons::Glyph::Sparkle, QStringLiteral("发布文案"),
        QStringLiteral("AI 生成，可直接复制"), right);
    auto *copyAll = new LinkLabel(QStringLiteral("复制全部"), publish);
    copyAll->setPixelSize(9);
    copyAll->setTextColor(Theme::Primary);
    copyAll->setHoverColor(Theme::PrimaryHover);
    copyAll->setUnderlineOnHover(false);
    connect(copyAll, &QAbstractButton::clicked, this, [this] {
        copyText(QStringLiteral("%1\n%2\n%3")
                     .arg(m_result.title, m_result.description,
                          m_result.tags.join(QLatin1Char(' '))),
                 QStringLiteral("标题、描述与话题"));
    });
    publish->headerLayout()->addWidget(copyAll);
    publish->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *publishBody = publish->bodyLayout();
    publishBody->setSpacing(0);
    auto addPublishRow = [&](const QString &label, QWidget *content,
                             const QString &copy, const QString &copyLabel, int gap) {
        auto *row = new QWidget(publish);
        auto *rowLayout = new QHBoxLayout(row);
        rowLayout->setContentsMargins(0, 0, 0, 0);
        rowLayout->setSpacing(8);
        auto *labelWidget = Ui::label(label, 9, QFont::Normal, Theme::Text3, row);
        labelWidget->setFixedWidth(38);
        rowLayout->addWidget(labelWidget, 0, Qt::AlignTop);
        rowLayout->addWidget(content, 1, Qt::AlignVCenter);
        rowLayout->addWidget(makeCopyButton(
                                 copy, copyLabel,
                                 [this, copy, copyLabel] { copyText(copy, copyLabel); },
                                 row),
                             0, Qt::AlignTop);
        publishBody->addWidget(row);
        if (gap > 0) {
            publishBody->addSpacing(gap);
        }
    };
    auto *titleLabel = Ui::label(m_result.title, 11, QFont::Medium, Theme::Text1,
                                 publish);
    titleLabel->setWordWrap(true);
    addPublishRow(QStringLiteral("标题"), titleLabel, m_result.title,
                  QStringLiteral("标题"), 8);
    auto *descriptionLabel = Ui::label(m_result.description, 10, QFont::Normal,
                                       Theme::Text2, publish);
    descriptionLabel->setWordWrap(true);
    addPublishRow(QStringLiteral("描述"), descriptionLabel, m_result.description,
                  QStringLiteral("描述"), 8);
    auto *tagsHost = new QWidget(publish);
    auto *tagsLayout = new QHBoxLayout(tagsHost);
    tagsLayout->setContentsMargins(0, 0, 0, 0);
    tagsLayout->setSpacing(4);
    for (const QString &tag : m_result.tags) {
        tagsLayout->addWidget(Studio::tagLabel(
            tag, Theme::Primary, Theme::alpha(Theme::Primary, 0.14), 9, tagsHost));
    }
    tagsLayout->addStretch(1);
    addPublishRow(QStringLiteral("话题"), tagsHost,
                  m_result.tags.join(QLatin1Char(' ')), QStringLiteral("话题"), 0);
    rightLayout->addWidget(publish);

    auto *details = new Studio::SectionCard(
        Icons::Glyph::Sparkle, QStringLiteral("包装明细"),
        QStringLiteral("%1 项自动处理").arg(m_result.highlights.size()), right);
    details->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *detailsGrid = new QGridLayout;
    detailsGrid->setContentsMargins(0, 0, 0, 0);
    detailsGrid->setHorizontalSpacing(6);
    detailsGrid->setVerticalSpacing(6);
    for (int index = 0; index < m_result.highlights.size(); ++index) {
        detailsGrid->addWidget(
            new Studio::StatTile(m_result.highlights.at(index).first,
                                 m_result.highlights.at(index).second, details),
            index / 4, index % 4);
    }
    details->bodyLayout()->addLayout(detailsGrid);
    details->bodyLayout()->addSpacing(8);
    auto *detailChips = new QWidget(details);
    auto *detailChipsLayout = new QHBoxLayout(detailChips);
    detailChipsLayout->setContentsMargins(0, 0, 0, 0);
    detailChipsLayout->setSpacing(6);
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("模板 · %1").arg(tmpl.name), Chip::Neutral, detailChips));
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("字幕 · %1 条").arg(m_result.subtitleCount), Chip::Neutral,
        detailChips));
    detailChipsLayout->addWidget(new Chip(
        QStringLiteral("B-Roll · %1 处").arg(m_result.brollCount), Chip::Neutral,
        detailChips));
    detailChipsLayout->addStretch(1);
    detailChipsLayout->addWidget(
        Ui::label(QStringLiteral("人声、画面、字幕与音效已自动对齐"), 8,
                  QFont::Normal, Theme::Text3, detailChips));
    details->bodyLayout()->addWidget(detailChips);
    rightLayout->addWidget(details);

    auto *output = new Studio::SectionCard(
        Icons::Glyph::Download, QStringLiteral("输出文件"),
        QStringLiteral("已自动保存到本地"), right);
    output->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *outputBody = output->bodyLayout();
    outputBody->setSpacing(8);
    auto *videoRight = new Button(QStringLiteral("打开成片目录"), Button::Ghost,
                                  output);
    videoRight->setCompact(true);
    videoRight->setLeadingGlyph(Icons::Glyph::Folder);
    videoRight->setTextColor(Theme::Success);
    connect(videoRight, &QAbstractButton::clicked, this,
            &OralPage::openExportFolder);
    outputBody->addWidget(makeFileRow(
        Icons::Glyph::Film, videoFileName, QStringLiteral("MP4"),
        QStringLiteral("%1 · %2 · %3 · 30fps")
            .arg(StudioData::oralRatioDimensions(m_result.ratio), m_result.fileSize,
                 m_result.videoCodec.isEmpty() ? QStringLiteral("H.264")
                                               : m_result.videoCodec),
        videoRight, output));
    auto *draftRight = new QWidget(output);
    auto *draftRightLayout = new QHBoxLayout(draftRight);
    draftRightLayout->setContentsMargins(0, 0, 0, 0);
    draftRightLayout->setSpacing(0);
    m_resultDraftButton = new Button(QStringLiteral("生成草稿"), Button::Default,
                                     draftRight);
    m_resultDraftButton->setCompact(true);
    m_resultDraftButton->setLeadingGlyph(Icons::Glyph::Scissors);
    if (m_draftState == 2) {
        m_resultDraftButton->setText(QStringLiteral("打开草稿"));
        m_resultDraftButton->setLeadingGlyph(Icons::Glyph::Folder);
        m_resultDraftButton->setTextColor(Theme::Success);
    }
    connect(m_resultDraftButton, &QAbstractButton::clicked, this, [this] {
        if (m_draftState == 2) {
            openDraftFolder();
            return;
        }
        startDraftDownload();
    });
    draftRightLayout->addWidget(m_resultDraftButton);
    auto *draftRow = makeFileRow(
        Icons::Glyph::Scissors,
        QFileInfo(m_resultDraftPath).fileName().isEmpty()
            ? fileName + QStringLiteral("_剪映草稿")
            : QFileInfo(m_resultDraftPath).fileName(),
        QStringLiteral("草稿"),
        m_draftState == 2
            ? QStringLiteral("已写入剪映草稿目录，可继续修改文案、配音与镜头")
            : QStringLiteral("生成可二次编辑的剪映草稿"),
        draftRight, output);
    m_resultDraftHint = nullptr;
    for (QLabel *label : draftRow->findChildren<QLabel *>()) {
        if (label->text().startsWith(QStringLiteral("生成可二次编辑"))) {
            m_resultDraftHint = label;
            break;
        }
    }
    outputBody->addWidget(draftRow);
    auto *pathRow = new QWidget(output);
    auto *pathRowLayout = new QHBoxLayout(pathRow);
    pathRowLayout->setContentsMargins(0, 2, 0, 0);
    pathRowLayout->setSpacing(5);
    pathRowLayout->addWidget(Studio::iconLabel(Icons::Glyph::Folder, 9,
                                                 Theme::Text3, pathRow));
    auto *pathLabel = monoLabel(Studio::formatClipPath(m_resultDraftPath, 52), 8,
                                Theme::Text3, pathRow);
    pathLabel->setToolTip(m_resultDraftPath);
    pathRowLayout->addWidget(pathLabel, 1);
    pathRowLayout->addWidget(makeCopyButton(
                                 m_resultDraftPath, QStringLiteral("草稿路径"),
                                 [this] {
                                     copyText(m_resultDraftPath,
                                              QStringLiteral("草稿路径"));
                                 },
                                 pathRow),
                             0);
    outputBody->addWidget(pathRow);
    rightLayout->addWidget(output);

    auto *script = new Studio::SectionCard(
        Icons::Glyph::Doc, QStringLiteral("口播原稿"),
        QStringLiteral("%1 字")
            .arg(QString(m_result.script).remove(QRegularExpression(QStringLiteral("\\s"))).size()),
        right);
    auto *scriptCopy = new LinkLabel(QStringLiteral("复制"), script);
    scriptCopy->setPixelSize(9);
    scriptCopy->setTextColor(Theme::Primary);
    scriptCopy->setHoverColor(Theme::PrimaryHover);
    scriptCopy->setUnderlineOnHover(false);
    connect(scriptCopy, &QAbstractButton::clicked, this, [this] {
        copyText(m_result.script, QStringLiteral("口播文案"));
    });
    script->headerLayout()->addWidget(scriptCopy);
    script->setBodyMargins(QMargins(12, 12, 12, 12));
    auto *scriptEdit = new QTextEdit(script);
    scriptEdit->setReadOnly(true);
    Studio::applyTextAreaStyle(scriptEdit, false, 10);
    scriptEdit->setFixedHeight(128);
    scriptEdit->setPlainText(m_result.script);
    script->bodyLayout()->addWidget(scriptEdit);
    rightLayout->addWidget(script);

    auto *adjustTip = new QWidget(right);
    adjustTip->setObjectName(QStringLiteral("oralAdjustTip"));
    adjustTip->setStyleSheet(
        QStringLiteral("#oralAdjustTip { background:%1; border:1px solid %2;"
                       " border-radius:8px; }")
            .arg(Theme::alpha(Theme::Surface1, 0.6).name(QColor::HexArgb),
                 Theme::Border.name()));
    auto *adjustLayout = new QHBoxLayout(adjustTip);
    adjustLayout->setContentsMargins(12, 9, 12, 9);
    adjustLayout->setSpacing(8);
    adjustLayout->addWidget(Studio::iconLabel(Icons::Glyph::Sparkle, 10, Theme::Ai,
                                               adjustTip));
    auto *adjustLabel = Ui::label(
        QStringLiteral("对结果不满意？可以调整模板、字幕或配图风格后重新包装，口播原片不会重复上传，封面会重新生成。"),
        9, QFont::Normal, Theme::Text3, adjustTip);
    adjustLabel->setWordWrap(true);
    adjustLayout->addWidget(adjustLabel, 1);
    auto *adjust = new LinkLabel(
        QStringLiteral("调整设置并重新包装 →"), adjustTip);
    adjust->setPixelSize(9);
    adjust->setTextColor(Theme::Primary);
    adjust->setHoverColor(Theme::PrimaryHover);
    adjust->setUnderlineOnHover(true);
    connect(adjust, &QAbstractButton::clicked, this, &OralPage::restartFromResult);
    adjustLayout->addWidget(adjust, 0, Qt::AlignVCenter);
    rightLayout->addWidget(adjustTip);
    rightLayout->addStretch(1);
    columnsLayout->addWidget(right, 116);

    scrollLayout->addWidget(columns);
    auto *scroll = makeScrollArea(scrollContent, m_resultScreen);
    root->addWidget(scroll, 1);

    setResultPlayIcon(m_resultCenterPlay, false);
    updateGenerationClock();
    updatePlayback();
}

void OralPage::openRecent(const StudioData::OralResult &result)
{
    if (isRunning()) {
        showProcessing();
        emit toastRequested(QStringLiteral("当前有智能口播正在生成，完成后即可查看"));
        return;
    }
    m_result = result;
    m_hasResult = true;
    showResult();
}

void OralPage::restartFromResult()
{
    if (!m_hasResult) {
        return;
    }
    if (m_playTimer) {
        m_playTimer->stop();
    }
    resetResultPlayer();
    m_playing = false;
    m_playProgress = 0;
    m_draftState = 0;
    m_draftProgress = 0;
    const StudioData::OralResult result = m_result;

    // 重新包装必须沿用这条结果的原始输入，而不是界面上残留的其它项目。
    // 新历史直接带有口播路径与素材；旧历史从剪辑计划 JSON 补齐。
    const OralRestoredInputs inputs = restoreOralInputs(result);
    StudioData::OralProject project = StudioData::defaultOralProject();
    const bool hasSource = !inputs.sourcePath.isEmpty();
    if (hasSource) {
        // 与导入口播的流程保持一致：沿用示例结构里的文案占位，再覆盖真实文件信息。
        project.source = StudioData::demoOralSource();
        project.hasSource = true;
        project.source.filePath = inputs.sourcePath;
        project.source.name = !inputs.sourceName.isEmpty()
            ? inputs.sourceName
            : QFileInfo(inputs.sourcePath).fileName();
        project.source.size = MediaUtils::formatByteSize(
            inputs.sourceSizeBytes > 0 ? inputs.sourceSizeBytes
                                       : QFileInfo(inputs.sourcePath).size());
        project.source.sample = false;
        project.source.duration = inputs.sourceDurationMs > 0
            ? MediaUtils::formatDuration(inputs.sourceDurationMs)
            : QStringLiteral("—");
        project.source.resolution =
            inputs.sourceWidth > 0 && inputs.sourceHeight > 0
            ? QStringLiteral("%1×%2")
                  .arg(inputs.sourceWidth)
                  .arg(inputs.sourceHeight)
            : QStringLiteral("—");
        // 旧历史没有剪辑计划时，尽量从结果标签「真人口播 · 02:48」里取时长。
        if (inputs.sourceDurationMs <= 0) {
            const QStringList parts =
                result.sourceLabel.split(QStringLiteral(" · "));
            if (parts.size() > 1 && !parts.constLast().isEmpty()) {
                project.source.duration = parts.constLast();
            }
        }
    }
    project.materials = inputs.materials;

    const bool hasInputs = project.hasSource && !project.materials.isEmpty();
    project.step = hasInputs ? 2 : 1;
    // 当前只开放清透讲解；历史里的其它模板重新包装时统一回到该模板。
    project.templateId = QStringLiteral("clean");
    project.ratio = result.ratio;
    project.outputBoth = result.outputBoth;
    m_project = project;
    showSetup();

    if (hasSource) {
        // 历史记录只保存了路径，重新探测一次分辨率与时长。
        probeSourceMetadata(inputs.sourcePath);
    }
    if (inputs.missingMaterials > 0) {
        if (inputs.materials.isEmpty()) {
            emit toastRequested(QStringLiteral(
                "%1 个原始素材文件已不存在，请重新添加").arg(
                inputs.missingMaterials));
        } else {
            emit toastRequested(
                QStringLiteral("已带入 %1 个素材；%2 个素材文件已不存在，已跳过")
                    .arg(inputs.materials.size())
                    .arg(inputs.missingMaterials));
        }
    } else if (hasInputs) {
        emit toastRequested(
            QStringLiteral("已带入原口播与 %1 个素材，可调整设置后重新包装")
                .arg(inputs.materials.size()));
    } else if (!inputs.materials.isEmpty()) {
        emit toastRequested(QStringLiteral(
            "口播原片文件已不存在，已带入 %1 个素材，请重新上传口播视频")
                                .arg(inputs.materials.size()));
    } else {
        emit toastRequested(QStringLiteral(
            "这条记录没有可复用的原始素材，请重新添加上传"));
    }
}

void OralPage::togglePlayback()
{
    if (!m_hasResult) {
        return;
    }
    if (!m_resultHasVideo || !m_resultPlayer) {
        emit toastRequested(QStringLiteral(
            "这条记录没有本地成片；重新包装生成后可播放真实成片"));
        return;
    }
    if (m_playing) {
        m_resultPlayer->pause();
        m_playing = false;
        m_playTimer->stop();
    } else {
        if (m_resultPlayer->duration() > 0
            && m_resultPlayer->position() >= m_resultPlayer->duration() - 50) {
            m_resultPlayer->setPosition(0);
        }
        // 切到播放器画面后再开始播放，避免海报覆盖视频首帧。
        if (m_resultMediaStack && m_resultMediaStack->count() > 1) {
            m_resultMediaStack->setCurrentIndex(1);
        }
        m_resultPlayer->play();
        m_playing = true;
        m_playTimer->start();
    }
    updatePlaybackControls();
    updatePlayback();
}

void OralPage::updatePlaybackControls()
{
    if (m_resultPlayButton) {
        m_resultPlayButton->setText(m_playing ? QStringLiteral("暂停播放")
                                              : QStringLiteral("播放成片"));
        m_resultPlayButton->setLeadingGlyph(m_playing ? Icons::Glyph::Pause
                                                      : Icons::Glyph::Play);
    }
    setResultPlayIcon(m_resultCenterPlay, m_playing);
    if (m_resultPlayToggle) {
        m_resultPlayToggle->setGlyph(m_playing ? Icons::Glyph::Pause
                                               : Icons::Glyph::Play);
    }
}

void OralPage::updatePlayback()
{
    if (!m_hasResult) {
        return;
    }
    if (m_resultHasVideo && m_resultPlayer) {
        const qint64 total = m_resultPlayer->duration();
        const qint64 current = qMax<qint64>(0, m_resultPlayer->position());
        if (m_playing && total > 0 && current >= total - 80) {
            // 定时器兜底：部分后端结束时不保证发出 EndOfMedia。
            finishPlayback();
            return;
        }
        m_playProgress = total > 0
            ? qBound(0, int(current * 100 / total), 100)
            : 0;
        if (m_resultPlayClock) {
            m_resultPlayClock->setText(StudioData::formatElapsed(current));
        }
    } else if (m_resultPlayClock) {
        m_resultPlayClock->setText(QStringLiteral("00:00"));
    }
    if (m_resultPoster) {
        m_resultPoster->setProgress(m_playProgress);
    }
    if (m_resultPlayBar) {
        m_resultPlayBar->setValue(m_playProgress);
    }
}

void OralPage::finishPlayback()
{
    m_playing = false;
    m_playProgress = 0;
    if (m_playTimer) {
        m_playTimer->stop();
    }
    // 已经在停止态时不要再调 stop()，避免结束信号再次触发本函数。
    if (m_resultPlayer
        && m_resultPlayer->playbackState() != QMediaPlayer::StoppedState) {
        m_resultPlayer->stop();
    }
    updatePlaybackControls();
    if (m_resultMediaStack) {
        m_resultMediaStack->setCurrentIndex(0);
    }
    if (m_resultPlayClock) {
        m_resultPlayClock->setText(QStringLiteral("00:00"));
    }
    if (m_resultPoster) {
        m_resultPoster->setProgress(0);
    }
    if (m_resultPlayBar) {
        m_resultPlayBar->setValue(0);
    }
}

void OralPage::resetResultPlayer()
{
    if (m_playTimer) {
        m_playTimer->stop();
    }
    m_playing = false;
    m_playProgress = 0;
    if (m_resultPlayer) {
        m_resultPlayer->stop();
        m_resultPlayer->setVideoOutput(nullptr);
        delete m_resultPlayer;
        m_resultPlayer = nullptr;
    }
    if (m_resultAudio) {
        delete m_resultAudio;
        m_resultAudio = nullptr;
    }
    if (m_resultVideoView) {
        delete m_resultVideoView;
        m_resultVideoView = nullptr;
    }
    m_resultVideoItem = nullptr;
    m_resultMediaStack = nullptr;
    m_resultHasVideo = false;
}

void OralPage::openResultVideoPreview()
{
    if (m_resultFilePath.isEmpty() || !QFileInfo::exists(m_resultFilePath)) {
        emit toastRequested(QStringLiteral("成片文件不存在，请重新生成"));
        return;
    }
    emit previewRequested(m_result.projectName, m_resultFilePath, true);
}

void OralPage::openDraftFolder()
{
    if (m_resultDraftPath.isEmpty() || !QFileInfo::exists(m_resultDraftPath)) {
        emit toastRequested(QStringLiteral("草稿目录不存在，请重新生成草稿"));
        return;
    }
    if (!QDesktopServices::openUrl(QUrl::fromLocalFile(m_resultDraftPath))) {
        emit toastRequested(QStringLiteral("无法打开草稿文件夹"));
    }
}

void OralPage::openExportFolder()
{
    if (m_resultFilePath.isEmpty() || !QFileInfo::exists(m_resultFilePath)) {
        emit toastRequested(QStringLiteral("成片文件不存在，请重新生成"));
        return;
    }
    const QString folder = QFileInfo(m_resultFilePath).absolutePath();
    if (QDesktopServices::openUrl(QUrl::fromLocalFile(folder))) {
        emit toastRequested(
            Theme::isWindowsWindowStyle()
                ? QStringLiteral("已在资源管理器中打开成片目录")
                : QStringLiteral("已在访达中打开成片目录"));
    } else {
        emit toastRequested(QStringLiteral("无法打开成片目录"));
    }
}

void OralPage::startDraftDownload()
{
    if (m_draftState == 1) {
        return;
    }
    if (!m_result.draftPath.isEmpty() && QFileInfo::exists(m_result.draftPath)) {
        m_draftState = 2;
        m_resultDraftPath = m_result.draftPath;
        openDraftFolder();
        return;
    }
    if (m_result.draftId.isEmpty()) {
        emit toastRequested(QStringLiteral(
            "这条结果没有可下载的剪映草稿，请重新包装并选择「成片 + 剪映草稿」"));
        return;
    }

    const QString movies = QStandardPaths::writableLocation(
        QStandardPaths::MoviesLocation);
    const QString draftRoot =
        QDir(movies).filePath(QStringLiteral("LiveMix/CapDraft"));
    m_draftState = 1;
    m_draftProgress = 0;
    if (m_resultDraftButton) {
        m_resultDraftButton->setText(QStringLiteral("0%"));
        m_resultDraftButton->setEnabled(false);
    }

    if (m_draftThread) {
        m_draftThread->quit();
        if (m_draftThread->wait(2000)) {
            m_draftThread->deleteLater();
        } else {
            // 线程仍在收尾时不能删除 QThread 对象，等它自然结束后回收。
            disconnect(m_draftThread, nullptr, this, nullptr);
        }
        m_draftThread = nullptr;
    }
    auto *task = new Oral::DraftDownloadTask(
        m_result.draftId, m_result.projectName, draftRoot);
    auto *thread = new QThread;
    task->moveToThread(thread);
    connect(thread, &QThread::started, task, &Oral::DraftDownloadTask::run);
    connect(task, &Oral::DraftDownloadTask::progress, this,
            &OralPage::onDraftProgress);
    connect(task, &Oral::DraftDownloadTask::finished, this,
            &OralPage::onDraftFinished);
    connect(thread, &QThread::finished, task, &QObject::deleteLater);
    connect(thread, &QThread::finished, thread, &QObject::deleteLater);
    m_draftTask = task;
    m_draftThread = thread;
    thread->start();
}

void OralPage::onDraftProgress(int percent, const QString &detail)
{
    m_draftProgress = qBound(0, percent, 100);
    if (m_resultDraftButton) {
        m_resultDraftButton->setText(
            QStringLiteral("%1%").arg(m_draftProgress));
    }
    if (m_resultDraftHint && !detail.isEmpty()) {
        m_resultDraftHint->setText(detail);
    }
}

void OralPage::onDraftFinished(bool success, const QString &localPath,
                               const QString &error)
{
    if (m_draftThread) {
        m_draftThread->quit();
        m_draftThread->wait(2000);
        m_draftThread = nullptr;
    }
    m_draftTask = nullptr;
    if (!success) {
        m_draftState = 0;
        if (m_resultDraftButton) {
            m_resultDraftButton->setText(QStringLiteral("生成草稿"));
            m_resultDraftButton->setLeadingGlyph(Icons::Glyph::Scissors);
            m_resultDraftButton->setEnabled(true);
        }
        if (m_resultDraftHint) {
            m_resultDraftHint->setText(QStringLiteral("生成可二次编辑的剪映草稿"));
        }
        emit toastRequested(QStringLiteral("剪映草稿下载失败：%1").arg(error));
        return;
    }
    m_draftState = 2;
    m_result.draftPath = localPath;
    m_result.draftGenerated = true;
    m_resultDraftPath = localPath;
    // 同步更新首页历史与持久化记录中的草稿路径，重启后仍可直接打开。
    for (StudioData::OralResult &item : m_persistedHistory) {
        if (item.id == m_result.id) {
            item.draftPath = localPath;
            item.draftGenerated = true;
            break;
        }
    }
    for (StudioData::OralResult &item : m_history) {
        if (item.id == m_result.id) {
            item.draftPath = localPath;
            item.draftGenerated = true;
            break;
        }
    }
    saveHistory();
    rebuildResult();
    emit toastRequested(
        QStringLiteral("剪映草稿已生成，可在剪映「本地草稿」中继续编辑"));
}

void OralPage::updateGenerationClock()
{
    if (!m_hasResult || !m_resultGenerationLabel) {
        return;
    }
    const QString label = StudioData::formatGenerationTime(m_result.generatedAt);
    m_resultGenerationLabel->setText(label);
    m_resultGenerationLabel->setStyleSheet(
        QStringLiteral("color:%1;background:transparent;")
            .arg((label.startsWith(QStringLiteral("刚刚")) ? Theme::Success
                                                           : Theme::Text1)
                     .name()));
    m_resultGenerationLabel->setToolTip(
        QStringLiteral("生成时间：%1")
            .arg(StudioData::formatGenerationFullTime(m_result.generatedAt)));
}

void OralPage::copyText(const QString &text, const QString &label)
{
    QGuiApplication::clipboard()->setText(text);
    emit toastRequested(QStringLiteral("已复制%1").arg(label));
}
