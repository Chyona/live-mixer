#pragma once

#include <QString>

namespace AutoStart {

bool isSupported();

// Enables/disables launching the current application at user login.
// macOS uses a LaunchAgent; Windows uses the current-user Run key.
bool setEnabled(bool enabled, QString *error = nullptr);

} // namespace AutoStart
