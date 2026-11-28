// 文件作用：ffmpeg 子进程调用实现（音频提取、降噪 PCM 解码/封装、
// 视频裁剪、分辨率探测）。
#include "FfmpegUtils.h"

#include "../core/MediaUtils.h"
#include "FfmpegLocator.h"
#include "MediaProbe.h"

#include <QCryptographicHash>
#include <QDateTime>
#include <QDebug>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QProcess>
#include <QStandardPaths>
#include <QStringList>
#include <QTemporaryFile>
#include <QThread>

#include <algorithm>

#ifdef Q_OS_WIN
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#endif

namespace {

QString shortError(const QString &text)
{
    const QString trimmed = text.trimmed();
    if (trimmed.isEmpty()) {
        return QStringLiteral("ffmpeg 执行失败。");
    }
    return trimmed.size() > 800 ? trimmed.right(800) : trimmed;
}

QString previewCachePath(const QString &videoPath, const QString &suffix)
{
    QString cacheDir = QStandardPaths::writableLocation(QStandardPaths::CacheLocation);
    if (cacheDir.isEmpty()) {
        cacheDir = QDir::tempPath();
    }
    const QFileInfo info(videoPath);
    const QString key = videoPath + QLatin1Char(':')
        + QString::number(info.lastModified().toMSecsSinceEpoch());
    const QString hash = QString::fromLatin1(
        QCryptographicHash::hash(key.toUtf8(), QCryptographicHash::Sha1).toHex().left(16));
    return QDir(cacheDir).filePath(QStringLiteral("previews/") + hash + suffix);
}

bool runFfmpeg(const QStringList &arguments, int timeoutMs, QString *error)
{
    const QString ffmpeg = FfmpegLocator::ffmpegPath();
    if (ffmpeg.isEmpty()) {
        if (error) {
            *error = QStringLiteral("未找到 ffmpeg，请重新安装应用或设置 LIVEMIX_FFMPEG_DIR。");
        }
        return false;
    }

    QProcess process;
    process.setProgram(ffmpeg);
    process.setArguments(arguments);
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(5000)) {
        if (error) {
            *error = QStringLiteral("无法启动 ffmpeg：%1").arg(process.errorString());
        }
        return false;
    }
    if (!process.waitForFinished(timeoutMs)) {
        process.kill();
        process.waitForFinished(1000);
        if (error) {
            *error = QStringLiteral("ffmpeg 执行超时。");
        }
        return false;
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        if (error) {
            *error = shortError(QString::fromLocal8Bit(process.readAllStandardError()));
        }
        return false;
    }
    return true;
}

#ifdef Q_OS_WIN
QString win32ErrorText(DWORD code)
{
    if (code == 0) {
        return QString();
    }
    LPWSTR buffer = nullptr;
    const DWORD length = FormatMessageW(
        FORMAT_MESSAGE_ALLOCATE_BUFFER | FORMAT_MESSAGE_FROM_SYSTEM
            | FORMAT_MESSAGE_IGNORE_INSERTS,
        nullptr, code, 0, reinterpret_cast<LPWSTR>(&buffer), 0, nullptr);
    QString text;
    if (length > 0 && buffer) {
        text = QString::fromWCharArray(buffer, int(length)).simplified();
    }
    if (buffer) {
        LocalFree(buffer);
    }
    return text;
}
#endif

