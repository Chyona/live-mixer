#include "StudioPosters.h"

#include <QFontMetricsF>
#include <QLinearGradient>
#include <QPainter>
#include <QPainterPath>
#include <QPolygonF>
#include <QRadialGradient>
#include <QTimer>

#include <algorithm>
#include <cmath>

namespace Studio {
namespace {

struct StyledChar {
    QChar ch;
    QColor color;
};

struct DrawLine {
    QVector<StyledChar> chars;

    QString text() const
    {
        QString result;
        result.reserve(chars.size());
        for (const StyledChar &item : chars) {
            result.append(item.ch);
        }
        return result;
    }

    qreal width(const QFontMetricsF &metrics) const
    {
        return metrics.horizontalAdvance(text());
    }
};

QVector<QColor> colorsForText(const QString &text, const QString &highlight,
                              const QColor &base, const QColor &emphasis)
{
    QVector<QColor> colors(text.size(), base);
    if (highlight.isEmpty()) {
        return colors;
    }

    int index = text.indexOf(highlight);
    while (index >= 0) {
        for (int position = index; position < index + highlight.size()
                                       && position < colors.size();
             ++position) {
            colors[position] = emphasis;
        }
        index = text.indexOf(highlight, index + qMax(1, highlight.size()));
    }
    return colors;
}

QVector<DrawLine> layoutText(const QString &text, const QString &highlight,
                             const QColor &base, const QColor &emphasis,
                             const QFont &font, qreal maxWidth, int maxLines,
                             bool *truncated = nullptr)
{
    if (truncated) {
        *truncated = false;
    }

    const QFontMetricsF metrics(font);
    const QVector<QColor> colors = colorsForText(text, highlight, base, emphasis);
    QVector<DrawLine> lines;
    DrawLine current;
    QString currentText;

    auto flush = [&] {
        while (!current.chars.isEmpty()
               && current.chars.last().ch.isSpace()
               && lines.isEmpty() && currentText.trimmed().isEmpty()) {
            current.chars.clear();
        }
        if (!current.chars.isEmpty()) {
            lines.append(current);
        }
        current.chars.clear();
        currentText.clear();
    };

    for (int index = 0; index < text.size(); ++index) {
        const QChar ch = text.at(index);
        if (ch == QLatin1Char('\n')) {
            flush();
            continue;
        }

        const QString candidate = currentText + ch;
        if (!current.chars.isEmpty() && metrics.horizontalAdvance(candidate) > maxWidth) {
            flush();
        }
        current.chars.append({ch, colors.value(index, base)});
        currentText = current.text();
    }
    flush();

    if (lines.isEmpty() && !text.isEmpty()) {
        DrawLine line;
        line.chars.append({text.at(0), base});
        lines.append(line);
    }

    if (maxLines > 0 && lines.size() > maxLines) {
        if (truncated) {
            *truncated = true;
        }
        lines.resize(maxLines);
        DrawLine &last = lines.last();
        const QString ellipsis = QStringLiteral("…");
        while (!last.chars.isEmpty()
               && metrics.horizontalAdvance(last.text() + ellipsis) > maxWidth) {
            last.chars.removeLast();
        }
        last.chars.append({ellipsis.at(0), last.chars.isEmpty() ? base
                                                               : last.chars.last().color});
    }

    return lines;
}

qreal drawLines(QPainter &painter, const QPointF &topLeft,
                const QVector<DrawLine> &lines, const QFont &font,
                const QColor &fallback, qreal lineHeight)
{
    painter.setFont(font);
    const QFontMetricsF metrics(font);
    qreal y = topLeft.y() + metrics.ascent();
    for (const DrawLine &line : lines) {
        qreal x = topLeft.x();
        for (const StyledChar &item : line.chars) {
            painter.setPen(item.color.isValid() ? item.color : fallback);
            const QString text(item.ch);
            painter.drawText(QPointF(x, y), text);
            x += metrics.horizontalAdvance(text);
        }
        y += lineHeight;
    }
    return qMax(0.0, y - topLeft.y() - metrics.descent());
}

qreal lineHeightFor(const QFont &font, qreal factor = 1.25)
{
    return QFontMetricsF(font).height() * factor;
}

void paintWaveBars(QPainter &painter, const QRectF &rect, int bars,
                   const QColor &color, int progress = -1, int phase = 0)
{
    if (bars <= 0 || rect.height() <= 0) {
        return;
    }
    const qreal gap = 2.0;
    const qreal barWidth = qMax<qreal>(1.0, (rect.width() - gap * (bars - 1))
                                                 / qMax(1, bars));
    painter.setPen(Qt::NoPen);
    for (int index = 0; index < bars; ++index) {
        int height = 22 + (index * 37) % 66;
        if (phase != 0) {
            height = qBound(15, height + static_cast<int>(
                                        std::sin((index + phase) * 0.55) * 18),
                            96);
        }
        const qreal heightPx = rect.height() * height / 100.0;
        const qreal x = rect.left() + index * (barWidth + gap);
        const qreal y = rect.center().y() - heightPx / 2.0;
        QColor use = color;
        if (progress >= 0) {
            const double ratio = (static_cast<double>(index) / qMax(1, bars - 1)) * 100.0;
            use.setAlphaF(ratio <= progress ? 0.95 : 0.24);
        } else {
            use.setAlphaF(0.86);
        }
        painter.setBrush(use);
        painter.drawRoundedRect(QRectF(x, y, barWidth, heightPx), barWidth / 2.0,
                                barWidth / 2.0);
    }
}

void paintPill(QPainter &painter, const QRectF &rect, const QString &text,
               const QColor &dotColor, const QFont &font)
{
    const QFontMetricsF metrics(font);
    painter.setFont(font);
    const qreal textWidth = metrics.horizontalAdvance(text);
    const qreal dot = qMax<qreal>(3.0, metrics.height() * 0.18);
    const qreal paddingX = metrics.height() * 0.55;
    QRectF pill = rect;
    pill.setWidth(qMin(rect.width(), textWidth + dot + paddingX * 2 + 5.0));
    if (pill.width() < 4) {
        return;
    }
    painter.setPen(QPen(QColor(255, 255, 255, 51), 1.0));
    painter.setBrush(QColor(0, 0, 0, 77));
    painter.drawRoundedRect(pill, pill.height() / 2.0, pill.height() / 2.0);
    painter.setPen(Qt::NoPen);
    painter.setBrush(dotColor);
    painter.drawEllipse(QPointF(pill.left() + paddingX + dot / 2.0, pill.center().y()),
                        dot / 2.0, dot / 2.0);
    painter.setPen(QColor(255, 255, 255, 204));
    painter.drawText(QRectF(pill.left() + paddingX + dot + 4.0, pill.top(),
                            pill.width() - paddingX - dot - 4.0, pill.height()),
                     Qt::AlignLeft | Qt::AlignVCenter, text);
}

void clipRounded(QPainter &painter, const QRectF &rect, qreal radius)
{
    QPainterPath path;
    path.addRoundedRect(rect, radius, radius);
    painter.setClipPath(path);
}

// 按目标区域等比铺满并居中裁剪绘制图片。
void paintCoverImage(QPainter &painter, const QRectF &rect, const QImage &image)
{
    if (image.isNull() || rect.width() < 2 || rect.height() < 2) {
        return;
    }
    painter.setRenderHint(QPainter::SmoothPixmapTransform, true);
    const QSize imageSize = image.size();
    const qreal imageAspect = qreal(imageSize.width()) / imageSize.height();
    const qreal rectAspect = rect.width() / rect.height();
    QRectF source(0, 0, imageSize.width(), imageSize.height());
    if (imageAspect > rectAspect) {
        const qreal width = imageSize.height() * rectAspect;
        source = QRectF((imageSize.width() - width) / 2.0, 0, width,
                        imageSize.height());
    } else {
        const qreal height = imageSize.width() / rectAspect;
        source = QRectF(0, (imageSize.height() - height) / 2.0,
                        imageSize.width(), height);
    }
    painter.drawImage(rect, image, source);
}

QFont posterFont(StudioData::OralFont family, int pixelSize, int weight)
{
    QFont font = Theme::font(pixelSize, weight);
    if (family == StudioData::OralFont::Serif) {
        font.setFamilies({QStringLiteral("Songti SC"), QStringLiteral("Noto Serif SC"),
                          QStringLiteral("Georgia"), font.family()});
    } else if (family == StudioData::OralFont::Mono) {
        font = Theme::monoFont(pixelSize, weight);
    }
    return font;
}

} // namespace

/* ------------------------------------------------------------------ */
/* 口播海报                                                             */
/* ------------------------------------------------------------------ */

void paintOralPoster(QPainter &painter, const QRectF &rect,
                     const OralPosterSpec &spec)
{
    if (rect.width() < 4 || rect.height() < 4) {
        return;
    }

    painter.save();
    painter.setRenderHint(QPainter::Antialiasing, true);
    clipRounded(painter, rect, 10);

    // 有真实成片首帧时只绘制视频画面，避免原型海报的标题/关键词叠在封面上。
    if (!spec.frameImage.isNull()) {
        paintCoverImage(painter, rect, spec.frameImage);
        painter.restore();
        return;
    }

    const StudioData::OralTemplate tmpl = StudioData::oralTemplate(spec.templateId);
    if (spec.ratio == QLatin1String("16:9")) {
        paintStudioScene(painter, rect);
    } else {
        paintPortrait(painter, rect);
    }

    QLinearGradient shade(rect.topLeft(), rect.bottomLeft());
    shade.setColorAt(0.0, QColor(4, 5, 9, 117));
    shade.setColorAt(0.32, QColor(4, 5, 9, 0));
    shade.setColorAt(0.54, QColor(4, 5, 9, 0));
    shade.setColorAt(1.0, QColor(4, 5, 9, 194));
    painter.fillRect(rect, shade);

    if (tmpl.layout == StudioData::OralLayout::Tech) {
        painter.setPen(QPen(QColor(85, 214, 224, 18), 1.0));
        const qreal step = qMax<qreal>(12.0, rect.width() / 14.0);
        for (qreal x = rect.left(); x < rect.right(); x += step) {
            painter.drawLine(QPointF(x, rect.top()), QPointF(x, rect.bottom()));
        }
        for (qreal y = rect.top(); y < rect.bottom(); y += step) {
            painter.drawLine(QPointF(rect.left(), y), QPointF(rect.right(), y));
        }
        painter.setPen(QPen(QColor(85, 214, 224, 128), 1.0));
        painter.drawLine(QPointF(rect.left() + 8, rect.top() + 8),
                         QPointF(rect.left() + 22, rect.top() + 8));
        painter.drawLine(QPointF(rect.left() + 8, rect.top() + 8),
                         QPointF(rect.left() + 8, rect.top() + 22));
    } else if (tmpl.layout == StudioData::OralLayout::Neon) {
        QRadialGradient pink(rect.right() - 20, rect.top() + 20, rect.width() * 0.55);
        pink.setColorAt(0, QColor(255, 91, 181, 64));
        pink.setColorAt(1, QColor(255, 91, 181, 0));
        painter.fillRect(rect, pink);
        QRadialGradient blue(rect.left() + 10, rect.top() + rect.height() * 0.32,
                             rect.width() * 0.45);
        blue.setColorAt(0, QColor(110, 139, 255, 51));
        blue.setColorAt(1, QColor(110, 139, 255, 0));
        painter.fillRect(rect, blue);
    } else if (tmpl.layout == StudioData::OralLayout::Magazine) {
        QLinearGradient top(rect.topLeft(), QPointF(rect.left(),
                                                    rect.top() + rect.height() / 2));
        top.setColorAt(0, QColor(232, 213, 169, 31));
        top.setColorAt(1, QColor(232, 213, 169, 0));
        painter.fillRect(rect, top);
    }

    const bool small = rect.height() < 160;
    const qreal pad = small ? 7 : 11;
    const qreal chip = small ? 5.5 : 7;
    const qreal titleSize = small ? 12 : 19;
    const qreal captionSize = small ? 8 : 10.5;
    const qreal keywordSize = small ? 5.8 : 7.5;
    const qreal brollSize = small ? 36 : 52;
    const qreal gap = small ? 5 : 7;

    const QString badge = spec.badge.isEmpty() ? tmpl.name : spec.badge;
    const QFont chipFont = Theme::font(qMax(4, qRound(chip)), QFont::Medium);
    const qreal pillHeight = chip * 2.4;
    paintPill(painter, QRectF(rect.left() + pad, rect.top() + pad,
                              rect.width() - pad * 2, pillHeight),
              badge, tmpl.accent, chipFont);

    const QString title = spec.headline.isEmpty() ? tmpl.headline : spec.headline;
    QFont titleFont = posterFont(tmpl.font, qMax(8, qRound(titleSize)), QFont::Bold);
    if (tmpl.layout == StudioData::OralLayout::Magazine) {
        titleFont.setLetterSpacing(QFont::PercentageSpacing, 112);
        titleFont.setCapitalization(QFont::AllUppercase);
    }
    const qreal titleTop = rect.top() + pad + pillHeight + gap + 2;
    const qreal titleMaxWidth = rect.width() - pad * 2;
    const QVector<DrawLine> titleLines = layoutText(
        title, QString(), QColor(255, 255, 255),
        tmpl.layout == StudioData::OralLayout::Magazine
            ? QColor(242, 231, 207)
            : QColor(255, 255, 255),
        titleFont, qMax<qreal>(20, titleMaxWidth), small ? 2 : 3);
    const QColor titleColor = tmpl.layout == StudioData::OralLayout::Magazine
        ? QColor(242, 231, 207)
        : QColor(255, 255, 255);
    if (tmpl.layout == StudioData::OralLayout::Punch) {
        const QFontMetricsF metrics(titleFont);
        qreal y = titleTop;
        for (const DrawLine &line : titleLines) {
            const QString text = line.text();
            const qreal width = metrics.horizontalAdvance(text);
            const QRectF box(rect.left() + pad, y - metrics.ascent() * 0.15,
                             width + pad * 0.7, metrics.height() + 2);
            painter.save();
            painter.translate(box.center());
            painter.rotate(-1.5);
            painter.setPen(Qt::NoPen);
            painter.setBrush(tmpl.accent);
            painter.drawRect(QRectF(-box.width() / 2.0, -box.height() / 2.0,
                                    box.width(), box.height()));
            painter.setFont(titleFont);
            painter.setPen(QColor(26, 18, 5));
            painter.drawText(QRectF(-width / 2.0, -box.height() / 2.0, width,
                                    box.height()),
                             Qt::AlignLeft | Qt::AlignVCenter, text);
            painter.restore();
            y += lineHeightFor(titleFont, 1.18);
        }
    } else {
        drawLines(painter, QPointF(rect.left() + pad, titleTop), titleLines, titleFont,
                  titleColor, lineHeightFor(titleFont, 1.22));
    }

    if (!spec.materials.isEmpty()) {
        const qreal thumbsTop = titleTop + titleSize * 1.35 + 10;
        const QVector<StudioData::OralMaterial> materials =
            spec.materials.mid(0, 2);
        qreal y = thumbsTop;
        for (int index = 0; index < materials.size(); ++index) {
            const qreal height = brollSize * (index % 2 == 0 ? 1.2 : 0.86);
            QRectF thumb(rect.right() - pad - brollSize, y, brollSize, height);
            painter.save();
            painter.translate(thumb.center());
            painter.rotate(index % 2 == 0 ? 2.5 : -2.0);
            QRectF rotated(-thumb.width() / 2.0, -thumb.height() / 2.0, thumb.width(),
                           thumb.height());
            painter.setClipRect(rotated);
            paintOralMaterial(painter, rotated, materials.at(index));
            painter.setClipping(false);
            painter.setPen(QPen(QColor(255, 255, 255, 64), 1.0));
            painter.setBrush(Qt::NoBrush);
            painter.drawRect(rotated);
            painter.restore();
            y += height + gap;
        }
    }

    const qreal bottomLineHeight = qMax<qreal>(12, chip * 2.2);
    qreal cursorY = rect.bottom() - pad - bottomLineHeight;

    const int maxKeywords = spec.subtitleStyle == StudioData::OralSubtitleStyle::Big ? 2
        : (spec.subtitleStyle == StudioData::OralSubtitleStyle::Minimal ? 0 : 3);
    const QStringList keywords = spec.keywords.mid(0, maxKeywords);
    if (!keywords.isEmpty()) {
        const QFont keywordFont = Theme::font(qMax(4, qRound(keywordSize)),
                                               QFont::Medium);
        const QFontMetricsF keywordMetrics(keywordFont);
        qreal keywordHeight = keywordSize * 1.8;
        cursorY -= keywordHeight;
        qreal x = rect.left() + pad;
        painter.setFont(keywordFont);
        for (const QString &keyword : keywords) {
            const QString text = QStringLiteral("#") + keyword;
            const qreal width = keywordMetrics.horizontalAdvance(text) + keywordSize * 1.1;
            if (x + width > rect.right() - pad) {
                break;
            }
            const QRectF chipRect(x, cursorY, width, keywordHeight);
            painter.setPen(QPen(QColor(255, 255, 255, 51), 1.0));
            painter.setBrush(QColor(0, 0, 0, 90));
            painter.drawRoundedRect(chipRect, 3, 3);
            painter.setPen(QColor(255, 255, 255, 217));
            painter.drawText(chipRect, Qt::AlignCenter, text);
            x += width + 4;
        }
        cursorY -= gap;
    }

    if (!spec.caption.isEmpty()) {
        const QFont captionFont = posterFont(
            tmpl.font,
            qMax(6, qRound(spec.subtitleStyle == StudioData::OralSubtitleStyle::Big
                               ? captionSize * 1.18
                               : captionSize)),
            QFont::DemiBold);
        const qreal boxWidth = rect.width() - pad * 2;
        QVector<DrawLine> captionLines = layoutText(
            spec.caption, QString(), QColor(255, 255, 255), QColor(255, 255, 255),
            captionFont, qMax<qreal>(20, boxWidth - captionSize * 1.2),
            small ? 2 : 3);
        const qreal lineHeight = lineHeightFor(captionFont, 1.35);
        const qreal textHeight = captionLines.size() * lineHeight;
        const qreal paddingX = qMax<qreal>(4, captionSize * 0.6);
        const qreal paddingY = qMax<qreal>(2, captionSize * 0.38);
        qreal maxLineWidth = 0;
        const QFontMetricsF captionMetrics(captionFont);
        for (const DrawLine &line : captionLines) {
            maxLineWidth = qMax(maxLineWidth, line.width(captionMetrics));
        }
        const QRectF box(rect.left() + pad, cursorY - textHeight - paddingY * 2,
                         qMin(boxWidth, maxLineWidth + paddingX * 2),
                         textHeight + paddingY * 2);
        painter.setPen(Qt::NoPen);
        QColor boxFill = QColor(7, 16, 24, 183);
        QColor textColor(255, 255, 255);
        if (tmpl.layout == StudioData::OralLayout::Punch) {
            boxFill = tmpl.accent;
            textColor = QColor(20, 13, 4);
        } else if (tmpl.layout == StudioData::OralLayout::Vlog) {
            boxFill = QColor(255, 247, 234, 235);
            textColor = QColor(59, 36, 23);
        } else if (tmpl.layout == StudioData::OralLayout::Tech) {
            boxFill = QColor(6, 19, 24, 199);
            textColor = QColor(223, 252, 255);
        } else if (tmpl.layout == StudioData::OralLayout::Magazine) {
            boxFill = QColor(12, 10, 7, 92);
            textColor = QColor(244, 234, 215);
        } else if (tmpl.layout == StudioData::OralLayout::Neon) {
            boxFill = QColor(28, 8, 38, 199);
            textColor = QColor(255, 255, 255);
        }
        painter.setBrush(boxFill);
        painter.drawRoundedRect(box, qMax<qreal>(2, captionSize * 0.28),
                                qMax<qreal>(2, captionSize * 0.28));
        if (tmpl.layout == StudioData::OralLayout::Clean) {
            painter.setPen(Qt::NoPen);
            painter.setBrush(tmpl.accent);
            painter.drawRect(QRectF(box.left(), box.top(), 2.0, box.height()));
        } else if (!small
                   && (tmpl.layout == StudioData::OralLayout::Tech
                       || tmpl.layout == StudioData::OralLayout::Magazine
                       || tmpl.layout == StudioData::OralLayout::Neon)) {
            painter.setPen(QPen(Theme::alpha(tmpl.accent, 0.55), 1.0));
            painter.setBrush(Qt::NoBrush);
            painter.drawLine(box.topLeft(), box.topRight());
        }
        for (DrawLine &line : captionLines) {
            for (StyledChar &item : line.chars) {
                item.color = textColor;
            }
        }
        if (tmpl.layout == StudioData::OralLayout::Tech) {
            painter.setFont(captionFont);
            painter.setPen(textColor);
            painter.drawText(
                QRectF(box.left() + paddingX + 2, box.top() + paddingY,
                       box.width(), box.height()),
                Qt::AlignLeft | Qt::AlignVCenter, QStringLiteral("›"));
        }
        drawLines(painter,
                  QPointF(box.left() + paddingX + (small ? 0 : 2),
                          box.top() + paddingY),
                  captionLines, captionFont, textColor, lineHeight);
        cursorY = box.top() - gap;
    }

    const QColor waveColor = tmpl.accent;
    paintWaveBars(painter,
                  QRectF(rect.left() + pad, rect.bottom() - pad - bottomLineHeight,
                         qMin<qreal>(rect.width() * 0.22, 46), bottomLineHeight),
                  small ? 8 : 14, waveColor);
    painter.setFont(Theme::monoFont(qMax(4, qRound(chip)), QFont::Normal));
    painter.setPen(QColor(255, 255, 255, 140));
    painter.drawText(QRectF(rect.left() + pad, rect.bottom() - pad - bottomLineHeight,
                            rect.width() - pad * 2, bottomLineHeight),
                     Qt::AlignRight | Qt::AlignVCenter, spec.timecode);

    if (spec.showPlay) {
        const qreal diameter = qMin<qreal>(48, rect.height() * 0.12);
        const QRectF circle(rect.center().x() - diameter / 2.0,
                            rect.center().y() - diameter / 2.0, diameter, diameter);
        painter.setPen(QPen(QColor(255, 255, 255, 76), 1.0));
        painter.setBrush(QColor(0, 0, 0, 110));
        painter.drawEllipse(circle);
        QPainterPath play;
        play.moveTo(circle.center().x() - diameter * 0.11,
                    circle.center().y() - diameter * 0.16);
        play.lineTo(circle.center().x() + diameter * 0.17, circle.center().y());
        play.lineTo(circle.center().x() - diameter * 0.11,
                    circle.center().y() + diameter * 0.16);
        play.closeSubpath();
        painter.setBrush(QColor(255, 255, 255, 230));
        painter.setPen(Qt::NoPen);
        painter.drawPath(play);
    }

    if (spec.progress >= 0) {
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 77));
        painter.drawRect(QRectF(rect.left(), rect.bottom() - 3, rect.width(), 3));
        painter.setBrush(tmpl.accent);
        painter.drawRect(QRectF(rect.left(), rect.bottom() - 3,
                                rect.width() * qBound(0, spec.progress, 100) / 100.0, 3));
    }

    if (spec.dimmed) {
        painter.fillRect(rect, QColor(5, 6, 10, 122));
    }

    painter.restore();
}

