#include "ApplicationStyle.h"

#include <QApplication>
#include <QFontDatabase>
#include <QPalette>
#include <QStyleFactory>

namespace {

QFont::Weight qtWeight(int weight)
{
    const int bounded = qBound(100, ((weight + 50) / 100) * 100, 900);
    return static_cast<QFont::Weight>(bounded);
}

} // namespace

namespace Theme {

WindowStyle windowStyle()
{
    static const WindowStyle style = [] {
        const QString preview =
            qEnvironmentVariable("LIVEMIX_WINDOW_STYLE").trimmed().toLower();
        if (preview == QLatin1String("windows")) {
            return WindowStyle::Windows;
        }
        if (preview == QLatin1String("macos")) {
            return WindowStyle::Mac;
        }
#if defined(LIVEMIX_PLATFORM_WINDOWS)
        return WindowStyle::Windows;
#else
        return WindowStyle::Mac;
#endif
    }();
    return style;
}

bool isWindowsWindowStyle()
{
    return windowStyle() == WindowStyle::Windows;
}

int windowCornerRadius()
{
    return isWindowsWindowStyle() ? 8 : 12;
}

QColor alpha(const QColor &color, qreal opacity)
{
    QColor result = color;
    result.setAlphaF(qBound(0.0, opacity, 1.0));
    return result;
}

QColor mix(const QColor &a, const QColor &b, qreal amount)
{
    amount = qBound(0.0, amount, 1.0);
    return QColor::fromRgbF(a.redF() * (1.0 - amount) + b.redF() * amount,
                            a.greenF() * (1.0 - amount) + b.greenF() * amount,
                            a.blueF() * (1.0 - amount) + b.blueF() * amount,
                            a.alphaF() * (1.0 - amount) + b.alphaF() * amount);
}

QFont font(int pixelSize, int weight)
{
    static const QFont systemFont = QFontDatabase::systemFont(QFontDatabase::GeneralFont);
    static const QStringList families = [] {
        QStringList result = {QFontDatabase::systemFont(QFontDatabase::GeneralFont).family()};
#if defined(LIVEMIX_PLATFORM_MACOS)
        result << QStringLiteral("PingFang SC") << QStringLiteral("Apple Symbols");
#elif defined(LIVEMIX_PLATFORM_WINDOWS)
        result << QStringLiteral("Microsoft YaHei UI") << QStringLiteral("Segoe UI Symbol");
#else
        result << QStringLiteral("Noto Sans CJK SC");
#endif
        return result;
    }();

    QFont f = systemFont;
    f.setFamilies(families);
    f.setStyleHint(QFont::SansSerif);
    f.setPixelSize(pixelSize);
    f.setWeight(qtWeight(weight));
    f.setStyleStrategy(QFont::PreferAntialias);
    return f;
}

QFont monoFont(int pixelSize, int weight)
{
    static const QFont systemMono = QFontDatabase::systemFont(QFontDatabase::FixedFont);
    static const QStringList families = [] {
        QStringList result = {QFontDatabase::systemFont(QFontDatabase::FixedFont).family()};
#if defined(LIVEMIX_PLATFORM_MACOS)
        result << QStringLiteral("Apple Symbols");
#elif defined(LIVEMIX_PLATFORM_WINDOWS)
        result << QStringLiteral("Segoe UI Symbol");
#else
        result << QStringLiteral("DejaVu Sans Mono");
#endif
        return result;
    }();

    QFont f = systemMono;
    f.setFamilies(families);
    f.setStyleHint(QFont::Monospace);
    f.setFixedPitch(true);
    f.setPixelSize(pixelSize);
    f.setWeight(qtWeight(weight));
    f.setStyleStrategy(QFont::PreferAntialias);
    return f;
}

QString modifierKey()
{
#if defined(LIVEMIX_PLATFORM_MACOS)
    return QStringLiteral("⌘");
#else
    return QStringLiteral("Ctrl");
#endif
}

QString shortcutText(const QString &key)
{
#if defined(LIVEMIX_PLATFORM_MACOS)
    return QStringLiteral("⌘") + key;
#else
    return QStringLiteral("Ctrl+") + key;
#endif
}

void apply(QApplication &app)
{
    app.setStyle(QStyleFactory::create(QStringLiteral("Fusion")));

    QPalette palette;
    palette.setColor(QPalette::Window, Window);
    palette.setColor(QPalette::WindowText, Text1);
    palette.setColor(QPalette::Base, Surface1);
    palette.setColor(QPalette::AlternateBase, Surface2);
    palette.setColor(QPalette::ToolTipBase, Surface3);
    palette.setColor(QPalette::ToolTipText, Text1);
    palette.setColor(QPalette::Text, Text1);
    palette.setColor(QPalette::Button, Surface2);
    palette.setColor(QPalette::ButtonText, Text1);
    palette.setColor(QPalette::BrightText, Qt::white);
    palette.setColor(QPalette::Link, Primary);
    palette.setColor(QPalette::Highlight, Primary);
    palette.setColor(QPalette::HighlightedText, OnAccent);
    palette.setColor(QPalette::PlaceholderText, Text3);
    palette.setColor(QPalette::Disabled, QPalette::Text, Text3);
    palette.setColor(QPalette::Disabled, QPalette::ButtonText, Text3);
    app.setPalette(palette);

    app.setFont(font(13));

    app.setStyleSheet(QStringLiteral(R"(
        QToolTip {
            background-color: #222734;
            color: #E9ECF4;
            border: 1px solid #343B4E;
            border-radius: 5px;
            padding: 5px 8px;
            font-size: 11px;
        }
        QScrollBar:vertical {
            background: transparent;
            width: 9px;
            margin: 2px 1px 2px 1px;
        }
        QScrollBar::handle:vertical {
            background: #343B4E;
            min-height: 28px;
            border-radius: 3px;
        }
        QScrollBar::handle:vertical:hover {
            background: #465069;
        }
        QScrollBar::add-line:vertical, QScrollBar::sub-line:vertical {
            height: 0px;
            background: transparent;
        }
        QScrollBar::add-page:vertical, QScrollBar::sub-page:vertical {
            background: transparent;
        }
        QScrollBar:horizontal {
            background: transparent;
            height: 9px;
            margin: 1px 2px 1px 2px;
        }
        QScrollBar::handle:horizontal {
            background: #343B4E;
            min-width: 28px;
            border-radius: 3px;
        }
        QScrollBar::handle:horizontal:hover {
            background: #465069;
        }
        QScrollBar::add-line:horizontal, QScrollBar::sub-line:horizontal {
            width: 0px;
            background: transparent;
        }
        QScrollBar::add-page:horizontal, QScrollBar::sub-page:horizontal {
            background: transparent;
        }
        QPlainTextEdit, QTextEdit { selection-background-color: #4A63BC; selection-color: #FFFFFF; }
        QLineEdit { selection-background-color: #4A63BC; selection-color: #FFFFFF; }
        QComboBox QAbstractItemView {
            background: #1A1E28;
            color: #E9ECF4;
            border: 1px solid #343B4E;
            border-radius: 7px;
            padding: 5px;
            outline: 0;
            selection-background-color: rgba(110, 139, 255, 51);
            selection-color: #E9ECF4;
        }
        QComboBox QAbstractItemView::item {
            min-height: 26px;
            padding: 2px 8px;
            border-radius: 5px;
        }
        QMenu {
            background: #1A1E28;
            color: #E9ECF4;
            border: 1px solid #343B4E;
            border-radius: 7px;
            padding: 5px;
        }
        QMenu::item { padding: 5px 18px 5px 12px; border-radius: 5px; }
        QMenu::item:selected { background: rgba(110, 139, 255, 46); }
        QMenu::item:disabled { color: #62697E; }
        QMenu::separator { height: 1px; background: #242938; margin: 4px 6px; }
    )"));
}

} // namespace Theme