// 把已校验通过的 MP4 保存到正式路径。
// Windows 上 QFile::rename 走的是 MoveFile（不覆盖已存在文件），且临时文件
// 被杀毒扫描、同步盘或播放器短暂占用时会直接失败且拿不到错误码。这里改用
// MoveFileEx(REPLACE_EXISTING) 并做退避重试，失败时把 Win32 错误带回界面与
// 日志（5=拒绝访问/只读，32=文件被占用，183=目标已存在），避免只剩一句
// 无法保存、无法排查的提示。
bool promoteFile(const QString &sourcePath, const QString &targetPath, QString *error)
{
    constexpr int kMaxAttempts = 6;
    bool moved = false;
    QString detail;
#ifdef Q_OS_WIN
    const QString source = QDir::toNativeSeparators(sourcePath);
    const QString target = QDir::toNativeSeparators(targetPath);
    DWORD lastError = 0;
    for (int attempt = 0; attempt < kMaxAttempts; ++attempt) {
        if (::MoveFileExW(reinterpret_cast<const wchar_t *>(source.utf16()),
                          reinterpret_cast<const wchar_t *>(target.utf16()),
                          MOVEFILE_REPLACE_EXISTING)) {
            moved = true;
            break;
        }
        lastError = ::GetLastError();
        // 共享冲突/权限问题多由杀毒或同步盘短暂占用引起，稍等后重试通常能
        // 成功；其余错误（路径不存在、跨卷、磁盘满）重试没有意义。
        if (lastError != ERROR_SHARING_VIOLATION
            && lastError != ERROR_ACCESS_DENIED
            && lastError != ERROR_LOCK_VIOLATION) {
            break;
        }
        qWarning() << "MoveFileEx failed, retrying save:" << targetPath
                   << "win32 error" << lastError << "attempt" << attempt + 1;
        if (attempt + 1 < kMaxAttempts) {
            QThread::msleep(100u << attempt); // 100/200/400/800/1600ms
        }
    }
    if (!moved) {
        // 最常见的两类失败给出可操作的提示，其余错误附上系统文案。
        if (lastError == ERROR_SHARING_VIOLATION
            || lastError == ERROR_LOCK_VIOLATION) {
            detail = QStringLiteral(
                         "Windows 错误 %1：文件被其他程序占用（可能是杀毒扫描、"
                         "播放器或同步盘），已重试 %2 次")
                         .arg(lastError)
                         .arg(kMaxAttempts - 1);
        } else if (lastError == ERROR_ACCESS_DENIED) {
            detail = QStringLiteral(
                         "Windows 错误 %1：拒绝访问（目标文件可能只读、被其他"
                         "程序打开或被安全软件阻止）")
                         .arg(lastError);
        } else {
            const QString systemText = win32ErrorText(lastError);
            detail = systemText.isEmpty()
                ? QStringLiteral("Windows 错误 %1").arg(lastError)
                : QStringLiteral("Windows 错误 %1：%2").arg(lastError).arg(systemText);
        }
    }
#else
    QFile sourceFile(sourcePath);
    for (int attempt = 0; attempt < kMaxAttempts; ++attempt) {
        if (sourceFile.rename(targetPath)) {
            moved = true;
            break;
        }
        if (attempt + 1 < kMaxAttempts) {
            QThread::msleep(100u << attempt);
        }
    }
    if (!moved) {
        detail = sourceFile.errorString();
    }
#endif
    if (moved) {
        return true;
    }
    if (error) {
        *error = detail.isEmpty()
            ? QStringLiteral("无法保存完整 MP4：%1").arg(targetPath)
            : QStringLiteral("无法保存完整 MP4：%1（%2）").arg(targetPath, detail);
    }
    return false;
}

} // namespace

bool FfmpegUtils::extractMp3(const QString &inputPath, const QString &outputPath,
                             QString *error)
{
    const QFileInfo input(inputPath);
    if (!input.exists()) {
        if (error) {
            *error = QStringLiteral("源文件不存在：%1").arg(inputPath);
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建音频目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }

    // ASR 标准版通过公网 URL 读取整段音频，不受上传体积限制；
    // 16kHz 单声道 64kbps 约每小时 28MB，足够容纳常见直播时长。
    //
    // ASR 返回的时间线是相对这份 MP3 的，所以 MP3 必须与源 MP4 等长：
    // 1. aresample=first_pts=0 在音轨晚于视频起点时补开头静音；
    // 2. apad 补尾部静音，-t 再精确截到视频时长（音轨比视频长时同样会截断）。
    qint64 targetDurationMs = 0;
    const bool hasDuration =
        mediaDurationMs(inputPath, &targetDurationMs) && targetDurationMs > 0;

    QStringList args = {
        QStringLiteral("-y"),           QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
        QStringLiteral("error"),        QStringLiteral("-i"),
        inputPath,                      QStringLiteral("-map"),
        QStringLiteral("0:a:0"),
    };
    if (hasDuration) {
        args << QStringLiteral("-af")
             << QStringLiteral("aresample=async=1:first_pts=0,apad")
             << QStringLiteral("-t")
             << QString::number(targetDurationMs / 1000.0, 'f', 3);
    } else {
        // 读不到容器时长时至少保证音轨起点对齐视频 t=0。
        args << QStringLiteral("-af")
             << QStringLiteral("aresample=async=1:first_pts=0");
    }
    args << QStringLiteral("-ac") << QStringLiteral("1")
         << QStringLiteral("-ar") << QStringLiteral("16000")
         << QStringLiteral("-c:a") << QStringLiteral("libmp3lame")
         << QStringLiteral("-b:a") << QStringLiteral("64k")
         << outputPath;
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        return false;
    }

    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("音频提取失败，未生成有效文件。");
        }
        return false;
    }

    // 复查输出时长：MP3 按帧（16kHz 下约 72ms）对齐，允许少量取整误差；
    // 偏差超过 500ms 说明补静音/截断没有生效，继续下去会让字幕整体错位。
    qint64 outputDurationMs = 0;
    if (hasDuration && mediaDurationMs(outputPath, &outputDurationMs)) {
        const qint64 differenceMs = qAbs(outputDurationMs - targetDurationMs);
        if (differenceMs > 500) {
            if (error) {
                *error = QStringLiteral("音频时长与源视频不一致（源 %1ms，音频 %2ms）。")
                             .arg(targetDurationMs)
                             .arg(outputDurationMs);
            }
            return false;
        }
        if (differenceMs > 100) {
            qWarning() << "Extracted MP3 duration differs from source by"
                       << differenceMs << "ms";
        }
    }
    return true;
}

