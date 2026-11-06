#!/usr/bin/env bash
#
# LiveMix Studio — macOS DMG packaging helper.
#
# Builds a Release app, deploys Qt frameworks/plugins, then creates a
# drag-to-Applications disk image. Requires macOS and Xcode Command Line Tools.
#
# Examples:
#   ./scripts/pack-macos.sh
#   ./scripts/pack-macos.sh --skip-build
#   ./scripts/pack-macos.sh --clean --jobs 8
#   ./scripts/pack-macos.sh --sign "Developer ID Application: Example (TEAMID)"
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

usage() {
    cat <<'EOF'
LiveMix Studio DMG 打包脚本（macOS）

用法:
  ./scripts/pack-macos.sh [选项] [-- <构建脚本额外参数>]

流程:
  1. 编译 Release 版本（可通过 --skip-build 跳过）
  2. 复制 .app 并用 macdeployqt 部署 Qt 运行库
  3. 创建 Applications 快捷方式并生成压缩 DMG

选项:
  --skip-build           复用构建目录中已有的 LiveMixStudio.app
  --clean                编译前清理构建目录
  -j, --jobs N           并行编译线程数
  --qt PATH              指定 Qt 6 安装前缀
  --ffmpeg-dir DIR       ffmpeg/ffprobe 所在目录
                         （默认: third_party/ffmpeg/macos-<arch>/bin 或 LIVEMIX_FFMPEG_DIR）
  --build-dir DIR        构建目录（默认: build-dmg）
  --output DIR           DMG 输出目录（默认: dist）
  --sign IDENTITY        codesign 签名身份；默认使用 ad-hoc 临时签名
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

[ "$(uname -s)" = "Darwin" ] || fail "DMG 打包仅支持 macOS。"

for tool in cmake macdeployqt hdiutil codesign ditto lipo shasum; do
    command -v "$tool" >/dev/null 2>&1 || fail "缺少所需工具: $tool"
done

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
DMG_PATH="$OUTPUT_DIR/LiveMixStudio-${APP_VERSION}-${ARCH_LABEL}.dmg"

WORK_DIR="$(mktemp -d "${TMPDIR:-/tmp}/livemix-dmg.XXXXXX")"
STAGING_DIR="$WORK_DIR/dmg-root"
LOG_DIR="$WORK_DIR/logs"
mkdir -p "$STAGING_DIR" "$LOG_DIR"
cleanup() {
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
# 更早的版本（如 CI 使用的 6.8）本身不签名，且会把该参数当作未知参数直接报错。
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

info "生成 DMG"
CREATE_LOG="$LOG_DIR/hdiutil-create.log"
VERIFY_LOG="$LOG_DIR/hdiutil-verify.log"
rm -f "$DMG_PATH"
if ! hdiutil create \
        -volname "$VOLUME_NAME" \
        -srcfolder "$STAGING_DIR" \
        -fs HFS+ \
        -format UDZO \
        -imagekey zlib-level=9 \
        -ov \
        "$DMG_PATH" >"$CREATE_LOG" 2>&1; then
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
