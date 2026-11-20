// 文件作用：字幕断句、词级时间对齐与字数均衡折行实现。
#include "CaptionTiming.h"

#include <QDebug>

namespace {

struct CaptionClause
{
    QString displayText;
    // 去掉标点与空白后的文字，用来和 ASR words 逐字对齐。
    QString alignmentText;
};

bool isCaptionBreak(QChar ch)
{
    static const QString breaks = QStringLiteral("，。！？；：、,.!?;:…—\n");
    return breaks.contains(ch);
}

// 数字之间的小数点、千分位、时间冒号与连字符属于数值本身，不当作标点。
bool isNumericSeparator(const QString &text, int index)
{
    if (index <= 0 || index + 1 >= text.size()) {
        return false;
    }
    static const QString separators = QStringLiteral(".,:：，-");
    return separators.contains(text.at(index)) && text.at(index - 1).isDigit()
        && text.at(index + 1).isDigit();
}

bool isPunctuation(QChar ch)
{
    const QChar::Category category = ch.category();
    return category == QChar::Punctuation_Connector
        || category == QChar::Punctuation_Dash
        || category == QChar::Punctuation_Open
        || category == QChar::Punctuation_Close
        || category == QChar::Punctuation_InitialQuote
        || category == QChar::Punctuation_FinalQuote
        || category == QChar::Punctuation_Other;
}

// 保留文字与英文单词间的空格，其余标点（引号、括号、破折号等）全部去掉。
QString stripCaptionPunctuation(const QString &text)
{
    QString cleaned;
    cleaned.reserve(text.size());
    for (int i = 0; i < text.size(); ++i) {
        const QChar ch = text.at(i);
        if (ch.isSpace()) {
            if (!cleaned.isEmpty() && !cleaned.endsWith(QLatin1Char(' '))) {
                cleaned.append(QLatin1Char(' '));
            }
            continue;
        }
        if (isPunctuation(ch) && !isNumericSeparator(text, i)) {
            continue;
        }
        cleaned.append(ch);
    }
    return cleaned.trimmed();
}

QString stripAlignmentText(const QString &displayText)
{
    QString text;
    text.reserve(displayText.size());
    for (const QChar ch : displayText) {
        if (!ch.isSpace()) {
            text.append(ch);
        }
    }
    return text;
}

// 按标点把一句 ASR 文本拆成多条字幕。
QVector<CaptionClause> splitClauses(const QString &text)
{
    QVector<CaptionClause> clauses;
    if (text.isEmpty()) {
        return clauses;
    }

    int clauseStart = 0;
    for (int i = 0; i < text.size(); ++i) {
        const QChar ch = text.at(i);
        bool boundary = i + 1 == text.size();
        if (!boundary && isCaptionBreak(ch)) {
            boundary = !isNumericSeparator(text, i);
        }
        if (!boundary) {
            continue;
        }

        const int clauseEnd = i + 1;
        const QString displayText =
            stripCaptionPunctuation(text.mid(clauseStart, clauseEnd - clauseStart));
        if (!displayText.isEmpty()) {
            CaptionClause clause;
            clause.displayText = displayText;
            clause.alignmentText = stripAlignmentText(displayText);
            clauses.append(clause);
        }
        clauseStart = clauseEnd;
    }
    return clauses;
}

bool hasValidTime(const AsrWord &word)
{
    return word.startMs >= 0 && word.endMs > word.startMs;
}

// ASR 的一个 word 可能包含多个字，按字符在该词中的位置线性插值。
qint64 interpolatedStart(const AsrWord &word, int charOffset)
{
    const int length = word.text.size();
    if (length <= 1) {
        return word.startMs;
    }
    return word.startMs + (word.endMs - word.startMs) * charOffset / length;
}

qint64 interpolatedEnd(const AsrWord &word, int charOffset)
{
    const int length = word.text.size();
    if (length <= 0) {
        return word.endMs;
    }
    if (charOffset >= length - 1) {
        return word.endMs;
    }
    return word.startMs + (word.endMs - word.startMs) * (charOffset + 1) / length;
}

// 词流里的标点、空格只是断句标记，不参与逐字对齐。
bool isWordFiller(QChar ch)
{
    return ch.isSpace() || isPunctuation(ch);
}

// 在 ASR words 上逐字前进，取出一段文字对应的时间范围。
// 词流里的标点/空格会被跳过；数字之间的小数点、冒号等如果正是待匹配字符，按普通字符处理。
class WordCursor
{
public:
    explicit WordCursor(const QVector<AsrWord> &words)
        : m_words(words)
    {
    }

