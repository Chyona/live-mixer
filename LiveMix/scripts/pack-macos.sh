#!/usr/bin/env bash
#
# LiveMix Studio — macOS DMG packaging helper.
#
# Builds a Release app, deploys Qt frameworks/plugins, then creates a
# drag-to-Applications disk image with a branded installer window.
# Requires macOS and Xcode Command Line Tools.
#
# Examples:
#   ./scripts/pack-macos.sh
#   ./scripts/pack-macos.sh --skip-build
#   ./scripts/pack-macos.sh --clean --jobs 8
#   ./scripts/pack-macos.sh --sign "Developer ID Application: Example (TEAMID)"
#   ./scripts/pack-macos.sh --no-dmg-style
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$ROOT_DIR/build-dmg"
OUTPUT_DIR="$ROOT_DIR/dist"
SKIP_BUILD=0
CLEAN=0
KEEP_STAGING=0
JOBS=""
QT_PREFIX=""
FFMPEG_DIR="${LIVEMIX_FFMPEG_DIR:-}"
SIGN_IDENTITY="${CODESIGN_IDENTITY:-}"
EXTRA_BUILD_ARGS=()
VOLUME_NAME="LiveMix Studio"
APP_VERSION_OVERRIDE=""
DMG_STYLED=1

# 安装窗口布局（单位 pt）：背景图 640×400，图标中心与背景箭头对齐
DMG_WINDOW_WIDTH=640
DMG_WINDOW_HEIGHT=400
DMG_WINDOW_X=200
DMG_WINDOW_Y=120
DMG_TITLEBAR_HEIGHT=28
DMG_ICON_SIZE=120
DMG_TEXT_SIZE=13
DMG_APP_ICON_X=168
DMG_APP_ICON_Y=190
DMG_APPS_ICON_X=472
DMG_APPS_ICON_Y=190
DMG_BACKGROUND_DIR="$ROOT_DIR/resources/dmg"
DMG_VOLUME_ICON="$ROOT_DIR/resources/AppIcon.icns"

usage() {
    cat <<'EOF'
LiveMix Studio DMG 打包脚本（macOS）

用法:
  ./scripts/pack-macos.sh [选项] [-- <构建脚本额外参数>]

流程:
  1. 编译 Release 版本（可通过 --skip-build 跳过）
  2. 复制 .app 并用 macdeployqt 部署 Qt 运行库
  3. 创建 Applications 快捷方式，套用品牌背景与图标布局后生成压缩 DMG

选项:
  --skip-build           复用构建目录中已有的 LiveMixStudio.app
  --clean                编译前清理构建目录
  -j, --jobs N           并行编译线程数
  --qt PATH              指定 Qt 6 安装前缀
  --ffmpeg-dir DIR       ffmpeg/ffprobe 所在目录
                         （默认: third_party/ffmpeg/macos-<arch>/bin 或 LIVEMIX_FFMPEG_DIR）
  --version X.Y.Z        覆盖应用版本号（默认取构建时的版本 tag，否则 1.0.0）
  --build-dir DIR        构建目录（默认: build-dmg）
  --output DIR           DMG 输出目录（默认: dist）
  --sign IDENTITY        codesign 签名身份；默认使用 ad-hoc 临时签名
  --no-dmg-style         跳过安装窗口美化（品牌背景、图标布局、卷图标）
  --keep-staging         保留临时部署目录，便于排查问题
  -h, --help             显示帮助

示例:
  ./scripts/pack-macos.sh
  ./scripts/pack-macos.sh --clean
  ./scripts/pack-macos.sh --skip-build
  ./scripts/pack-macos.sh --sign "Developer ID Application: Company (TEAMID)"
EOF
}

info() {
    printf '\n==> %s\n' "$1"
}