bool FfmpegUtils::cutClip(const QString &inputPath, qint64 startMs, qint64 endMs,
                          const QString &outputPath, QString *error)
{
    if (endMs <= startMs) {
        if (error) {
            *error = QStringLiteral("切片时间范围无效。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建切片目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }

    const double startSeconds = startMs / 1000.0;
    const double durationSeconds = (endMs - startMs) / 1000.0;

    // 混合 seek：第三方录制/分段拼接的源 MP4 常见「关键帧索引不可信」——直接按
    // -ss 定位时 ffmpeg 可能从一个 P/B 帧开始解码，缺参考帧后输出灰影/重影，直到
    // 下一个真关键帧才恢复，并被重编码进切片。先按输入侧 -ss 回退 15 秒给解码器
    // 留出预解码时间，再在输出侧 -ss 精确定位；起点不足 15 秒时直接从 0 解码。
    // 与 backend/internal/pkg/media/ffmpeg.go 的 buildCutVideoArgs 保持一致。
    constexpr qint64 kCoarseBackMs = 15 * 1000;

    QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
    };
    if (startMs >= kCoarseBackMs) {
        const double backSeconds = (startMs - kCoarseBackMs) / 1000.0;
        const double dropSeconds = kCoarseBackMs / 1000.0;
        args << QStringLiteral("-ss") << QString::number(backSeconds, 'f', 3)
             << QStringLiteral("-i") << inputPath
             << QStringLiteral("-ss") << QString::number(dropSeconds, 'f', 3)
             << QStringLiteral("-t") << QString::number(durationSeconds, 'f', 3);
    } else {
        args << QStringLiteral("-i") << inputPath
             << QStringLiteral("-ss") << QString::number(startSeconds, 'f', 3)
             << QStringLiteral("-t") << QString::number(durationSeconds, 'f', 3);
    }
    args << QStringLiteral("-map") << QStringLiteral("0:v:0")
         << QStringLiteral("-map") << QStringLiteral("0:a:0?")
         << QStringLiteral("-c:v") << QStringLiteral("libx264")
         << QStringLiteral("-preset") << QStringLiteral("veryfast")
         << QStringLiteral("-crf") << QStringLiteral("20")
         << QStringLiteral("-pix_fmt") << QStringLiteral("yuv420p")
         << QStringLiteral("-c:a") << QStringLiteral("aac")
         << QStringLiteral("-b:a") << QStringLiteral("128k")
         << QStringLiteral("-movflags") << QStringLiteral("+faststart")
         << outputPath;

    if (!runFfmpeg(args, 10 * 60 * 1000, error)) {
        return false;
    }

    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("视频裁剪失败，未生成有效文件。");
        }
        return false;
    }
    return true;
}