    bool consume(const QString &need, qint64 *startMs, qint64 *endMs)
    {
        if (need.isEmpty()) {
            return false;
        }

        bool hasStart = false;
        qint64 start = 0;
        qint64 end = 0;
        for (const QChar ch : need) {
            bool matched = false;
            while (m_wordIndex < m_words.size()) {
                const AsrWord &word = m_words.at(m_wordIndex);
                if (word.text.isEmpty() || m_charOffset >= word.text.size()) {
                    ++m_wordIndex;
                    m_charOffset = 0;
                    continue;
                }

                const QChar wordChar = word.text.at(m_charOffset);
                if (wordChar == ch) {
                    if (hasValidTime(word)) {
                        const qint64 charStart = interpolatedStart(word, m_charOffset);
                        const qint64 charEnd = interpolatedEnd(word, m_charOffset);
                        if (!hasStart) {
                            start = charStart;
                            hasStart = true;
                        }
                        end = charEnd;
                    }
                    ++m_charOffset;
                    matched = true;
                    break;
                }
                if (isWordFiller(wordChar)) {
                    ++m_charOffset;
                    continue;
                }
                // 真正的文字对不上：这份词级时间线不可信，整句退回按长度分摊。
                return false;
            }
            if (!matched) {
                return false;
            }
        }

        if (!hasStart || end <= start) {
            return false;
        }
        *startMs = start;
        *endMs = end;
        return true;
    }

private:
    const QVector<AsrWord> &m_words;
    int m_wordIndex = 0;
    int m_charOffset = 0;
};

bool assignWordTimes(const AsrUtterance &utterance,
                     const QVector<CaptionClause> &clauses,
                     QVector<TimedCaption> *captions)
{
    WordCursor cursor(utterance.words);
    QVector<TimedCaption> result;
    result.reserve(clauses.size());
    qint64 previousEnd = utterance.startMs;

    for (const CaptionClause &clause : clauses) {
        if (clause.alignmentText.isEmpty()) {
            continue;
        }
        qint64 start = 0;
        qint64 end = 0;
        if (!cursor.consume(clause.alignmentText, &start, &end)) {
            return false;
        }

        start = qBound(utterance.startMs, start, utterance.endMs);
        end = qBound(utterance.startMs, end, utterance.endMs);
        // 相邻词的边界偶尔会有几毫秒重叠；把本行起点吸附到上一行结尾，
        // 比整句退回字符分摊更准确。重叠大到本行没有时长时才放弃词级时间线。
        start = qMax(start, previousEnd);
        if (end <= start) {
            return false;
        }

        TimedCaption caption;
        caption.startMs = start;
        caption.endMs = end;
        caption.text = clause.displayText;
        result.append(caption);
        previousEnd = end;
    }

    if (result.isEmpty()) {
        return false;
    }
    *captions = result;
    return true;
}

QVector<TimedCaption> assignProportionalTimes(const AsrUtterance &utterance,
                                              const QVector<CaptionClause> &clauses)
{
    QVector<TimedCaption> result;
    qint64 totalWeight = 0;
    for (const CaptionClause &clause : clauses) {
        totalWeight += qMax(1, clause.alignmentText.size());
    }
    if (totalWeight <= 0) {
        return result;
    }

    const qint64 durationMs = utterance.endMs - utterance.startMs;
    qint64 consumedWeight = 0;
    result.reserve(clauses.size());
    for (const CaptionClause &clause : clauses) {
        const qint64 weight = qMax(1, clause.alignmentText.size());
        const qint64 start = utterance.startMs + durationMs * consumedWeight / totalWeight;
        consumedWeight += weight;
        qint64 end = utterance.startMs + durationMs * consumedWeight / totalWeight;
        if (end <= start) {
            end = qMin(utterance.endMs, start + 1);
        }
        if (end > start) {
            TimedCaption caption;
            caption.startMs = start;
            caption.endMs = end;
            caption.text = clause.displayText;
            result.append(caption);
        }
    }
    return result;
}

// 折行用的最小单元：一个汉字/假名/表情，或一个完整的英文单词/数字。
struct WrapUnit
{
    QString text;
    int chars = 0;
    bool spaceBefore = false;
};

bool isCjkOrKana(QChar ch)
{
    const ushort code = ch.unicode();
    return (code >= 0x3040 && code <= 0x30FF)   // 日文假名
        || (code >= 0x3400 && code <= 0x4DBF)   // 中文扩展 A
        || (code >= 0x4E00 && code <= 0x9FFF)   // 中文基本区
        || (code >= 0xAC00 && code <= 0xD7AF)   // 韩文
        || (code >= 0xF900 && code <= 0xFAFF);  // 兼容汉字
}

// 英文单词与数字内部允许出现的连接符（仅在两侧都是数字时并入同一单元）。
bool isWordCharacter(QChar ch)
{
    return !isCjkOrKana(ch) && ch.isLetterOrNumber();
}

bool isPercentSign(QChar ch)
{
    return ch == QLatin1Char('%') || ch == QChar(0xFF05);
}

bool isNumericConnector(QChar ch)
{
    static const QString connectors = QStringLiteral(".,:/，-／");
    return connectors.contains(ch);
}

QVector<WrapUnit> tokenizeForWrapping(const QString &text)
{
    QVector<WrapUnit> units;
    int index = 0;
    bool pendingSpace = false;
    while (index < text.size()) {
        const QChar ch = text.at(index);
        if (ch.isSpace()) {
            pendingSpace = !units.isEmpty();
            ++index;
            continue;
        }

        WrapUnit unit;
        unit.spaceBefore = pendingSpace && !units.isEmpty();
        pendingSpace = false;

        if (ch.isHighSurrogate() && index + 1 < text.size()
            && text.at(index + 1).isLowSurrogate()) {
            unit.text = text.mid(index, 2);
            index += 2;
        } else if (isWordCharacter(ch)) {
            // 100、100%、29.3、hello、ok 等都作为不可拆分的整体。
            const int start = index;
            ++index;
            while (index < text.size()) {
                const QChar next = text.at(index);
                if (isWordCharacter(next)) {
                    ++index;
                    continue;
                }
                if (isPercentSign(next) && index > start
                    && text.at(index - 1).isDigit()) {
                    ++index;
                    continue;
                }
                if (isNumericConnector(next) && index > start
                    && index + 1 < text.size() && text.at(index - 1).isDigit()
                    && text.at(index + 1).isDigit()) {
                    ++index;
                    continue;
                }
                break;
            }
            unit.text = text.mid(start, index - start);
        } else {
            unit.text = QString(ch);
            ++index;
        }
        unit.chars = unit.text.size();
        units.append(unit);
    }
    return units;
}

} // namespace