/* ------------------------------------------------------------------ */
/* 混剪海报                                                             */
/* ------------------------------------------------------------------ */

void paintMixPoster(QPainter &painter, const QRectF &rect,
                    const MixPosterSpec &spec)
{
    if (rect.width() < 4 || rect.height() < 4) {
        return;
    }

    painter.save();
    painter.setRenderHint(QPainter::Antialiasing, true);
    clipRounded(painter, rect, 10);
    if (!spec.frameImage.isNull()) {
        paintCoverImage(painter, rect, spec.frameImage);
    } else {
        paintMixScene(painter, rect, spec.scene);
    }

    QLinearGradient shade(rect.topLeft(), rect.bottomLeft());
    shade.setColorAt(0.0, QColor(4, 5, 9, 158));
    shade.setColorAt(0.30, QColor(4, 5, 9, 20));
    shade.setColorAt(0.52, QColor(4, 5, 9, 0));
    shade.setColorAt(1.0, QColor(4, 5, 9, 214));
    painter.fillRect(rect, shade);

    const bool small = rect.height() < 160;
    const qreal pad = small ? 8 : 12;
    const qreal chip = small ? 5.4 : 7;
    const qreal titleSize = small ? 11.5 : 19;
    const qreal captionSize = small ? 7.4 : 10.5;
    const qreal metaSize = small ? 5.2 : 7;
    const qreal thumbSize = small ? 24 : 40;
    const qreal gap = small ? 5 : 7;

    const QFont chipFont = Theme::font(qMax(4, qRound(chip)), QFont::Medium);
    const qreal pillHeight = chip * 2.4;
    paintPill(painter, QRectF(rect.left() + pad, rect.top() + pad,
                              rect.width() - pad * 2, pillHeight),
              spec.badge, Theme::Ai, chipFont);

    const qreal accentY = rect.top() + pad + pillHeight + gap + 1;
    painter.setPen(Qt::NoPen);
    painter.setBrush(Theme::Ai);
    painter.drawRoundedRect(
        QRectF(rect.left() + pad, accentY, qMax<qreal>(10, titleSize * 0.8),
               qMax<qreal>(1.5, titleSize * 0.14)),
        1, 1);

    QFont titleFont = Theme::font(qMax(8, qRound(titleSize)), QFont::Bold);
    const qreal titleTop = accentY + titleSize * 0.4;
    const QVector<DrawLine> titleLines = layoutText(
        spec.title, QString(), QColor(255, 255, 255), QColor(255, 255, 255),
        titleFont,
        qMax<qreal>(20, rect.width() - pad * 2),
        small ? 2 : 3);
    drawLines(painter, QPointF(rect.left() + pad, titleTop), titleLines, titleFont,
              QColor(255, 255, 255), lineHeightFor(titleFont, 1.22));

    if (!spec.secondaryScenes.isEmpty() && !small) {
        qreal y = titleTop + titleSize * 1.45 + 12;
        for (int index = 0; index < qMin(2, spec.secondaryScenes.size()); ++index) {
            const qreal height = thumbSize * 1.28;
            QRectF thumb(rect.right() - pad - thumbSize, y, thumbSize, height);
            painter.save();
            painter.translate(thumb.center());
            painter.rotate(index % 2 == 0 ? 2.5 : -2.0);
            QRectF rotated(-thumb.width() / 2.0, -thumb.height() / 2.0, thumb.width(),
                           thumb.height());
            painter.setClipRect(rotated);
            paintMixScene(painter, rotated, spec.secondaryScenes.at(index));
            painter.setClipping(false);
            painter.setPen(QPen(QColor(255, 255, 255, 64), 1.0));
            painter.setBrush(Qt::NoBrush);
            painter.drawRect(rotated);
            painter.restore();
            y += height + gap;
        }
    }

    qreal bottom = rect.bottom() - pad;

    painter.setFont(Theme::monoFont(qMax(4, qRound(metaSize)), QFont::Normal));
    painter.setPen(QColor(255, 255, 255, 115));
    const qreal brandHeight = qMax<qreal>(10, metaSize * 1.7);
    const QRectF brandRect(rect.left() + pad, bottom - brandHeight,
                           rect.width() - pad * 2, brandHeight);
    painter.drawText(brandRect, Qt::AlignLeft | Qt::AlignVCenter,
                     QStringLiteral("LIVEMIX"));
    painter.setPen(QPen(QColor(255, 255, 255, 38), 1.0));
    painter.drawLine(QPointF(brandRect.left() + 52, brandRect.center().y()),
                     QPointF(brandRect.right() - 31, brandRect.center().y()));
    painter.setPen(QColor(255, 255, 255, 115));
    painter.drawText(brandRect, Qt::AlignRight | Qt::AlignVCenter, spec.timecode);
    bottom -= brandHeight + gap;

    const QFont captionFont = Theme::font(qMax(6, qRound(captionSize)), QFont::Normal);
    const qreal captionMaxWidth = rect.width() - pad * 2 - 8;
    const QVector<DrawLine> captionLines = layoutText(
        spec.caption, spec.highlight, QColor(255, 255, 255, 242),
        QColor(255, 194, 75), captionFont, qMax<qreal>(20, captionMaxWidth),
        small ? 2 : 3);
    const qreal captionLineHeight = lineHeightFor(captionFont, 1.45);
    const qreal captionTextHeight = captionLines.size() * captionLineHeight;
    const qreal paddingX = qMax<qreal>(4, captionSize * 0.55);
    const qreal paddingY = qMax<qreal>(2, captionSize * 0.36);
    const QRectF captionBox(rect.left() + pad, bottom - captionTextHeight - paddingY * 2,
                            rect.width() - pad * 2,
                            captionTextHeight + paddingY * 2);
    painter.setPen(QPen(QColor(255, 255, 255, 25), 1.0));
    painter.setBrush(QColor(8, 10, 16, 204));
    painter.drawRoundedRect(captionBox, 4, 4);
    painter.setPen(Qt::NoPen);
    painter.setBrush(Theme::Ai);
    painter.drawRoundedRect(
        QRectF(captionBox.left() + paddingX, captionBox.top() + paddingY,
               qMax<qreal>(1.5, captionSize * 0.16),
               captionTextHeight),
        1, 1);
    drawLines(painter,
              QPointF(captionBox.left() + paddingX + captionSize * 0.7,
                      captionBox.top() + paddingY),
              captionLines, captionFont, QColor(255, 255, 255, 242),
              captionLineHeight);
    bottom = captionBox.top() - gap;

    if (!spec.voiceName.isEmpty() && !small) {
        const QFont voiceFont = Theme::font(qMax(4, qRound(metaSize)),
                                            QFont::Normal);
        const QFontMetricsF voiceMetrics(voiceFont);
        const QString voiceText = QStringLiteral("AI 配音 · ") + spec.voiceName;
        const qreal width = voiceMetrics.horizontalAdvance(voiceText) + metaSize * 3.6;
        const qreal height = metaSize * 2.6;
        const QRectF voiceRect(rect.left() + pad, bottom - height, width, height);
        painter.setPen(QPen(QColor(255, 255, 255, 38), 1.0));
        painter.setBrush(QColor(0, 0, 0, 97));
        painter.drawRoundedRect(voiceRect, height / 2.0, height / 2.0);
        paintWaveBars(painter,
                      QRectF(voiceRect.left() + 5, voiceRect.top() + 3, 16,
                             voiceRect.height() - 6),
                      7, QColor(255, 194, 75));
        painter.setFont(voiceFont);
        painter.setPen(QColor(255, 255, 255, 209));
        painter.drawText(voiceRect.adjusted(25, 0, -4, 0),
                         Qt::AlignLeft | Qt::AlignVCenter, voiceText);
    }

    if (spec.showPlay) {
        const qreal diameter = qMin<qreal>(44, rect.height() * 0.11);
        const QRectF circle(rect.center().x() - diameter / 2.0,
                            rect.center().y() - diameter / 2.0, diameter, diameter);
        painter.setPen(QPen(QColor(255, 255, 255, 76), 1.0));
        painter.setBrush(QColor(0, 0, 0, 110));
        painter.drawEllipse(circle);
        QPainterPath play;
        play.moveTo(circle.center().x() - diameter * 0.11,
                    circle.center().y() - diameter * 0.16);
        play.lineTo(circle.center().x() + diameter * 0.17, circle.center().y());
        play.lineTo(circle.center().x() - diameter * 0.11,
                    circle.center().y() + diameter * 0.16);
        play.closeSubpath();
        painter.setBrush(QColor(255, 255, 255, 230));
        painter.setPen(Qt::NoPen);
        painter.drawPath(play);
    }

    if (spec.dimmed) {
        painter.fillRect(rect, QColor(5, 6, 10, 122));
    }

    if (spec.progress >= 0) {
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 77));
        painter.drawRect(QRectF(rect.left(), rect.bottom() - 3, rect.width(), 3));
        painter.setBrush(Theme::Ai);
        painter.drawRect(QRectF(rect.left(), rect.bottom() - 3,
                                rect.width() * qBound(0, spec.progress, 100) / 100.0, 3));
    }

    painter.restore();
}

