#pragma once

#include <QColor>
#include <QFont>
#include <QString>

class QApplication;

namespace Theme {

// 窗口外壳惯例：macOS 交通灯 / Windows 11 单层标题栏。
// 默认跟随构建目标平台；LIVEMIX_WINDOW_STYLE=windows|macos 可用于设计预览。
enum class WindowStyle { Mac, Windows };

WindowStyle windowStyle();
bool isWindowsWindowStyle();
int windowCornerRadius();

// Core palette from the LiveMix Studio design tokens.
inline const QColor Bg0{"#08090D"};
inline const QColor Window{"#101218"};
inline const QColor Surface1{"#14171F"};
inline const QColor Surface2{"#1A1E28"};
inline const QColor Surface3{"#222734"};
inline const QColor Border{"#242938"};
inline const QColor BorderStrong{"#343B4E"};
inline const QColor Text1{"#E9ECF4"};
inline const QColor Text2{"#9BA2B5"};
inline const QColor Text3{"#62697E"};
inline const QColor Primary{"#6E8BFF"};
inline const QColor PrimaryHover{"#849CFF"};
inline const QColor Ai{"#FFB224"};
inline const QColor Success{"#3ECF8E"};
inline const QColor Danger{"#F6465D"};
inline const QColor Warn{"#F5A623"};
inline const QColor OnAccent{"#0B0C10"};

QColor alpha(const QColor &color, qreal opacity);
QColor mix(const QColor &a, const QColor &b, qreal amount);

QFont font(int pixelSize, int weight = QFont::Normal);
QFont monoFont(int pixelSize, int weight = QFont::Normal);

QString modifierKey();
QString shortcutText(const QString &key);

void apply(QApplication &app);

} // namespace Theme
