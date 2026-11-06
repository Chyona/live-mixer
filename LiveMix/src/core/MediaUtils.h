#pragma once

#include <QString>
#include <QtGlobal>

namespace MediaUtils {

// Formats milliseconds as MM:SS, or H:MM:SS when the duration reaches an hour.
QString formatDuration(qint64 milliseconds);

// Formats a byte count as KB / MB / GB, matching the download UI.
QString formatByteSize(qint64 bytes);

// True when path is root itself or lives inside root.
bool isWithinDirectory(const QString &root, const QString &path);

// Sums regular files recursively. Returns 0 when the path is missing.
qint64 directorySize(const QString &path);

// Sums #EXTINF durations in an HLS playlist. Returns 0 when unavailable.
qint64 hlsPlaylistDurationMs(const QString &playlistPath);

// Produces a filesystem-safe single path component, capped at maxLength.
QString sanitizeFileComponent(const QString &value, int maxLength = 48);

} // namespace MediaUtils
