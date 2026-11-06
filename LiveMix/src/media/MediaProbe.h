#pragma once

#include "../data/MediaTypes.h"

#include <QString>

namespace MediaProbe {

// Blocking ffprobe call with a bounded timeout. Callers should show a wait
// cursor when invoking this from the UI thread.
ProbeInfo probe(const QString &filePath, int timeoutMs = 15000);

} // namespace MediaProbe