bool FfmpegUtils::cutKeepRanges(const QString &inputPath,
                                const QVector<QPair<qint64, qint64>> &ranges,
                                const QString &outputPath, QString *error)
{
    // 统一整理区间：去掉空区间、按时间排序并合并重叠部分，保证保留顺序正确。
    QVector<QPair<qint64, qint64>> normalized;
    for (const QPair<qint64, qint64> &range : ranges) {
        const qint64 start = qMax<qint64>(0, range.first);
        const qint64 end = range.second;
        if (end > start) {
            normalized.append({start, end});
        }
    }
    std::sort(normalized.begin(), normalized.end(),
              [](const QPair<qint64, qint64> &left,
                 const QPair<qint64, qint64> &right) {
                  if (left.first != right.first) {
                      return left.first < right.first;
                  }
                  return left.second < right.second;
              });
    QVector<QPair<qint64, qint64>> merged;
    for (const QPair<qint64, qint64> &range : normalized) {
        if (!merged.isEmpty() && range.first <= merged.last().second) {
            merged.last().second = qMax(merged.last().second, range.second);
        } else {
            merged.append(range);
        }
    }
    if (merged.isEmpty()) {
        if (error) {
            *error = QStringLiteral("没有可用的保留区间。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建视频输出目录：%1")
                         .arg(QFileInfo(outputPath).absolutePath());
        }
        return false;
    }

    // 用 select/aselect 只保留目标区间，再按帧序号/采样序号重建连续时间轴。
    // 区间取半开 [start, end)，避免边界处的帧/采样被相邻区间重复包含。
    // 这样一条命令即可完成裁剪 + 拼接，且不会引入逐段编码的累计漂移。
    QStringList terms;
    terms.reserve(merged.size());
    for (const QPair<qint64, qint64> &range : merged) {
        terms.append(QStringLiteral("gte(t,%1)*lt(t,%2)")
                         .arg(range.first / 1000.0, 0, 'f', 3)
                         .arg(range.second / 1000.0, 0, 'f', 3));
    }
    const QString expression = terms.join(QLatin1Char('+'));
    const QString filterGraph =
        QStringLiteral("[0:v:0]select='") + expression
        + QStringLiteral("',setpts=N/FRAME_RATE/TB[v];[0:a:0]aselect='")
        + expression + QStringLiteral("',asetpts=N/SR/TB[a]");

    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-i"),
        inputPath,
        QStringLiteral("-filter_complex"),
        filterGraph,
        QStringLiteral("-map"),
        QStringLiteral("[v]"),
        QStringLiteral("-map"),
        QStringLiteral("[a]"),
        QStringLiteral("-c:v"),
        QStringLiteral("libx264"),
        QStringLiteral("-preset"),
        QStringLiteral("veryfast"),
        QStringLiteral("-crf"),
        QStringLiteral("20"),
        QStringLiteral("-pix_fmt"),
        QStringLiteral("yuv420p"),
        QStringLiteral("-c:a"),
        QStringLiteral("aac"),
        QStringLiteral("-b:a"),
        QStringLiteral("128k"),
        QStringLiteral("-movflags"),
        QStringLiteral("+faststart"),
        outputPath,
    };
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        return false;
    }
    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("视频裁剪拼接失败，未生成有效文件。");
        }
        return false;
    }
    return true;
}

bool FfmpegUtils::audioStreamInfo(const QString &filePath, int *sampleRate,
                                  int *channels, QString *error)
{
    if (sampleRate) {
        *sampleRate = 0;
    }
    if (channels) {
        *channels = 0;
    }

    const QString ffprobe = FfmpegLocator::ffprobePath();
    if (ffprobe.isEmpty()) {
        if (error) {
            *error = QStringLiteral("未找到 ffprobe，请重新安装应用或设置 LIVEMIX_FFMPEG_DIR。");
        }
        return false;
    }

    QProcess process;
    process.setProgram(ffprobe);
    process.setArguments({QStringLiteral("-v"), QStringLiteral("error"),
                          QStringLiteral("-select_streams"), QStringLiteral("a:0"),
                          QStringLiteral("-show_entries"),
                          QStringLiteral("stream=sample_rate,channels"),
                          QStringLiteral("-of"), QStringLiteral("csv=p=0"),
                          filePath});
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(5000) || !process.waitForFinished(15000)) {
        process.kill();
        process.waitForFinished(1000);
        if (error) {
            *error = QStringLiteral("读取音频参数超时。");
        }
        return false;
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        if (error) {
            *error = shortError(QString::fromLocal8Bit(process.readAllStandardError()));
        }
        return false;
    }

    // ffprobe 的 csv 输出形如「44100,2」；没有音轨时为空。
    const QStringList fields =
        QString::fromLatin1(process.readAllStandardOutput()).trimmed()
            .split(QLatin1Char(','));
    bool rateOk = false;
    bool channelsOk = false;
    const int parsedRate = fields.value(0).trimmed().toInt(&rateOk);
    const int parsedChannels = fields.value(1).trimmed().toInt(&channelsOk);
    if (!rateOk || !channelsOk || parsedRate <= 0 || parsedChannels <= 0) {
        if (error) {
            *error = QStringLiteral("视频没有可处理的音轨。");
        }
        return false;
    }
    if (sampleRate) {
        *sampleRate = parsedRate;
    }
    if (channels) {
        *channels = parsedChannels;
    }
    return true;
}