/* ------------------------------------------------------------------ */
/* 场景插画                                                             */
/* ------------------------------------------------------------------ */

void paintMixScene(QPainter &painter, const QRectF &rect, const QString &scene)
{
    if (rect.width() < 2 || rect.height() < 2) {
        return;
    }
    painter.save();
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setClipRect(rect);
    const qreal scale = qMax(rect.width() / 300.0, rect.height() / 400.0);
    painter.translate(rect.center());
    painter.scale(scale, scale);
    painter.translate(-150, -200);

    const QPair<QColor, QColor> palette = StudioData::mixcutScenePalette(scene);
    const QColor accent = StudioData::mixcutSceneAccent(scene);

    QLinearGradient background(0, 0, 300, 400);
    background.setColorAt(0, palette.first);
    background.setColorAt(1, palette.second);
    painter.fillRect(QRectF(0, 0, 300, 400), background);

    QRadialGradient glow(165, 112, 248);
    QColor glowColor = accent;
    glowColor.setAlphaF(0.42);
    glow.setColorAt(0, glowColor);
    glowColor.setAlphaF(0);
    glow.setColorAt(1, glowColor);
    painter.fillRect(QRectF(0, 0, 300, 400), glow);

    painter.setPen(Qt::NoPen);
    if (scene == QLatin1String("street")) {
        painter.setBrush(QColor(255, 211, 153, 235));
        painter.drawEllipse(QPointF(212, 112), 38, 38);
        const struct {
            qreal x, y, w, h;
            QColor color;
        } buildings[] = {
            {18, 208, 52, 150, QColor(15, 34, 51, 235)},
            {76, 172, 44, 186, QColor(11, 27, 41, 242)},
            {126, 222, 38, 136, QColor(18, 39, 56, 230)},
            {170, 188, 48, 170, QColor(12, 29, 44, 242)},
            {224, 216, 58, 142, QColor(15, 33, 50, 230)},
        };
        for (const auto &building : buildings) {
            painter.setBrush(building.color);
            painter.drawRect(QRectF(building.x, building.y, building.w, building.h));
        }
        painter.setBrush(QColor(255, 208, 138, 128));
        for (int row = 0; row < 4; ++row) {
            for (int column = 0; column < 4; ++column) {
                painter.drawRect(QRectF(28 + column * 62, 226 + row * 20, 6, 4));
            }
        }
        painter.setBrush(QColor(8, 18, 28));
        painter.drawRect(QRectF(0, 322, 300, 78));
        painter.setPen(QPen(QColor(255, 193, 119, 41), 2));
        painter.drawLine(QPointF(0, 336), QPointF(300, 336));
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(6, 11, 18));
        painter.drawEllipse(QPointF(132, 266), 8, 8);
        QPainterPath person;
        person.moveTo(124, 279);
        person.cubicTo(116, 297, 115, 313, 116, 326);
        person.lineTo(150, 326);
        person.cubicTo(151, 309, 147, 293, 141, 279);
        person.closeSubpath();
        painter.drawPath(person);
        painter.setPen(QPen(QColor(6, 11, 18), 6, Qt::SolidLine, Qt::RoundCap));
        painter.drawLine(QPointF(124, 294), QPointF(112, 322));
        painter.drawLine(QPointF(142, 294), QPointF(153, 320));
        painter.setPen(Qt::NoPen);
    } else if (scene == QLatin1String("serum")) {
        painter.setBrush(QColor(0, 0, 0, 92));
        painter.drawEllipse(QRectF(54, 316, 192, 36));
        QLinearGradient glass(101, 142, 199, 324);
        glass.setColorAt(0, QColor(255, 255, 255, 245));
        glass.setColorAt(1, QColor(244, 229, 210, 163));
        painter.setBrush(glass);
        painter.drawRoundedRect(QRectF(101, 142, 98, 182), 24, 24);
        painter.setBrush(QColor(42, 32, 24));
        painter.drawRoundedRect(QRectF(120, 118, 60, 34), 9, 9);
        painter.drawRoundedRect(QRectF(146, 99, 8, 22), 4, 4);
        painter.setBrush(QColor(241, 225, 202, 242));
        painter.drawRoundedRect(QRectF(116, 196, 68, 76), 9, 9);
        painter.setBrush(QColor(122, 91, 60, 158));
        painter.drawRoundedRect(QRectF(126, 212, 48, 3), 1.5, 1.5);
        painter.drawRoundedRect(QRectF(126, 222, 34, 2), 1, 1);
        painter.setBrush(QColor(255, 255, 255, 92));
        painter.drawRoundedRect(QRectF(114, 164, 12, 130), 6, 6);
        painter.setBrush(accent);
        painter.setOpacity(0.36);
        painter.drawEllipse(QPointF(224, 252), 9, 9);
        painter.setOpacity(1);
    } else if (scene == QLatin1String("face")) {
        painter.setBrush(QColor(59, 42, 44));
        QPainterPath shoulders;
        shoulders.moveTo(56, 400);
        shoulders.cubicTo(63, 312, 100, 273, 150, 273);
        shoulders.cubicTo(200, 273, 237, 312, 244, 400);
        shoulders.closeSubpath();
        painter.drawPath(shoulders);
        QRadialGradient skin(116, 130, 220);
        skin.setColorAt(0, QColor(247, 214, 193));
        skin.setColorAt(1, QColor(220, 164, 140));
        painter.setBrush(skin);
        painter.drawEllipse(QRectF(74, 100, 152, 196));
        painter.setBrush(QColor(43, 32, 32, 235));
        QPainterPath hair;
        hair.moveTo(74, 198);
        hair.cubicTo(69, 100, 100, 59, 150, 59);
        hair.cubicTo(200, 59, 233, 100, 226, 198);
        hair.cubicTo(218, 151, 208, 123, 190, 113);
        hair.cubicTo(176, 105, 161, 104, 150, 109);
        hair.cubicTo(132, 117, 119, 117, 103, 112);
        hair.cubicTo(89, 142, 80, 160, 74, 198);
        hair.closeSubpath();
        painter.drawPath(hair);
        painter.setPen(QPen(QColor(92, 64, 56), 3, Qt::SolidLine, Qt::RoundCap));
        painter.drawLine(QPointF(109, 186), QPointF(137, 183));
        painter.drawLine(QPointF(164, 183), QPointF(192, 186));
        painter.drawLine(QPointF(111, 208), QPointF(136, 208));
        painter.drawLine(QPointF(165, 208), QPointF(190, 208));
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(255, 255, 255, 153));
        painter.drawEllipse(QPointF(117, 72), 3.6, 3.6);
    } else if (scene == QLatin1String("swatch")) {
        painter.setBrush(QColor(245, 230, 220, 242));
        painter.drawEllipse(QPointF(58, 261), 29, 17);
        painter.setBrush(QColor(255, 244, 233, 230));
        painter.drawEllipse(QPointF(86, 254), 27, 16);
        painter.setBrush(QColor(239, 216, 200, 240));
        painter.drawEllipse(QPointF(106, 267), 24, 14);
        painter.setBrush(QColor(255, 255, 255, 140));
        painter.drawEllipse(QPointF(57, 257), 16, 7);
        painter.drawEllipse(QPointF(85, 250), 13, 6);
    } else if (scene == QLatin1String("desk")) {
        painter.setBrush(QColor(199, 161, 201, 61));
        painter.drawRect(QRectF(0, 286, 300, 114));
        painter.setBrush(QColor(241, 232, 220));
        QPainterPath notebook;
        notebook.moveTo(68, 238);
        notebook.lineTo(170, 218);
        notebook.lineTo(196, 300);
        notebook.lineTo(92, 320);
        notebook.closeSubpath();
        painter.drawPath(notebook);
        painter.setBrush(QColor(199, 161, 201, 140));
        painter.drawRect(QRectF(66, 238, 104, 6));
        painter.setBrush(QColor(245, 240, 232, 235));
        painter.drawRoundedRect(QRectF(210, 246, 36, 48), 7, 7);
        painter.setBrush(QColor(120, 220, 180, 179));
        painter.drawRect(QRectF(216, 234, 24, 14));
        painter.setPen(QPen(QColor(240, 215, 190), 3, Qt::SolidLine, Qt::RoundCap));
        painter.drawLine(QPointF(190, 280), QPointF(226, 286));
    } else if (scene == QLatin1String("sun")) {
        QLinearGradient rays(0, 0, 160, 240);
        rays.setColorAt(0, QColor(255, 255, 255, 41));
        rays.setColorAt(1, QColor(255, 255, 255, 0));
        painter.fillRect(QRectF(0, 0, 300, 400), rays);
        painter.setBrush(QColor(255, 242, 196, 217));
        painter.drawEllipse(QPointF(206, 130), 36, 36);
        painter.setBrush(QColor(78, 60, 42, 92));
        QPainterPath silhouette;
        silhouette.moveTo(42, 400);
        silhouette.cubicTo(42, 310, 62, 260, 110, 260);
        silhouette.cubicTo(158, 260, 178, 310, 178, 400);
        silhouette.closeSubpath();
        painter.drawPath(silhouette);
        painter.setPen(QPen(QColor(27, 42, 58, 92), 3));
        painter.drawLine(QPointF(0, 262), QPointF(300, 262));
    } else if (scene == QLatin1String("bag")) {
        painter.setBrush(QColor(243, 227, 207, 230));
        QPainterPath bag;
        bag.moveTo(42, 137);
        bag.lineTo(258, 137);
        bag.lineTo(274, 320);
        bag.lineTo(26, 320);
        bag.closeSubpath();
        painter.drawPath(bag);
        painter.setBrush(QColor(59, 43, 35, 92));
        painter.drawRect(QRectF(40, 138, 220, 18));
        painter.setPen(QPen(QColor(58, 42, 33, 140), 7, Qt::SolidLine, Qt::RoundCap));
        painter.drawArc(QRectF(112, 66, 76, 112), 0, 180 * 16);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(232, 201, 168, 179));
        painter.drawRoundedRect(QRectF(126, 196, 48, 28), 4, 4);
    } else {
        painter.setBrush(QColor(239, 216, 178, 235));
        painter.drawRoundedRect(QRectF(62, 150, 176, 156), 10, 10);
        painter.setBrush(QColor(213, 184, 145, 230));
        painter.drawRect(QRectF(146, 150, 18, 156));
        painter.setBrush(QColor(255, 247, 234, 92));
        painter.drawRect(QRectF(62, 150, 176, 18));
        painter.setPen(QPen(QColor(255, 255, 255, 41), 2));
        painter.drawLine(QPointF(82, 184), QPointF(218, 184));
    }

    QLinearGradient vignette(0, 0, 0, 400);
    vignette.setColorAt(0, QColor(4, 5, 9, 26));
    vignette.setColorAt(0.55, QColor(4, 5, 9, 0));
    vignette.setColorAt(1, QColor(4, 5, 9, 128));
    painter.fillRect(QRectF(0, 0, 300, 400), vignette);
    painter.restore();
}

