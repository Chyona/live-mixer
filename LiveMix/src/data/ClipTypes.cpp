// 文件作用：ASR 转写结果的 JSON 编解码实现。
#include "ClipTypes.h"

#include <QJsonArray>
#include <QJsonValue>
#include <QSet>

namespace {

// 网关若透传说话人字段，字段名可能是 speaker / speaker_id / speaker_label；
// 数字编号也转成字符串，界面统一按标签展示。
QString parseSpeaker(const QJsonObject &object)
{
    static const char *keys[] = {"speaker", "speaker_id", "speaker_label"};
    for (const char *key : keys) {
        const QJsonValue value = object.value(QLatin1String(key));
        if (value.isString()) {
            const QString text = value.toString().trimmed();
            if (!text.isEmpty()) {
                return text;
            }
        } else if (value.isDouble()) {
            return QString::number(value.toInt());
        }
    }
    return QString();
}

// 置信度既可能是 0–1 的小数，也可能是 0–100 的百分数；缺失时保持 -1。
double parseConfidence(const QJsonObject &object)
{
    if (!object.contains(QStringLiteral("confidence"))) {
        return -1.0;
    }
    const QJsonValue value = object.value(QStringLiteral("confidence"));
    bool ok = false;
    double confidence = 0.0;
    if (value.isDouble()) {
        confidence = value.toDouble();
        ok = true;
    } else if (value.isString()) {
        confidence = value.toString().toDouble(&ok);
    }
    if (!ok) {
        return -1.0;
    }
    if (confidence >= 0.0 && confidence <= 1.0) {
        confidence *= 100.0;
    }
    return confidence >= 0.0 && confidence <= 100.0 ? confidence : -1.0;
}

} // namespace

qint64 AsrTranscript::durationMs() const
{
    return utterances.isEmpty() ? 0 : utterances.last().endMs;
}

int AsrTranscript::speakerCount() const
{
    QSet<QString> speakers;
    for (const AsrUtterance &utterance : utterances) {
        const QString speaker = utterance.speaker.trimmed();
        if (!speaker.isEmpty()) {
            speakers.insert(speaker);
        }
    }
    return speakers.size();
}

double AsrTranscript::averageConfidence() const
{
    double total = 0.0;
    int count = 0;
    for (const AsrUtterance &utterance : utterances) {
        if (utterance.confidence >= 0.0) {
            total += utterance.confidence;
            ++count;
        }
    }
    return count > 0 ? total / count : -1.0;
}

AsrTranscript AsrTranscript::fromJson(const QJsonObject &data, QString *error)
{
    AsrTranscript transcript;
    transcript.text = data.value(QStringLiteral("text")).toString();

    const QJsonArray utterances = data.value(QStringLiteral("utterances")).toArray();
    transcript.utterances.reserve(utterances.size());
    for (const QJsonValue &value : utterances) {
        const QJsonObject object = value.toObject();
        AsrUtterance utterance;
        utterance.text = object.value(QStringLiteral("text")).toString();
        utterance.speaker = object.value(QStringLiteral("speaker")).toString().trimmed();
        // 缓存里保存的已经是 0–100 的百分数，不再做 0–1 归一化。
        utterance.confidence =
            object.value(QStringLiteral("confidence")).toDouble(-1.0);

        const QJsonArray words = object.value(QStringLiteral("words")).toArray();
        utterance.words.reserve(words.size());
        for (const QJsonValue &wordValue : words) {
            const QJsonObject wordObject = wordValue.toObject();
            AsrWord word;
            word.text = wordObject.value(QStringLiteral("text")).toString();
            word.startMs = wordObject.value(QStringLiteral("start_time")).toInteger();
            word.endMs = wordObject.value(QStringLiteral("end_time")).toInteger();
            utterance.words.append(word);
        }

        // 缓存格式显式保存分句起止时间；旧缓存没有该字段时再用逐字时间线推导。
        const QJsonValue startValue = object.value(QStringLiteral("start_ms"));
        const QJsonValue endValue = object.value(QStringLiteral("end_ms"));
        bool hasRange = false;
        if (startValue.isDouble() && endValue.isDouble()) {
            utterance.startMs = startValue.toInteger();
            utterance.endMs = endValue.toInteger();
            hasRange = utterance.endMs > utterance.startMs;
        }
        if (!hasRange && !utterance.words.isEmpty()) {
            utterance.startMs = utterance.words.first().startMs;
            utterance.endMs = utterance.words.last().endMs;
        } else if (!hasRange && !transcript.utterances.isEmpty()) {
            // 没有任何时间线时退化为紧随上一句，避免时间线出现空洞。
            utterance.startMs = transcript.utterances.last().endMs;
            utterance.endMs = utterance.startMs;
        }

        if (!utterance.text.trimmed().isEmpty()) {
            transcript.utterances.append(utterance);
        }
    }

    if (transcript.isEmpty()) {
        if (error) {
            *error = QStringLiteral("ASR 返回结果为空。");
        }
        return AsrTranscript();
    }
    return transcript;
}

