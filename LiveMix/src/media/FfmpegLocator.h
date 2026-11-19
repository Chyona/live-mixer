#pragma once

#include <QString>

namespace FfmpegLocator {

// Resolves a bundled ffmpeg/ffprobe first, then a build-time override and PATH.
QString ffmpegPath();
QString ffprobePath();

QString locate(const QString &executableBaseName);

} // namespace FfmpegLocator