fail() {
    printf '错误: %s\n' "$1" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    arg="$1"
    case "$arg" in
        --skip-build)
            SKIP_BUILD=1
            ;;
        --clean)
            CLEAN=1
            ;;
        -j|--jobs)
            [ "$#" -ge 2 ] || fail "$arg 需要一个参数"
            JOBS="$2"
            shift
            ;;
        --jobs=*)
            JOBS="${arg#*=}"
            ;;
        --qt)
            [ "$#" -ge 2 ] || fail "--qt 需要一个路径"
            QT_PREFIX="$2"
            shift
            ;;
        --qt=*)
            QT_PREFIX="${arg#*=}"
            ;;
        --ffmpeg-dir)
            [ "$#" -ge 2 ] || fail "--ffmpeg-dir 需要一个路径"
            FFMPEG_DIR="$2"
            shift
            ;;
        --ffmpeg-dir=*)
            FFMPEG_DIR="${arg#*=}"
            ;;
        --version)
            [ "$#" -ge 2 ] || fail "--version 需要一个版本号"
            APP_VERSION_OVERRIDE="$2"
            shift
            ;;
        --version=*)
            APP_VERSION_OVERRIDE="${arg#*=}"
            ;;
        --build-dir)
            [ "$#" -ge 2 ] || fail "--build-dir 需要一个路径"
            BUILD_DIR="$2"
            shift
            ;;
        --build-dir=*)
            BUILD_DIR="${arg#*=}"
            ;;
        --output)
            [ "$#" -ge 2 ] || fail "--output 需要一个路径"
            OUTPUT_DIR="$2"
            shift
            ;;
        --output=*)
            OUTPUT_DIR="${arg#*=}"
            ;;
        --sign)
            [ "$#" -ge 2 ] || fail "--sign 需要一个签名身份"
            SIGN_IDENTITY="$2"
            shift
            ;;
        --sign=*)
            SIGN_IDENTITY="${arg#*=}"
            ;;
        --no-dmg-style)
            DMG_STYLED=0
            ;;
        --keep-staging)
            KEEP_STAGING=1
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            while [ "$#" -gt 0 ]; do
                EXTRA_BUILD_ARGS+=("$1")
                shift
            done
            break
            ;;
        *)
            printf '错误: 未知参数 %s\n\n' "$arg" >&2
            usage >&2
            exit 1
            ;;
    esac
    shift
done

if [ -n "$JOBS" ]; then
    case "$JOBS" in
        ''|*[!0-9]*) fail "--jobs 必须是正整数" ;;
    esac
    [ "$JOBS" -gt 0 ] || fail "--jobs 必须是正整数"
fi

if [ -n "$APP_VERSION_OVERRIDE" ]; then
    APP_VERSION_OVERRIDE="${APP_VERSION_OVERRIDE#v}"
    [[ "$APP_VERSION_OVERRIDE" =~ ^[0-9]+(\.[0-9]+){0,3}$ ]] \
        || fail "--version 必须是数字版本号，如 1.2.3（当前: ${APP_VERSION_OVERRIDE}）"
    EXTRA_BUILD_ARGS+=("-DLIVEMIX_APP_VERSION=$APP_VERSION_OVERRIDE")
fi

[ "$(uname -s)" = "Darwin" ] || fail "DMG 打包仅支持 macOS。"

for tool in cmake macdeployqt hdiutil codesign ditto lipo shasum; do
    command -v "$tool" >/dev/null 2>&1 || fail "缺少所需工具: $tool"
done
if [ "$DMG_STYLED" -eq 1 ]; then
    for tool in osascript SetFile; do
        command -v "$tool" >/dev/null 2>&1 || fail "缺少所需工具: $tool（安装界面美化需要 Xcode Command Line Tools）"
    done
fi