bool FfmpegUtils::decodeAudioF32(const QString &inputPath,
                                 const QString &outputPath, int sampleRate,
                                 int channels, QString *error)
{
    if (sampleRate <= 0 || channels <= 0) {
        if (error) {
            *error = QStringLiteral("音频参数无效。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建降噪临时目录。");
        }
        return false;
    }

    // aresample=first_pts=0 在音轨晚于视频起点时补开头静音，保证时间轴从
    // 视频 t=0 开始。这里不补尾部：垫出来的数字静音会污染噪声谱估计，
    // 尾部对齐交给封装阶段的 apad,atrim。
    const QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-i"),
        inputPath,
        QStringLiteral("-map"),
        QStringLiteral("0:a:0"),
        QStringLiteral("-af"),
        QStringLiteral("aresample=async=1:first_pts=0"),
        QStringLiteral("-ac"),
        QString::number(channels),
        QStringLiteral("-ar"),
        QString::number(sampleRate),
        QStringLiteral("-f"),
        QStringLiteral("f32le"),
        outputPath,
    };
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        return false;
    }
    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("音频解码失败，未生成有效数据。");
        }
        return false;
    }
    return true;
}

bool FfmpegUtils::muxAudioF32(const QString &videoPath,
                              const QString &rawAudioPath, int sampleRate,
                              int channels, qint64 durationMs,
                              const QString &outputPath, QString *error)
{
    if (sampleRate <= 0 || channels <= 0) {
        if (error) {
            *error = QStringLiteral("音频参数无效。");
        }
        return false;
    }
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建降噪输出目录。");
        }
        return false;
    }

    // 画面直接 copy，只重新编码已处理的音轨；口播成片只有一条人声轨。
    QStringList args = {
        QStringLiteral("-y"),
        QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"),
        QStringLiteral("-loglevel"),
        QStringLiteral("error"),
        QStringLiteral("-f"),
        QStringLiteral("f32le"),
        QStringLiteral("-ar"),
        QString::number(sampleRate),
        QStringLiteral("-ac"),
        QString::number(channels),
        QStringLiteral("-i"),
        rawAudioPath,
        QStringLiteral("-i"),
        videoPath,
        QStringLiteral("-map"),
        QStringLiteral("0:a:0"),
        QStringLiteral("-map"),
        QStringLiteral("1:v:0?"),
        QStringLiteral("-c:v"),
        QStringLiteral("copy"),
    };
    // 尾部静音在这里补齐（而不是解码前），降噪 DSP 只看到真实人声。
    // 用音频滤镜 atrim 截到视频时长，而不是输出级 -t：后者在容器时长
    // 略短于视频流时会连最后一帧画面一起裁掉。
    if (durationMs > 0) {
        const QString alignAudio =
            QStringLiteral("apad,atrim=start=0:end=%1")
                .arg(QString::number(durationMs / 1000.0, 'f', 6));
        args << QStringLiteral("-af") << alignAudio;
    }
    args << QStringLiteral("-c:a") << QStringLiteral("aac")
         << QStringLiteral("-b:a") << QStringLiteral("192k")
         << QStringLiteral("-movflags") << QStringLiteral("+faststart")
         << outputPath;
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        return false;
    }
    const QFileInfo output(outputPath);
    if (!output.exists() || output.size() <= 0) {
        if (error) {
            *error = QStringLiteral("降噪封装失败，未生成有效文件。");
        }
        return false;
    }
    return true;
}

