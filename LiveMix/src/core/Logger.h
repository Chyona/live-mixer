#pragma once

#include <QString>

namespace Logger {

// Writes Qt log messages to yyyy-MM-dd.log, one file per day, keeping the
// newest 30 files. The directory follows platform conventions:
//   macOS:   ~/Library/Logs/<app name>/
//   Windows: <AppLocalDataLocation>/logs/
//   other:   <application dir>/logs/
// LIVEMIX_LOG_DIR overrides the location. Messages are also mirrored to
// stderr.
//
// Call once, after QCoreApplication is constructed. Returns false when the
// log directory cannot be created or written; messages then only reach
// stderr, and file logging is retried when the date rolls over.
bool install();

// Directory holding the daily log files. Empty before install().
QString logDirectory();

} // namespace Logger