void paintOralMaterial(QPainter &painter, const QRectF &rect,
                       const StudioData::OralMaterial &material)
{
    if (rect.width() < 2 || rect.height() < 2) {
        return;
    }
    painter.save();
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setClipRect(rect);

    // 导入素材使用真实缩略图：按目标区域等比铺满并居中裁剪。
    if (!material.preview.isNull()) {
        paintCoverImage(painter, rect, material.preview);
        painter.restore();
        return;
    }

    const qreal scale = qMax(rect.width() / 160.0, rect.height() / 100.0);
    painter.translate(rect.center());
    painter.scale(scale, scale);
    painter.translate(-80, -50);

    QLinearGradient background(0, 0, 160, 100);
    background.setColorAt(0, material.paletteFrom);
    background.setColorAt(1, material.paletteTo);
    painter.fillRect(QRectF(0, 0, 160, 100), background);

    QRadialGradient light(40, 10, 130);
    light.setColorAt(0, QColor(255, 255, 255, 102));
    light.setColorAt(1, QColor(255, 255, 255, 0));
    painter.fillRect(QRectF(0, 0, 160, 100), light);

    painter.setPen(Qt::NoPen);
    painter.setBrush(QColor(255, 255, 255, 20));
    painter.drawEllipse(QPointF(132, 18), 34, 34);
    painter.setBrush(QColor(0, 0, 0, 31));
    painter.drawEllipse(QPointF(24, 86), 30, 30);

    const QString visual = material.visual;
    if (visual == QLatin1String("serum")) {
        painter.setBrush(QColor(0, 0, 0, 51));
        painter.drawEllipse(QRectF(45, 83, 70, 12));
        painter.setBrush(QColor(255, 255, 255, 209));
        painter.drawRoundedRect(QRectF(58, 28, 44, 60), 9, 9);
        painter.setBrush(QColor(246, 239, 230, 214));
        painter.drawRoundedRect(QRectF(69, 16, 22, 14), 4, 4);
        painter.setBrush(QColor(53, 38, 29, 191));
        painter.drawRoundedRect(QRectF(75, 8, 10, 10), 3, 3);
        painter.setBrush(QColor(255, 255, 255, 102));
        painter.drawRect(QRectF(66, 36, 5, 45));
    } else if (visual == QLatin1String("cream")) {
        painter.setBrush(QColor(0, 0, 0, 51));
        painter.drawEllipse(QRectF(39, 82, 82, 14));
        painter.setBrush(QColor(255, 255, 255, 209));
        painter.drawRoundedRect(QRectF(42, 45, 76, 42), 12, 12);
        painter.setBrush(QColor(247, 241, 232, 219));
        painter.drawRoundedRect(QRectF(38, 34, 84, 17), 8, 8);
        painter.setBrush(QColor(255, 255, 255, 102));
        painter.drawRect(QRectF(48, 50, 5, 32));
    } else if (visual == QLatin1String("swatch")) {
        painter.setBrush(QColor(245, 230, 220));
        painter.drawEllipse(QPointF(58, 61), 29, 17);
        painter.setBrush(QColor(255, 244, 233));
        painter.drawEllipse(QPointF(86, 54), 27, 16);
        painter.setBrush(QColor(239, 216, 200));
        painter.drawEllipse(QPointF(106, 67), 24, 14);
        painter.setBrush(QColor(255, 255, 255, 140));
        painter.drawEllipse(QPointF(57, 57), 16, 7);
        painter.drawEllipse(QPointF(85, 50), 13, 6);
    } else if (visual == QLatin1String("face")) {
        painter.setBrush(QColor(243, 201, 178));
        painter.drawEllipse(QPointF(82, 53), 29, 36);
        painter.setBrush(QColor(74, 45, 53, 209));
        QPainterPath hair;
        hair.moveTo(49, 51);
        hair.cubicTo(47, 20, 61, 7, 83, 7);
        hair.cubicTo(105, 7, 117, 21, 115, 51);
        hair.cubicTo(109, 33, 100, 25, 82, 25);
        hair.cubicTo(64, 25, 54, 33, 49, 51);
        hair.closeSubpath();
        painter.drawPath(hair);
        painter.setPen(QPen(QColor(255, 255, 255, 89), 2.5));
        painter.drawLine(QPointF(57, 34), QPointF(86, 41));
        painter.setPen(Qt::NoPen);
        painter.drawEllipse(QPointF(117, 72), 3.6, 3.6);
    } else if (visual == QLatin1String("city")) {
        painter.setBrush(QColor(255, 217, 163, 204));
        painter.drawEllipse(QPointF(114, 35), 21, 21);
        const struct {
            qreal x, y, w, h;
            QColor color;
        } buildings[] = {
            {10, 53, 22, 47, QColor(16, 32, 51, 173)},
            {36, 38, 27, 62, QColor(22, 41, 61, 199)},
            {68, 57, 24, 43, QColor(26, 45, 66, 179)},
            {96, 46, 29, 54, QColor(18, 35, 54, 191)},
            {130, 63, 20, 37, QColor(15, 31, 49, 179)},
        };
        for (const auto &building : buildings) {
            painter.setBrush(building.color);
            painter.drawRect(QRectF(building.x, building.y, building.w, building.h));
        }
        painter.setBrush(QColor(255, 223, 174, 148));
        for (int row = 0; row < 3; ++row) {
            for (int column = 0; column < 3; ++column) {
                painter.drawRect(QRectF(42 + column * 8, 58 + row * 8, 3, 2));
            }
        }
    } else if (visual == QLatin1String("sun")) {
        painter.setBrush(QColor(255, 255, 255, 38));
        QPolygonF ray;
        ray << QPointF(0, 0) << QPointF(60, 0) << QPointF(130, 100)
            << QPointF(76, 100);
        painter.drawPolygon(ray);
        painter.setBrush(QColor(255, 242, 196, 217));
        painter.drawEllipse(QPointF(116, 30), 16, 16);
        painter.setBrush(QColor(78, 60, 42, 89));
        QPainterPath silhouette;
        silhouette.moveTo(12, 100);
        silhouette.cubicTo(12, 72, 30, 60, 48, 60);
        silhouette.cubicTo(66, 60, 76, 78, 76, 100);
        silhouette.closeSubpath();
        painter.drawPath(silhouette);
    } else if (visual == QLatin1String("bag")) {
        painter.setBrush(QColor(243, 227, 207, 230));
        QPainterPath bag;
        bag.moveTo(42, 37);
        bag.lineTo(118, 37);
        bag.lineTo(126, 91);
        bag.lineTo(34, 91);
        bag.closeSubpath();
        painter.drawPath(bag);
        painter.setBrush(QColor(85, 60, 52, 102));
        painter.drawRect(QRectF(40, 38, 80, 10));
        painter.setPen(QPen(QColor(58, 42, 33, 140), 4, Qt::SolidLine, Qt::RoundCap));
        painter.drawArc(QRectF(62, 14, 36, 54), 0, 180 * 16);
    } else {
        painter.setBrush(QColor(241, 232, 220));
        QPainterPath notebook;
        notebook.moveTo(30, 78);
        notebook.lineTo(105, 66);
        notebook.lineTo(118, 92);
        notebook.lineTo(42, 100);
        notebook.closeSubpath();
        painter.drawPath(notebook);
        painter.setBrush(QColor(158, 140, 196, 179));
        painter.drawRect(QRectF(30, 78, 76, 4));
        painter.setBrush(QColor(245, 240, 232, 230));
        painter.drawRoundedRect(QRectF(118, 70, 20, 26), 4, 4);
    }

    QLinearGradient vignette(0, 0, 0, 100);
    vignette.setColorAt(0, QColor(0, 0, 0, 0));
    vignette.setColorAt(1, QColor(0, 0, 0, 46));
    painter.fillRect(QRectF(0, 0, 160, 100), vignette);
    painter.restore();
}