bool FfmpegUtils::mediaDurationMs(const QString &filePath, qint64 *durationMs)
{
    const QString ffprobe = FfmpegLocator::ffprobePath();
    if (ffprobe.isEmpty()) {
        return false;
    }

    QProcess process;
    process.setProgram(ffprobe);
    process.setArguments({QStringLiteral("-v"), QStringLiteral("error"),
                          QStringLiteral("-show_entries"), QStringLiteral("format=duration"),
                          QStringLiteral("-of"), QStringLiteral("default=nw=1:nk=1"),
                          filePath});
#ifdef Q_OS_WIN
    process.setCreateProcessArgumentsModifier(
        [](QProcess::CreateProcessArguments *args) {
            args->flags |= CREATE_NO_WINDOW;
        });
#endif
    process.start();
    if (!process.waitForStarted(5000) || !process.waitForFinished(30000)) {
        process.kill();
        process.waitForFinished(1000);
        return false;
    }
    if (process.exitStatus() != QProcess::NormalExit || process.exitCode() != 0) {
        return false;
    }

    bool ok = false;
    const double seconds =
        QString::fromLatin1(process.readAllStandardOutput()).trimmed().toDouble(&ok);
    if (!ok || seconds <= 0.0) {
        return false;
    }
    if (durationMs) {
        *durationMs = qRound64(seconds * 1000.0);
    }
    return true;
}

bool FfmpegUtils::extractThumbnail(const QString &inputPath, qint64 positionMs,
                                   const QString &outputPath, QString *error)
{
    if (!QDir().mkpath(QFileInfo(outputPath).absolutePath())) {
        if (error) {
            *error = QStringLiteral("无法创建预览图目录。");
        }
        return false;
    }

    const double seconds = qMax<qint64>(0, positionMs) / 1000.0;
    const QStringList args = {
        QStringLiteral("-y"),           QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
        QStringLiteral("error"),        QStringLiteral("-ss"),
        QString::number(seconds, 'f', 3),
        QStringLiteral("-i"),           inputPath,
        QStringLiteral("-frames:v"),    QStringLiteral("1"),
        QStringLiteral("-vf"),          QStringLiteral("scale=640:-2"),
        outputPath,
    };
    if (!runFfmpeg(args, 30000, error)) {
        return false;
    }
    return QFileInfo::exists(outputPath) && QFileInfo(outputPath).size() > 0;
}

QString FfmpegUtils::thumbnailCachePath(const QString &videoPath)
{
    return previewCachePath(videoPath, QStringLiteral(".png"));
}

QString FfmpegUtils::posterFrameCachePath(const QString &videoPath)
{
    // 与素材缩略图使用同一路径哈希，靠 -poster 后缀区分 0 秒帧与 0.5 秒帧。
    return previewCachePath(videoPath, QStringLiteral("-poster.png"));
}

QString FfmpegUtils::hlsPreviewCachePath(const QString &videoPath)
{
    // 文件名带 full 标记：旧版本缓存只含前 5 分钟，不能复用。
    return previewCachePath(videoPath, QStringLiteral("-hls-full.mp4"));
}

QString FfmpegUtils::hlsPreviewHeadCachePath(const QString &videoPath)
{
    return previewCachePath(videoPath, QStringLiteral("-hls-head60.mp4"));
}

QString FfmpegUtils::recordingMp4Path(const QString &playlistPath)
{
    if (!playlistPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        return playlistPath;
    }
    return QDir(QFileInfo(playlistPath).absolutePath())
        .filePath(QStringLiteral("index.mp4"));
}

