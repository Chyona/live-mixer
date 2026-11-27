#pragma once

// 智能口播 / 智能混剪的抽象画面素材与成片海报。
// 原型中的 SVG 插画在 Qt 端用 QPainter 重绘，保留同样的构图、配色与信息层级。

#include "../data/StudioData.h"
#include "Controls.h"

#include <QColor>
#include <QRectF>
#include <QString>
#include <QStringList>
#include <QVector>
#include <QWidget>

class QPainter;
class QTimer;

namespace Studio {

struct OralPosterSpec {
    QString templateId = QStringLiteral("clean");
    QString headline;
    QString caption;
    QStringList keywords;
    QVector<StudioData::OralMaterial> materials;
    QString ratio = QStringLiteral("9:16");
    QString timecode = QStringLiteral("00:58");
    QString badge;
    int progress = -1;
    bool showPlay = false;
    StudioData::OralSubtitleStyle subtitleStyle = StudioData::OralSubtitleStyle::Keyword;
    bool dimmed = false;
};

struct MixPosterSpec {
    QString scene = QStringLiteral("street");
    QString title;
    QString caption;
    QString highlight;
    QString voiceName;
    QStringList secondaryScenes;
    QString ratio = QStringLiteral("9:16");
    QString timecode = QStringLiteral("00:00");
    QString badge = QStringLiteral("智能混剪");
    int progress = -1;
    bool showPlay = false;
    bool dimmed = false;
};

void paintOralPoster(QPainter &painter, const QRectF &rect,
                     const OralPosterSpec &spec);
void paintMixPoster(QPainter &painter, const QRectF &rect,
                    const MixPosterSpec &spec);
void paintMixScene(QPainter &painter, const QRectF &rect, const QString &scene);
void paintOralMaterial(QPainter &painter, const QRectF &rect,
                       const StudioData::OralMaterial &material);
void paintPortrait(QPainter &painter, const QRectF &rect);
void paintStudioScene(QPainter &painter, const QRectF &rect);

class OralPosterWidget : public QWidget
{
public:
    explicit OralPosterWidget(QWidget *parent = nullptr);

    void setSpec(const OralPosterSpec &spec);
    void setProgress(int progress);
    void setDimmed(bool dimmed);
    void setShowPlay(bool showPlay);
    OralPosterSpec &spec() { return m_spec; }
    const OralPosterSpec &spec() const { return m_spec; }

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    OralPosterSpec m_spec;
};

class MixPosterWidget : public QWidget
{
public:
    explicit MixPosterWidget(QWidget *parent = nullptr);

    void setSpec(const MixPosterSpec &spec);
    void setProgress(int progress);
    void setDimmed(bool dimmed);
    void setShowPlay(bool showPlay);
    MixPosterSpec &spec() { return m_spec; }
    const MixPosterSpec &spec() const { return m_spec; }

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    MixPosterSpec m_spec;
};

class MixSceneWidget : public QWidget
{
public:
    explicit MixSceneWidget(QWidget *parent = nullptr);

    void setScene(const QString &scene);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    QString m_scene = QStringLiteral("street");
};

class MaterialVisualWidget : public QWidget
{
public:
    explicit MaterialVisualWidget(QWidget *parent = nullptr);

    void setMaterial(const StudioData::OralMaterial &material);

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    StudioData::OralMaterial m_material;
};

class StudioSceneWidget : public QWidget
{
public:
    explicit StudioSceneWidget(QWidget *parent = nullptr);

protected:
    void paintEvent(QPaintEvent *event) override;
};

class Waveform : public QWidget
{
public:
    explicit Waveform(QWidget *parent = nullptr);

    void setBars(int bars);
    void setColor(const QColor &color);
    void setProgress(int progress);
    void setAnimated(bool animated);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    int m_bars = 18;
    QColor m_color = Theme::Text3;
    int m_progress = -1;
    bool m_animated = false;
    int m_phase = 0;
    QTimer *m_timer = nullptr;
};

class VoiceOrbWidget : public QWidget
{
public:
    VoiceOrbWidget(const StudioData::MixcutVoice &voice, int size,
                   QWidget *parent = nullptr);

    void setVoice(const StudioData::MixcutVoice &voice);
    QSize sizeHint() const override;

protected:
    void paintEvent(QPaintEvent *event) override;

private:
    StudioData::MixcutVoice m_voice;
    int m_size = 28;
};

} // namespace Studio