void paintPortrait(QPainter &painter, const QRectF &rect)
{
    painter.save();
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setClipRect(rect);
    const qreal scale = qMax(rect.width() / 300.0, rect.height() / 400.0);
    painter.translate(rect.center());
    painter.scale(scale, scale);
    painter.translate(-150, -200);

    QLinearGradient background(0, 0, 300, 400);
    background.setColorAt(0, QColor(QStringLiteral("#3a2b25")));
    background.setColorAt(1, QColor(QStringLiteral("#151011")));
    painter.fillRect(QRectF(0, 0, 300, 400), background);

    QRadialGradient glow(150, 128, 232);
    glow.setColorAt(0, QColor(255, 178, 122, 97));
    glow.setColorAt(1, QColor(255, 178, 122, 0));
    painter.fillRect(QRectF(0, 0, 300, 400), glow);

    painter.setPen(QPen(QColor(255, 178, 122, 56), 1.0));
    painter.setBrush(Qt::NoBrush);
    painter.drawEllipse(QPointF(150, 152), 102, 102);
    painter.setPen(QPen(QColor(255, 178, 122, 20), 1.0));
    painter.drawEllipse(QPointF(150, 152), 118, 118);
    painter.setPen(Qt::NoPen);

    painter.setBrush(QColor(QStringLiteral("#2b2020")));
    QPainterPath hairBack;
    hairBack.moveTo(83, 171);
    hairBack.cubicTo(75, 89, 105, 54, 150, 54);
    hairBack.cubicTo(195, 54, 225, 89, 217, 171);
    hairBack.lineTo(225, 305);
    hairBack.cubicTo(203, 323, 179, 328, 150, 328);
    hairBack.cubicTo(121, 328, 97, 323, 75, 305);
    hairBack.closeSubpath();
    painter.drawPath(hairBack);

    QLinearGradient jacket(32, 286, 268, 400);
    jacket.setColorAt(0, QColor(QStringLiteral("#d9c8bb")));
    jacket.setColorAt(1, QColor(217, 200, 187, 140));
    painter.setBrush(jacket);
    QPainterPath shoulders;
    shoulders.moveTo(32, 400);
    shoulders.cubicTo(37, 320, 84, 286, 150, 286);
    shoulders.cubicTo(216, 286, 263, 320, 268, 400);
    shoulders.closeSubpath();
    painter.drawPath(shoulders);

    painter.setBrush(QColor(238, 194, 172, 184));
    painter.drawRect(QRectF(124, 275, 52, 47));

    QLinearGradient skin(100, 116, 210, 262);
    skin.setColorAt(0, QColor(QStringLiteral("#eec2ac")));
    skin.setColorAt(1, QColor(238, 194, 172, 184));
    painter.setBrush(skin);
    painter.drawEllipse(QPointF(150, 181), 62, 79);
    painter.setBrush(QColor(238, 194, 172, 204));
    painter.drawEllipse(QPointF(99, 182), 9, 16);
    painter.setBrush(QColor(238, 194, 172, 158));
    painter.drawEllipse(QPointF(201, 182), 9, 16);

    painter.setBrush(QColor(QStringLiteral("#2b2020")));
    QPainterPath fringe;
    fringe.moveTo(88, 171);
    fringe.cubicTo(85, 113, 107, 75, 150, 75);
    fringe.cubicTo(193, 75, 215, 112, 212, 171);
    fringe.cubicTo(206, 140, 199, 123, 182, 116);
    fringe.cubicTo(167, 110, 154, 117, 140, 123);
    fringe.cubicTo(118, 132, 99, 127, 88, 171);
    fringe.closeSubpath();
    painter.drawPath(fringe);

    painter.setPen(QPen(QColor(255, 178, 122, 66), 3, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(112, 131), QPointF(150, 118));
    painter.setPen(QPen(QColor(43, 32, 32, 107), 2.2, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(123, 162), QPointF(139, 162));
    painter.drawLine(QPointF(161, 162), QPointF(177, 162));
    painter.setPen(QPen(QColor(43, 32, 32, 184), 2.4, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(124, 177), QPointF(138, 177));
    painter.drawLine(QPointF(162, 177), QPointF(176, 177));
    painter.setPen(QPen(QColor(21, 16, 17, 56), 2.0, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(150, 187), QPointF(153, 203));
    painter.setPen(QPen(QColor(21, 16, 17, 92), 2.4, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(140, 214), QPointF(160, 214));
    painter.setPen(Qt::NoPen);
    painter.setBrush(QColor(255, 178, 122, 191));
    painter.drawEllipse(QPointF(93, 300), 4, 4);

    QLinearGradient vignette(0, 0, 0, 400);
    vignette.setColorAt(0, QColor(4, 5, 9, 20));
    vignette.setColorAt(0.6, QColor(4, 5, 9, 0));
    vignette.setColorAt(1, QColor(4, 5, 9, 92));
    painter.fillRect(QRectF(0, 0, 300, 400), vignette);
    painter.restore();
}

void paintStudioScene(QPainter &painter, const QRectF &rect)
{
    painter.save();
    painter.setRenderHint(QPainter::Antialiasing, true);
    painter.setClipRect(rect);
    const qreal scale = qMax(rect.width() / 640.0, rect.height() / 360.0);
    painter.translate(rect.center());
    painter.scale(scale, scale);
    painter.translate(-320, -180);

    QLinearGradient background(0, 0, 640, 360);
    background.setColorAt(0, QColor(QStringLiteral("#3a2b25")));
    background.setColorAt(1, QColor(QStringLiteral("#151011")));
    painter.fillRect(QRectF(0, 0, 640, 360), background);

    QRadialGradient glow(352, 137, 330);
    glow.setColorAt(0, QColor(255, 178, 122, 87));
    glow.setColorAt(1, QColor(255, 178, 122, 0));
    painter.fillRect(QRectF(0, 0, 640, 360), glow);

    painter.setPen(QPen(QColor(255, 178, 122, 36), 1.0));
    painter.setBrush(Qt::NoBrush);
    painter.drawEllipse(QPointF(320, 150), 132, 132);
    painter.drawEllipse(QPointF(320, 150), 156, 156);

    painter.setPen(Qt::NoPen);
    painter.setBrush(QColor(QStringLiteral("#2b2020")));
    painter.drawRoundedRect(QRectF(250, 40, 140, 160), 62, 62);

    QLinearGradient jacket(150, 250, 500, 360);
    jacket.setColorAt(0, QColor(QStringLiteral("#d9c8bb")));
    jacket.setColorAt(1, QColor(217, 200, 187, 143));
    painter.setBrush(jacket);
    QPainterPath shoulders;
    shoulders.moveTo(150, 360);
    shoulders.cubicTo(160, 288, 230, 250, 320, 250);
    shoulders.cubicTo(410, 250, 480, 288, 490, 360);
    shoulders.closeSubpath();
    painter.drawPath(shoulders);

    QLinearGradient skin(260, 70, 400, 250);
    skin.setColorAt(0, QColor(QStringLiteral("#eec2ac")));
    skin.setColorAt(1, QColor(238, 194, 172, 184));
    painter.setBrush(skin);
    painter.drawEllipse(QPointF(320, 138), 67, 86);

    painter.setBrush(QColor(QStringLiteral("#2b2020")));
    QPainterPath fringe;
    fringe.moveTo(253, 128);
    fringe.cubicTo(250, 65, 275, 28, 320, 28);
    fringe.cubicTo(365, 28, 390, 65, 387, 128);
    fringe.cubicTo(380, 94, 372, 74, 352, 67);
    fringe.cubicTo(336, 61, 322, 69, 307, 76);
    fringe.cubicTo(282, 86, 262, 80, 253, 128);
    fringe.closeSubpath();
    painter.drawPath(fringe);

    painter.setPen(QPen(QColor(43, 32, 32, 153), 2.4, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(291, 132), QPointF(308, 132));
    painter.drawLine(QPointF(332, 132), QPointF(349, 132));
    painter.drawLine(QPointF(291, 147), QPointF(307, 147));
    painter.drawLine(QPointF(333, 147), QPointF(349, 147));
    painter.setPen(QPen(QColor(21, 16, 17, 82), 2.6, Qt::SolidLine, Qt::RoundCap));
    painter.drawLine(QPointF(306, 166), QPointF(334, 166));
    painter.setPen(Qt::NoPen);

    painter.setBrush(QColor(18, 20, 26, 235));
    painter.drawRoundedRect(QRectF(430, 250, 8, 66), 3, 3);
    painter.drawEllipse(QPointF(434, 242), 9, 16);

    QLinearGradient vignette(0, 0, 0, 360);
    vignette.setColorAt(0, QColor(4, 5, 9, 46));
    vignette.setColorAt(0.7, QColor(4, 5, 9, 0));
    vignette.setColorAt(1, QColor(4, 5, 9, 148));
    painter.fillRect(QRectF(0, 0, 640, 360), vignette);
    painter.restore();
}

/* ------------------------------------------------------------------ */
/* 组件                                                                 */
/* ------------------------------------------------------------------ */

OralPosterWidget::OralPosterWidget(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
}

void OralPosterWidget::setSpec(const OralPosterSpec &spec)
{
    m_spec = spec;
    update();
}

void OralPosterWidget::setProgress(int progress)
{
    m_spec.progress = progress;
    update();
}

void OralPosterWidget::setDimmed(bool dimmed)
{
    m_spec.dimmed = dimmed;
    update();
}

void OralPosterWidget::setShowPlay(bool showPlay)
{
    m_spec.showPlay = showPlay;
    update();
}

void OralPosterWidget::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    paintOralPoster(painter, QRectF(rect()), m_spec);
}

MixPosterWidget::MixPosterWidget(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
}

void MixPosterWidget::setSpec(const MixPosterSpec &spec)
{
    m_spec = spec;
    update();
}

void MixPosterWidget::setProgress(int progress)
{
    m_spec.progress = progress;
    update();
}

void MixPosterWidget::setDimmed(bool dimmed)
{
    m_spec.dimmed = dimmed;
    update();
}

void MixPosterWidget::setShowPlay(bool showPlay)
{
    m_spec.showPlay = showPlay;
    update();
}

void MixPosterWidget::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    paintMixPoster(painter, QRectF(rect()), m_spec);
}

MixSceneWidget::MixSceneWidget(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    setSizePolicy(QSizePolicy::Ignored, QSizePolicy::Ignored);
}

void MixSceneWidget::setScene(const QString &scene)
{
    m_scene = scene;
    update();
}

void MixSceneWidget::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    QPainterPath clip;
    clip.addRoundedRect(QRectF(rect()), 3, 3);
    painter.setClipPath(clip);
    paintMixScene(painter, QRectF(rect()), m_scene);
}

MaterialVisualWidget::MaterialVisualWidget(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    setSizePolicy(QSizePolicy::Ignored, QSizePolicy::Ignored);
}

void MaterialVisualWidget::setMaterial(const StudioData::OralMaterial &material)
{
    m_material = material;
    update();
}

void MaterialVisualWidget::setRoundedFrame(int radius, const QColor &border)
{
    m_frameRadius = qMax(0, radius);
    m_frameBorder = border;
    update();
}

void MaterialVisualWidget::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    const QRectF area(rect());
    QPainterPath frame;
    if (m_frameRadius > 0) {
        // 仅顶部圆角：素材卡片底部与名称行相接，不做圆角。
        const qreal r = m_frameRadius;
        frame.moveTo(area.left(), area.bottom());
        frame.lineTo(area.left(), area.top() + r);
        frame.arcTo(QRectF(area.left(), area.top(), 2 * r, 2 * r), 180, -90);
        frame.lineTo(area.right() - r, area.top());
        frame.arcTo(QRectF(area.right() - 2 * r, area.top(), 2 * r, 2 * r), 90,
                    -90);
        frame.lineTo(area.right(), area.bottom());
        painter.setClipPath(frame);
    }
    paintOralMaterial(painter, area, m_material);

    // 导入的视频素材叠加播放标记，提示可以点击预览（缩略图未就绪时也显示）。
    if (!m_material.filePath.isEmpty()
        && m_material.kind == QLatin1String("视频") && width() >= 72
        && height() >= 48) {
        painter.setRenderHint(QPainter::Antialiasing, true);
        const QPointF center = rect().center();
        const qreal radius =
            qBound<qreal>(9.0, qMin(width(), height()) * 0.15, 15.0);
        painter.setPen(Qt::NoPen);
        painter.setBrush(QColor(0, 0, 0, 110));
        painter.drawEllipse(center, radius, radius);
        QPolygonF triangle;
        triangle << center + QPointF(-radius * 0.30, -radius * 0.46)
                 << center + QPointF(radius * 0.55, 0.0)
                 << center + QPointF(-radius * 0.30, radius * 0.46);
        painter.setBrush(QColor(255, 255, 255, 235));
        painter.drawPolygon(triangle);
    }

    // 描边沿缩略图左、上、右三边（不画底边），与卡片自身描边衔接。
    if (m_frameRadius > 0 && m_frameBorder.isValid()) {
        painter.setClipping(false);
        painter.setPen(QPen(m_frameBorder, 1.0));
        painter.setBrush(Qt::NoBrush);
        painter.drawPath(frame);
    }
}

StudioSceneWidget::StudioSceneWidget(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    setSizePolicy(QSizePolicy::Ignored, QSizePolicy::Ignored);
}

void StudioSceneWidget::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    paintStudioScene(painter, QRectF(rect()));
}

Waveform::Waveform(QWidget *parent)
    : QWidget(parent)
{
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
    setSizePolicy(QSizePolicy::Expanding, QSizePolicy::Fixed);
    setFixedHeight(14);
}

void Waveform::setBars(int bars)
{
    m_bars = qMax(1, bars);
    update();
}

void Waveform::setColor(const QColor &color)
{
    m_color = color;
    update();
}

void Waveform::setProgress(int progress)
{
    m_progress = progress;
    update();
}

void Waveform::setAnimated(bool animated)
{
    if (m_animated == animated) {
        return;
    }
    m_animated = animated;
    if (animated) {
        if (!m_timer) {
            m_timer = new QTimer(this);
            m_timer->setInterval(90);
            connect(m_timer, &QTimer::timeout, this, [this] {
                m_phase = (m_phase + 1) % 64;
                update();
            });
        }
        m_timer->start();
    } else if (m_timer) {
        m_timer->stop();
    }
    update();
}

QSize Waveform::sizeHint() const
{
    return QSize(m_bars * 4, 14);
}

void Waveform::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    paintWaveBars(painter, QRectF(rect()), m_bars, m_color, m_progress,
                  m_animated ? m_phase : 0);
}