bool FfmpegUtils::ensureRecordingMp4(const QString &playlistPath, QString *mp4Path,
                                     QString *error)
{
    const QString outputPath = recordingMp4Path(playlistPath);
    if (playlistPath.isEmpty()) {
        if (error) {
            *error = QStringLiteral("源视频路径为空。");
        }
        return false;
    }
    if (!playlistPath.endsWith(QLatin1String(".m3u8"), Qt::CaseInsensitive)) {
        if (mp4Path) {
            *mp4Path = outputPath;
        }
        return true;
    }

    QFile playlistFile(playlistPath);
    if (!playlistFile.open(QIODevice::ReadOnly)) {
        if (error) {
            *error = QStringLiteral("无法读取 HLS 播放列表：%1").arg(playlistPath);
        }
        return false;
    }
    const QByteArray playlistData = playlistFile.readAll();
    playlistFile.close();
    const bool playlistComplete = playlistData.contains("#EXT-X-ENDLIST");
    const QFileInfo playlistInfo(playlistPath);
    const qint64 playlistMs = MediaUtils::hlsPlaylistDurationMs(playlistPath);

    // 正常完成的录像允许少量取整误差；意外中断的录像最后一段可能被截断，
    // 放宽到一个分片多一点，保证恢复出来的内容仍可成片。
    const qint64 toleranceMs = playlistComplete ? 2000 : 6000;

    auto isUsableMp4 = [&](const QString &path) {
        const QFileInfo info(path);
        if (!info.isFile() || info.size() <= 0) {
            return false;
        }
        // MP4 必须晚于播放列表生成，避免复用录制追加前的旧文件。
        if (info.lastModified() < playlistInfo.lastModified()) {
            return false;
        }
        qint64 durationMs = 0;
        if (!mediaDurationMs(path, &durationMs) || durationMs <= 0) {
            return false;
        }
        return playlistMs <= 0 || durationMs + toleranceMs >= playlistMs;
    };

    if (isUsableMp4(outputPath)) {
        qInfo() << "Reusing complete recording MP4:" << outputPath;
        if (mp4Path) {
            *mp4Path = outputPath;
        }
        return true;
    }

    // 之前预览生成的完整 MP4 缓存可以直接转正，避免同一录像存两份大文件。
    const QString cachePath = hlsPreviewCachePath(playlistPath);
    if (isUsableMp4(cachePath)) {
        QString promoteError;
        if (promoteFile(cachePath, outputPath, &promoteError)) {
            qInfo() << "Reused preview MP4 cache as recording MP4:" << outputPath;
            if (mp4Path) {
                *mp4Path = outputPath;
            }
            return true;
        }
        // 缓存本身可能正被预览播放器占用，仍可走重新合成兜底。
        qWarning() << "Unable to move preview MP4 cache, falling back to remux:"
                   << cachePath << promoteError;
    }

    // 上次改名失败留下的完整 MP4（内容已校验过）：直接重试改名即可，避免
    // 每重试一次就重新转封装数小时的素材。既然内容有效，改名失败通常不是
    // 文件本身的问题，重新合成只会再烧一遍时间，因此这里直接报错。
    const QDir recordingDir(playlistInfo.absolutePath());
    const QStringList leftoverNames =
        recordingDir.entryList({QStringLiteral("index-*.part.mp4")}, QDir::Files);
    for (const QString &name : leftoverNames) {
        const QString partPath = recordingDir.filePath(name);
        if (!isUsableMp4(partPath)) {
            continue;
        }
        qInfo() << "Retrying save of leftover complete MP4 part:" << partPath;
        if (!promoteFile(partPath, outputPath, error)) {
            qWarning() << "Unable to save complete MP4 from leftover part:" << partPath
                       << (error ? *error : QString());
            return false;
        }
        QFile::setPermissions(outputPath,
                              QFile::ReadOwner | QFile::WriteOwner
                                  | QFile::ReadGroup | QFile::ReadOther);
        qInfo() << "Reused leftover MP4 part as recording MP4:" << outputPath;
        if (mp4Path) {
            *mp4Path = outputPath;
        }
        return true;
    }

    // 中断的录像可能缺少 ENDLIST；合成前补一份临时列表，避免末尾分片被丢弃。
    QString playlistForRemux = playlistPath;
    QTemporaryFile endedPlaylist(playlistInfo.absolutePath()
                                 + QStringLiteral("/.livemix-remux-XXXXXX.m3u8"));
    if (!playlistComplete) {
        if (!endedPlaylist.open()) {
            if (error) {
                *error = QStringLiteral("无法创建临时播放列表：%1")
                             .arg(endedPlaylist.errorString());
            }
            return false;
        }
        endedPlaylist.write(playlistData);
        if (!playlistData.endsWith('\n')) {
            endedPlaylist.write("\n");
        }
        endedPlaylist.write("#EXT-X-ENDLIST\n");
        endedPlaylist.close();
        playlistForRemux = endedPlaylist.fileName();
    }

    // 上面未能复用的半成品视为失效，清理超过 1 小时的，避免长期占用双份磁盘空间。
    const QDateTime staleBefore = QDateTime::currentDateTime().addSecs(-3600);
    const QFileInfoList staleParts = QDir(playlistInfo.absolutePath())
        .entryInfoList({QStringLiteral("index-*.part.mp4")}, QDir::Files);
    for (const QFileInfo &part : staleParts) {
        if (part.lastModified() < staleBefore) {
            QFile::remove(part.absoluteFilePath());
        }
    }

    QTemporaryFile tempOutput(playlistInfo.absolutePath()
                              + QStringLiteral("/index-XXXXXX.part.mp4"));
    if (!tempOutput.open()) {
        if (error) {
            *error = QStringLiteral("无法创建 MP4 临时文件：%1")
                         .arg(tempOutput.errorString());
        }
        return false;
    }
    const QString tempPath = tempOutput.fileName();
    tempOutput.close();

    const QStringList args = {
        QStringLiteral("-y"),          QStringLiteral("-nostdin"),
        QStringLiteral("-hide_banner"), QStringLiteral("-loglevel"),
        QStringLiteral("error"),       QStringLiteral("-i"),
        playlistForRemux,              QStringLiteral("-map"),
        QStringLiteral("0:v:0"),       QStringLiteral("-map"),
        QStringLiteral("0:a:0?"),      QStringLiteral("-c"),
        QStringLiteral("copy"),        QStringLiteral("-f"),
        QStringLiteral("mp4"),         QStringLiteral("-movflags"),
        QStringLiteral("+faststart"),  tempPath,
    };
    if (!runFfmpeg(args, 30 * 60 * 1000, error)) {
        QFile::remove(tempPath);
        return false;
    }

    qint64 outputMs = 0;
    if (!mediaDurationMs(tempPath, &outputMs) || outputMs <= 0) {
        QFile::remove(tempPath);
        if (error) {
            *error = QStringLiteral("MP4 合成失败：输出文件不可用。");
        }
        return false;
    }
    if (playlistMs > 0 && outputMs + toleranceMs < playlistMs) {
        QFile::remove(tempPath);
        if (error) {
            *error = QStringLiteral("MP4 合成不完整：输出 %1ms，播放列表 %2ms。")
                         .arg(outputMs)
                         .arg(playlistMs);
        }
        return false;
    }

    // 校验通过：关闭 QTemporaryFile 的自动删除。改名失败时保留这份成品，
    // 下次重试会复用并只重试改名，不必重新转封装整段录像。
    tempOutput.setAutoRemove(false);
    if (!promoteFile(tempPath, outputPath, error)) {
        qWarning() << "Unable to save complete MP4, keeping part for retry:"
                   << tempPath;
        return false;
    }
    // QTemporaryFile 默认 0600；完整 MP4 作为用户可见素材，恢复为常规读权限。
    QFile::setPermissions(outputPath,
                          QFile::ReadOwner | QFile::WriteOwner
                              | QFile::ReadGroup | QFile::ReadOther);

    qInfo() << "Recording synthesized to MP4:" << outputPath;
    if (mp4Path) {
        *mp4Path = outputPath;
    }
    return true;
}

bool FfmpegUtils::videoSize(const QString &filePath, int *width, int *height)
{
    const ProbeInfo probe = MediaProbe::probe(filePath);
    if (!probe.ok) {
        return false;
    }

    const QJsonObject root =
        QJsonDocument::fromJson(probe.json.toUtf8()).object();
    const QJsonArray streams = root.value(QStringLiteral("streams")).toArray();
    for (const QJsonValue &value : streams) {
        const QJsonObject stream = value.toObject();
        if (stream.value(QStringLiteral("codec_type")).toString()
            != QLatin1String("video")) {
            continue;
        }
        const QJsonObject disposition =
            stream.value(QStringLiteral("disposition")).toObject();
        if (disposition.value(QStringLiteral("attached_pic")).toInt() == 1) {
            continue;
        }
        const int streamWidth = stream.value(QStringLiteral("width")).toInt();
        const int streamHeight = stream.value(QStringLiteral("height")).toInt();
        if (streamWidth > 0 && streamHeight > 0) {
            if (width) {
                *width = streamWidth;
            }
            if (height) {
                *height = streamHeight;
            }
            return true;
        }
    }
    return false;
}