AsrTranscript AsrTranscript::fromOpenAiVerboseJson(const QJsonObject &json)
{
    AsrTranscript transcript;
    transcript.text = json.value(QStringLiteral("text")).toString().trimmed();

    // verbose_json 的分句与逐词时间线单位都是秒，这里统一转成毫秒。
    const QJsonArray segments = json.value(QStringLiteral("segments")).toArray();
    for (const QJsonValue &value : segments) {
        const QJsonObject segment = value.toObject();
        AsrUtterance utterance;
        utterance.text = segment.value(QStringLiteral("text")).toString().trimmed();
        utterance.startMs =
            qRound64(segment.value(QStringLiteral("start")).toDouble() * 1000.0);
        utterance.endMs =
            qRound64(segment.value(QStringLiteral("end")).toDouble() * 1000.0);
        // 说话人分离与置信度属于网关可选字段；没有时保持为空 / -1，
        // 过程页会显示通用的说话人标签与 “—”，不伪造数据。
        utterance.speaker = parseSpeaker(segment);
        utterance.confidence = parseConfidence(segment);
        if (utterance.text.isEmpty() || utterance.endMs <= utterance.startMs) {
            continue;
        }

        // 只收有效时间：网关没有透传 words 时该数组为空，字幕层会自动退回
        // 按文字长度分摊。
        const QJsonArray words = segment.value(QStringLiteral("words")).toArray();
        utterance.words.reserve(words.size());
        for (const QJsonValue &wordValue : words) {
            const QJsonObject wordObject = wordValue.toObject();
            AsrWord word;
            word.text = wordObject.value(QStringLiteral("word")).toString();
            if (word.text.isEmpty()) {
                word.text = wordObject.value(QStringLiteral("text")).toString();
            }
            word.startMs =
                qRound64(wordObject.value(QStringLiteral("start")).toDouble() * 1000.0);
            word.endMs =
                qRound64(wordObject.value(QStringLiteral("end")).toDouble() * 1000.0);
            if (word.text.isEmpty() || word.startMs < 0 || word.endMs <= word.startMs) {
                continue;
            }
            utterance.words.append(word);
        }

        transcript.utterances.append(utterance);
    }

    // 少数模型不返回 segments；退化成整段文本 + 总时长，至少保证可继续选片。
    if (transcript.utterances.isEmpty() && !transcript.text.isEmpty()) {
        AsrUtterance utterance;
        utterance.text = transcript.text;
        utterance.startMs = 0;
        utterance.endMs =
            qRound64(json.value(QStringLiteral("duration")).toDouble() * 1000.0);
        transcript.utterances.append(utterance);
    }
    return transcript;
}

QJsonObject AsrTranscript::toJson() const
{
    QJsonArray utteranceArray;
    for (const AsrUtterance &utterance : utterances) {
        QJsonArray wordArray;
        for (const AsrWord &word : utterance.words) {
            QJsonObject wordObject;
            wordObject.insert(QStringLiteral("text"), word.text);
            wordObject.insert(QStringLiteral("start_time"), word.startMs);
            wordObject.insert(QStringLiteral("end_time"), word.endMs);
            wordArray.append(wordObject);
        }

        QJsonObject utteranceObject;
        utteranceObject.insert(QStringLiteral("text"), utterance.text);
        // 网关 ASR 只返回分句时间线（无逐字时间线），必须显式保存起止时间，
        // 否则缓存复用时字幕与切片时间会丢失。
        utteranceObject.insert(QStringLiteral("start_ms"), utterance.startMs);
        utteranceObject.insert(QStringLiteral("end_ms"), utterance.endMs);
        utteranceObject.insert(QStringLiteral("words"), wordArray);
        if (!utterance.speaker.isEmpty()) {
            utteranceObject.insert(QStringLiteral("speaker"), utterance.speaker);
        }
        if (utterance.confidence >= 0.0) {
            utteranceObject.insert(QStringLiteral("confidence"), utterance.confidence);
        }
        utteranceArray.append(utteranceObject);
    }

    QJsonObject root;
    root.insert(QStringLiteral("text"), text);
    root.insert(QStringLiteral("utterances"), utteranceArray);
    return root;
}

QString clipTaskStepName(int step)
{
    switch (step) {
    case 0:
        return QStringLiteral("ASR 转写");
    case 1:
        return QStringLiteral("AI 选片");
    case 2:
        return QStringLiteral("生成草稿");
    default:
        return QStringLiteral("处理中");
    }
}