case "$BUILD_DIR" in
    /*) ;;
    *) BUILD_DIR="$ROOT_DIR/$BUILD_DIR" ;;
esac
case "$OUTPUT_DIR" in
    /*) ;;
    *) OUTPUT_DIR="$ROOT_DIR/$OUTPUT_DIR" ;;
esac

if [ -z "$FFMPEG_DIR" ]; then
    case "$(uname -m)" in
        arm64|aarch64) ffmpeg_platform="macos-arm64" ;;
        x86_64) ffmpeg_platform="macos-x64" ;;
        *) ffmpeg_platform="" ;;
    esac
    if [ -n "$ffmpeg_platform" ] \
        && [ -d "$ROOT_DIR/third_party/ffmpeg/$ffmpeg_platform/bin" ]; then
        FFMPEG_DIR="$ROOT_DIR/third_party/ffmpeg/$ffmpeg_platform/bin"
    fi
fi
if [ -n "$FFMPEG_DIR" ]; then
    case "$FFMPEG_DIR" in
        /*) ;;
        *) FFMPEG_DIR="$ROOT_DIR/$FFMPEG_DIR" ;;
    esac
    for tool in ffmpeg ffprobe; do
        [ -x "$FFMPEG_DIR/$tool" ] \
            || fail "--ffmpeg-dir 缺少可执行文件: $FFMPEG_DIR/$tool"
    done
fi
if [ "$SKIP_BUILD" -eq 0 ] && [ -z "$FFMPEG_DIR" ]; then
    fail "未找到 ffmpeg。请先运行 ./scripts/fetch-ffmpeg.sh，或用 --ffmpeg-dir 指定包含 ffmpeg/ffprobe 的目录。"
fi

if [ "$SKIP_BUILD" -eq 0 ]; then
    info "编译 Release 应用"
    build_cmd=("$SCRIPT_DIR/build.sh" release --build-dir "$BUILD_DIR")
    if [ "$CLEAN" -eq 1 ]; then
        build_cmd+=(--clean)
    fi
    if [ -n "$JOBS" ]; then
        build_cmd+=(-j "$JOBS")
    fi
    if [ -n "$QT_PREFIX" ]; then
        build_cmd+=(--qt "$QT_PREFIX")
    fi
    if [ -n "$FFMPEG_DIR" ]; then
        build_cmd+=(--ffmpeg-dir "$FFMPEG_DIR")
    fi
    if [ "${#EXTRA_BUILD_ARGS[@]}" -gt 0 ]; then
        build_cmd+=(--)
        build_cmd+=("${EXTRA_BUILD_ARGS[@]}")
    fi
    "${build_cmd[@]}"
fi

APP_PATH=""
for candidate in \
    "$BUILD_DIR/LiveMixStudio.app" \
    "$BUILD_DIR/Release/LiveMixStudio.app" \
    "$BUILD_DIR/Debug/LiveMixStudio.app"; do
    if [ -d "$candidate" ]; then
        APP_PATH="$candidate"
        break
    fi
done
if [ -z "$APP_PATH" ]; then
    APP_PATH="$(find "$BUILD_DIR" -maxdepth 4 -type d -name LiveMixStudio.app -print -quit 2>/dev/null || true)"
fi
[ -n "$APP_PATH" ] && [ -d "$APP_PATH" ] || fail "未找到 LiveMixStudio.app，请先编译或检查 --build-dir。"
[ -x "$APP_PATH/Contents/MacOS/LiveMixStudio" ] || fail "应用包缺少 LiveMixStudio 可执行文件: $APP_PATH"

INFO_PLIST="$APP_PATH/Contents/Info.plist"
APP_VERSION=""
if [ -f "$INFO_PLIST" ]; then
    APP_VERSION="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "$INFO_PLIST" 2>/dev/null || true)"
fi
[ -n "$APP_VERSION" ] || APP_VERSION="1.0.0"

if [ -n "$APP_VERSION_OVERRIDE" ] && [ "$APP_VERSION" != "$APP_VERSION_OVERRIDE" ]; then
    if [ "$SKIP_BUILD" -eq 1 ]; then
        printf '警告: --skip-build 无法更改已编译应用的版本，继续使用 %s（忽略 --version %s）。\n' \
            "$APP_VERSION" "$APP_VERSION_OVERRIDE" >&2
    else
        fail "构建产物版本为 ${APP_VERSION}，与 --version ${APP_VERSION_OVERRIDE} 不一致。"
    fi
fi

ARCHS="$(lipo -archs "$APP_PATH/Contents/MacOS/LiveMixStudio" 2>/dev/null || true)"
if [ -z "$ARCHS" ]; then
    ARCH_LABEL="$(uname -m)"
else
    case "$ARCHS" in
        *" "*) ARCH_LABEL="universal" ;;
        *) ARCH_LABEL="$ARCHS" ;;
    esac
fi

mkdir -p "$OUTPUT_DIR"
DMG_PATH="$OUTPUT_DIR/LiveMixStudio-${APP_VERSION}-${ARCH_LABEL}-setup.dmg"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/livemix-dmg.XXXXXX")"
STAGING_DIR="$WORK_DIR/dmg-root"
LOG_DIR="$WORK_DIR/logs"
MOUNT_POINT=""
mkdir -p "$STAGING_DIR" "$LOG_DIR"
cleanup() {
    if [ -n "${MOUNT_POINT:-}" ] && [ -d "$MOUNT_POINT" ]; then
        hdiutil detach "$MOUNT_POINT" >/dev/null 2>&1 \
            || hdiutil detach -force "$MOUNT_POINT" >/dev/null 2>&1 || true
        MOUNT_POINT=""
    fi
    if [ -n "${WORK_DIR:-}" ] && [ -d "$WORK_DIR" ]; then
        if [ "$KEEP_STAGING" -eq 1 ]; then
            printf '\n临时目录已保留: %s\n' "$WORK_DIR"
        else
            rm -rf "$WORK_DIR"
        fi
    fi
}
trap cleanup EXIT

info "复制应用包"
STAGED_APP="$STAGING_DIR/LiveMixStudio.app"
ditto "$APP_PATH" "$STAGED_APP"

info "部署 Qt 运行库和插件"
DEPLOY_LOG="$LOG_DIR/macdeployqt.log"
# Qt 6.11 起 macdeployqt 默认对应用做 ad-hoc 签名，并提供 -no-codesign；
# 更早的版本（如 6.8）本身不签名，且会把该参数当作未知参数直接报错。
# 最终签名统一由下面的 codesign 步骤完成，因此仅当工具支持时才传入该参数。
MACDEPLOYQT_USAGE="$(macdeployqt 2>&1 || true)"
deploy_args=("$STAGED_APP" -always-overwrite -verbose=1)
case "$MACDEPLOYQT_USAGE" in
    *-no-codesign*) deploy_args+=(-no-codesign) ;;
esac
if ! macdeployqt "${deploy_args[@]}" >"$DEPLOY_LOG" 2>&1; then
    cat "$DEPLOY_LOG" >&2
    fail "macdeployqt 部署失败。"
fi

# macdeployqt deploys every optional plugin it finds. Keep imageformats: the
# import dialog and video library save poster frames as PNG. Input contexts can
# pull in Qt Virtual Keyboard, so they are removed.
rm -rf "$STAGED_APP/Contents/PlugIns/iconengines" \
       "$STAGED_APP/Contents/PlugIns/platforminputcontexts"
for framework in QtQml QtQmlMeta QtQmlModels QtQmlWorkerScript QtQuick; do
    rm -rf "$STAGED_APP/Contents/Frameworks/${framework}.framework"
done

[ -f "$STAGED_APP/Contents/PlugIns/platforms/libqcocoa.dylib" ] \
    || fail "Qt Cocoa 平台插件未部署成功。"
[ -d "$STAGED_APP/Contents/Frameworks/QtCore.framework" ] \
    || fail "Qt 基础框架未部署成功。"
[ -d "$STAGED_APP/Contents/Frameworks/QtSql.framework" ] \
    || fail "Qt SQL 框架未部署成功。"
[ -d "$STAGED_APP/Contents/Frameworks/QtNetwork.framework" ] \
    || fail "Qt Network 框架未部署成功。"
[ -d "$STAGED_APP/Contents/Frameworks/QtMultimedia.framework" ] \
    || fail "Qt Multimedia 框架未部署成功。"
[ -d "$STAGED_APP/Contents/Frameworks/QtMultimediaWidgets.framework" ] \
    || fail "Qt MultimediaWidgets 框架未部署成功。"
[ -f "$STAGED_APP/Contents/PlugIns/sqldrivers/libqsqlite.dylib" ] \
    || fail "Qt SQLite 驱动未部署成功。"
[ -d "$STAGED_APP/Contents/PlugIns/imageformats" ] \
    || fail "Qt imageformats 插件未部署成功（视频封面需要）。"
[ -n "$(ls -A "$STAGED_APP/Contents/PlugIns/multimedia" 2>/dev/null)" ] \
    || fail "Qt Multimedia 后端插件未部署成功。"

for tool in ffmpeg ffprobe; do
    [ -x "$STAGED_APP/Contents/Helpers/$tool" ] \
        || fail "应用包缺少 ffmpeg 组件: Contents/Helpers/$tool（请用 --ffmpeg-dir 重新构建）"
done

info "签名应用"
SIGN_LOG="$LOG_DIR/codesign.log"
sign_identity="${SIGN_IDENTITY:--}"
sign_args=(--force --sign "$sign_identity")
if [ "$sign_identity" != "-" ]; then
    sign_args+=(--options runtime --timestamp)
fi
for helper in "$STAGED_APP/Contents/Helpers/ffmpeg" \
              "$STAGED_APP/Contents/Helpers/ffprobe"; do
    if ! codesign "${sign_args[@]}" "$helper" >>"$SIGN_LOG" 2>&1; then
        cat "$SIGN_LOG" >&2
        fail "ffmpeg 组件签名失败: $helper"
    fi
done
if ! codesign "${sign_args[@]}" --deep "$STAGED_APP" >>"$SIGN_LOG" 2>&1; then
    cat "$SIGN_LOG" >&2
    fail "应用签名失败，请检查签名身份: $sign_identity"
fi
codesign --verify --deep --strict "$STAGED_APP" >/dev/null 2>&1 \
    || fail "应用签名校验失败。"

for tool in ffmpeg ffprobe; do
    if ! "$STAGED_APP/Contents/Helpers/$tool" -version >/dev/null 2>&1; then
        fail "内置 $tool 无法运行，请检查二进制架构是否匹配。"
    fi
done

ln -s /Applications "$STAGING_DIR/Applications"
[ -L "$STAGING_DIR/Applications" ] || fail "无法创建 Applications 快捷方式。"

if [ "$DMG_STYLED" -eq 1 ]; then
    for asset in \
        "$DMG_BACKGROUND_DIR/background.png" \
        "$DMG_BACKGROUND_DIR/background@2x.png" \
        "$DMG_VOLUME_ICON"; do
        [ -f "$asset" ] || fail "缺少安装界面素材: $asset"
    done
    mkdir -p "$STAGING_DIR/.background"
    cp "$DMG_BACKGROUND_DIR/background.png" "$STAGING_DIR/.background/"
    cp "$DMG_BACKGROUND_DIR/background@2x.png" "$STAGING_DIR/.background/"
fi

info "生成 DMG"
CREATE_LOG="$LOG_DIR/hdiutil-create.log"
VERIFY_LOG="$LOG_DIR/hdiutil-verify.log"
rm -f "$DMG_PATH"

create_plain_dmg() {
    hdiutil create \
        -volname "$VOLUME_NAME" \
        -srcfolder "$STAGING_DIR" \
        -fs HFS+ \
        -format UDZO \
        -imagekey zlib-level=9 \
        -ov \
        "$DMG_PATH" >"$CREATE_LOG" 2>&1
}

STYLED_RW_DMG="$WORK_DIR/LiveMixStudio-styled-rw.dmg"

detach_styled_image() {
    [ -n "${MOUNT_POINT:-}" ] || return 0
    local attempt
    for attempt in 1 2 3 4 5; do
        if hdiutil detach "$MOUNT_POINT" >>"$CREATE_LOG" 2>&1; then
            MOUNT_POINT=""
            return 0
        fi
        sleep 1
    done
    printf '警告: 无法正常卸载 %s，尝试强制卸载。\n' "$MOUNT_POINT" >&2
    hdiutil detach -force "$MOUNT_POINT" >>"$CREATE_LOG" 2>&1 || true
    MOUNT_POINT=""
    return 0
}

# 创建读写中间镜像，挂载后用 Finder 写入窗口外观（.DS_Store），
# 再压缩为只读 UDZO。Finder 自动化不可用时退回默认样式，避免阻塞发布。
create_styled_dmg() {
    hdiutil create \
        -volname "$VOLUME_NAME" \
        -srcfolder "$STAGING_DIR" \
        -fs HFS+ \
        -format UDRW \
        -ov \
        "$STYLED_RW_DMG" >>"$CREATE_LOG" 2>&1 || return 1

    local attach_output
    attach_output="$(hdiutil attach -nobrowse -noautoopen -readwrite "$STYLED_RW_DMG" 2>>"$CREATE_LOG")" || return 1
    MOUNT_POINT="$(printf '%s\n' "$attach_output" | awk -F'\t' '
        NF >= 3 && $1 ~ /^\/dev\// {
            mp = $3
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", mp)
            if (mp != "") { print mp; exit }
        }')"
    [ -n "$MOUNT_POINT" ] && [ -d "$MOUNT_POINT" ] || return 1

    local volume_name escaped_volume window_right window_bottom
    volume_name="$(basename "$MOUNT_POINT")"
    escaped_volume="${volume_name//\\/\\\\}"
    escaped_volume="${escaped_volume//\"/\\\"}"
    window_right=$((DMG_WINDOW_X + DMG_WINDOW_WIDTH))
    window_bottom=$((DMG_WINDOW_Y + DMG_WINDOW_HEIGHT + DMG_TITLEBAR_HEIGHT))

    osascript >>"$CREATE_LOG" 2>&1 <<APPLESCRIPT || return 1
tell application "Finder"
    tell disk "$escaped_volume"
        open
        set current view of container window to icon view
        set toolbar visible of container window to false
        set statusbar visible of container window to false
        set pathbar visible of container window to false
        set the bounds of container window to {$DMG_WINDOW_X, $DMG_WINDOW_Y, $window_right, $window_bottom}
        set vo to the icon view options of container window
        set arrangement of vo to not arranged
        set icon size of vo to $DMG_ICON_SIZE
        set text size of vo to $DMG_TEXT_SIZE
        set background picture of vo to file ".background:background.png"
        set position of item "LiveMixStudio.app" of container window to {$DMG_APP_ICON_X, $DMG_APP_ICON_Y}
        set position of item "Applications" of container window to {$DMG_APPS_ICON_X, $DMG_APPS_ICON_Y}
        update without registering applications
        delay 1
        close
    end tell
end tell
APPLESCRIPT

    # Finder 打开窗口后会移除预置的 .VolumeIcon.icns，因此最后再写入卷图标
    cp "$DMG_VOLUME_ICON" "$MOUNT_POINT/.VolumeIcon.icns" >>"$CREATE_LOG" 2>&1 || return 1
    SetFile -c icnC -a C "$MOUNT_POINT" >>"$CREATE_LOG" 2>&1 || return 1

    sync
    detach_styled_image || return 1

    local attempt
    for attempt in 1 2 3; do
        rm -f "$DMG_PATH"
        if hdiutil convert "$STYLED_RW_DMG" -format UDZO -imagekey zlib-level=9 \
                -o "$DMG_PATH" >>"$CREATE_LOG" 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

if [ "$DMG_STYLED" -eq 1 ]; then
    if ! create_styled_dmg; then
        detach_styled_image
        rm -f "$DMG_PATH"
        printf '警告: 安装界面样式应用失败（Finder 自动化不可用？），改为生成默认样式 DMG。\n' >&2
        printf '      详情见日志: %s\n' "$CREATE_LOG" >&2
        if ! create_plain_dmg; then
            cat "$CREATE_LOG" >&2
            fail "DMG 创建失败。"
        fi
    fi
elif ! create_plain_dmg; then
    cat "$CREATE_LOG" >&2
    fail "DMG 创建失败。"
fi

if ! hdiutil verify "$DMG_PATH" >"$VERIFY_LOG" 2>&1; then
    cat "$VERIFY_LOG" >&2
    fail "DMG 校验失败。"
fi

if [ -n "$SIGN_IDENTITY" ] && [ "$SIGN_IDENTITY" != "-" ]; then
    info "签名 DMG"
    SIGN_LOG="$LOG_DIR/codesign-dmg.log"
    if ! codesign --force --sign "$SIGN_IDENTITY" "$DMG_PATH" >"$SIGN_LOG" 2>&1; then
        cat "$SIGN_LOG" >&2
        fail "DMG 签名失败。"
    fi
    codesign --verify "$DMG_PATH" >/dev/null 2>&1 || fail "DMG 签名校验失败。"
fi

DMG_SIZE="$(du -h "$DMG_PATH" | awk '{print $1}')"
DMG_HASH="$(shasum -a 256 "$DMG_PATH" | awk '{print $1}')"
printf '%s\n' "$DMG_HASH" > "$DMG_PATH.sha256"

info "打包完成"
printf '  DMG:    %s\n' "$DMG_PATH"
printf '  版本:   %s (%s)\n' "$APP_VERSION" "$ARCH_LABEL"
printf '  大小:   %s\n' "$DMG_SIZE"
printf '  SHA256: %s\n' "$DMG_HASH"
printf '\n注意: 默认使用 ad-hoc 签名，跨设备分发前建议使用 Developer ID 签名并公证。\n'