QString CaptionTiming::wrapCaptionText(const QString &text, int maxCharsPerLine)
{
    if (maxCharsPerLine <= 0 || text.isEmpty()) {
        return text;
    }
    const QVector<WrapUnit> units = tokenizeForWrapping(text);
    int totalChars = 0;
    for (const WrapUnit &unit : units) {
        totalChars += unit.chars;
    }
    if (totalChars <= maxCharsPerLine || units.isEmpty()) {
        return text.trimmed();
    }

    // 每行目标长度按剩余字数动态均分：19 字 → 10/9，31 字 → 11/10/10。
    QStringList lines;
    int unitIndex = 0;
    int remainingChars = totalChars;
    while (unitIndex < units.size()) {
        const int linesLeft =
            qMax(1, (remainingChars + maxCharsPerLine - 1) / maxCharsPerLine);
        const int target = (remainingChars + linesLeft - 1) / linesLeft;

        QString line;
        int lineChars = 0;
        while (unitIndex < units.size()) {
            const WrapUnit &unit = units.at(unitIndex);
            // 空行总是先放入一个不可拆单元；放不下时整体挪到下一行，
            // 保证单词和数字不会被从中间截断。
            if (!line.isEmpty() && lineChars + unit.chars > target) {
                break;
            }
            if (!line.isEmpty() && unit.spaceBefore) {
                line += QLatin1Char(' ');
            }
            line += unit.text;
            lineChars += unit.chars;
            ++unitIndex;
        }
        lines.append(line);
        remainingChars -= lineChars;
    }
    return lines.join(QLatin1Char('\n'));
}

QVector<TimedCaption> CaptionTiming::splitUtterance(const AsrUtterance &utterance)
{
    if (utterance.text.isEmpty() || utterance.endMs <= utterance.startMs) {
        return {};
    }

    const QVector<CaptionClause> clauses = splitClauses(utterance.text);
    if (clauses.isEmpty()) {
        return {};
    }

    // 优先使用 ASR 的词级时间线；任何一处对不上就整句退回按长度分摊，
    // 避免同一条成片里一部分字幕按词、一部分按比例，时间轴忽准忽偏。
    if (!utterance.words.isEmpty()) {
        QVector<TimedCaption> captions;
        if (assignWordTimes(utterance, clauses, &captions)) {
            return captions;
        }
        qWarning() << "ASR words do not match utterance text, falling back to proportional"
                   << "caption timing:" << utterance.text.left(40);
    }
    return assignProportionalTimes(utterance, clauses);
}