VoiceOrbWidget::VoiceOrbWidget(const StudioData::MixcutVoice &voice, int size,
                               QWidget *parent)
    : QWidget(parent)
    , m_voice(voice)
    , m_size(size)
{
    setFixedSize(size, size);
    setAttribute(Qt::WA_TransparentForMouseEvents, true);
}

void VoiceOrbWidget::setVoice(const StudioData::MixcutVoice &voice)
{
    m_voice = voice;
    update();
}

QSize VoiceOrbWidget::sizeHint() const
{
    return QSize(m_size, m_size);
}

void VoiceOrbWidget::paintEvent(QPaintEvent *)
{
    QPainter painter(this);
    painter.setRenderHint(QPainter::Antialiasing, true);
    QLinearGradient gradient(0, 0, width(), height());
    gradient.setColorAt(0, m_voice.gradientFrom);
    gradient.setColorAt(1, m_voice.gradientTo);
    painter.setPen(QPen(QColor(255, 255, 255, 46), 1.0));
    painter.setBrush(gradient);
    painter.drawEllipse(QRectF(rect()).adjusted(0.5, 0.5, -0.5, -0.5));
    painter.setFont(Theme::font(qMax(9, qRound(m_size * 0.38)), QFont::DemiBold));
    painter.setPen(Qt::white);
    painter.drawText(rect(), Qt::AlignCenter, m_voice.name.left(1));
}

} // namespace Studio
