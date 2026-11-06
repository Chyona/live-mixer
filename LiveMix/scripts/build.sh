#!/usr/bin/env bash
#
# LiveMix Studio — macOS / Linux one-command build helper.
#
# Examples:
#   ./scripts/build.sh                  # Debug build
#   ./scripts/build.sh release          # Release build
#   ./scripts/build.sh release --clean  # Clean Release build
#   ./scripts/build.sh release --run    # Build and launch
#   ./scripts/build.sh -j 4             # Limit parallel jobs
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$ROOT_DIR/build"
BUILD_TYPE="Debug"
CLEAN=0
RUN=0
JOBS=""
QT_PREFIX=""
FFMPEG_DIR="${LIVEMIX_FFMPEG_DIR:-}"
EXTRA_CMAKE_ARGS=()

usage() {
    cat <<'EOF'
LiveMix Studio 一键编译脚本（macOS / Linux）

用法:
  ./scripts/build.sh [debug|release] [选项] [-- <CMake 额外参数>]

选项:
  --debug              编译 Debug 版本（默认）
  --release            编译 Release 版本
  --clean              编译前删除构建目录
  --run                编译成功后启动应用
  -j, --jobs N         并行编译线程数（默认由 CMake 决定）
  --qt PATH            指定 Qt 6 安装前缀
  --build-dir DIR      指定构建目录（默认: build）
  --ffmpeg-dir DIR     指定 ffmpeg/ffprobe 所在目录，编译时打入应用包
  -h, --help           显示帮助

示例:
  ./scripts/build.sh
  ./scripts/build.sh release
  ./scripts/build.sh release --clean --run
  ./scripts/build.sh --qt /opt/homebrew/opt/qt
  ./scripts/build.sh release --ffmpeg-dir third_party/ffmpeg/macos-arm64/bin
  ./scripts/build.sh release -- -DCMAKE_OSX_ARCHITECTURES=arm64
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
        debug|Debug|DEBUG)
            BUILD_TYPE="Debug"
            ;;
        release|Release|RELEASE)
            BUILD_TYPE="Release"
            ;;
        --debug)
            BUILD_TYPE="Debug"
            ;;
        --release)
            BUILD_TYPE="Release"
            ;;
        --clean)
            CLEAN=1
            ;;
        --run)
            RUN=1
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
        --build-dir)
            [ "$#" -ge 2 ] || fail "--build-dir 需要一个路径"
            BUILD_DIR="$2"
            shift
            ;;
        --build-dir=*)
            BUILD_DIR="${arg#*=}"
            ;;
        --ffmpeg-dir)
            [ "$#" -ge 2 ] || fail "--ffmpeg-dir 需要一个路径"
            FFMPEG_DIR="$2"
            shift
            ;;
        --ffmpeg-dir=*)
            FFMPEG_DIR="${arg#*=}"
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        --)
            shift
            while [ "$#" -gt 0 ]; do
                EXTRA_CMAKE_ARGS+=("$1")
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

case "$BUILD_DIR" in
    /*) ;;
    *) BUILD_DIR="$ROOT_DIR/$BUILD_DIR" ;;
esac

if [ -n "$FFMPEG_DIR" ]; then
    case "$FFMPEG_DIR" in
        /*) ;;
        *) FFMPEG_DIR="$ROOT_DIR/$FFMPEG_DIR" ;;
    esac
fi

if [ "$CLEAN" -eq 1 ]; then
    case "$BUILD_DIR" in
        ""|"/"|"$ROOT_DIR") fail "拒绝清理不安全的构建目录: $BUILD_DIR" ;;
    esac
    if [ -d "$BUILD_DIR" ] && [ -n "$(ls -A "$BUILD_DIR" 2>/dev/null)" ] \
        && [ ! -f "$BUILD_DIR/CMakeCache.txt" ]; then
        fail "拒绝清理非 CMake 构建目录: ${BUILD_DIR}（未找到 CMakeCache.txt）"
    fi
    info "清理构建目录: $BUILD_DIR"
    rm -rf "$BUILD_DIR"
fi

if [ -e "$BUILD_DIR" ] && [ ! -d "$BUILD_DIR" ]; then
    fail "构建路径已存在但不是目录: $BUILD_DIR"
fi
mkdir -p "$BUILD_DIR"
# Resolve symlinks (notably /tmp -> /private/tmp on macOS) so AUTOMOC's
# generated relative include paths remain valid.
BUILD_DIR="$(cd "$BUILD_DIR" && pwd -P)"

is_qt_prefix() {
    [ -n "${1:-}" ] && [ -f "$1/lib/cmake/Qt6/Qt6Config.cmake" ]
}

detect_qt() {
    if [ -n "$QT_PREFIX" ]; then
        is_qt_prefix "$QT_PREFIX" || fail "指定的 Qt 路径无效（缺少 lib/cmake/Qt6/Qt6Config.cmake）: $QT_PREFIX"
        return
    fi

    for candidate in "${QT_ROOT:-}" "${QTDIR:-}"; do
        if is_qt_prefix "$candidate"; then
            QT_PREFIX="$candidate"
            return
        fi
    done

    if command -v brew >/dev/null 2>&1; then
        candidate="$(brew --prefix qt 2>/dev/null || true)"
        if is_qt_prefix "$candidate"; then
            QT_PREFIX="$candidate"
            return
        fi
    fi

    for hint in qtpaths6 qmake6 qmake; do
        if command -v "$hint" >/dev/null 2>&1; then
            version="$("$hint" -query QT_VERSION 2>/dev/null || true)"
            case "$version" in
                6.*)
                    candidate="$("$hint" -query QT_INSTALL_PREFIX 2>/dev/null || true)"
                    if is_qt_prefix "$candidate"; then
                        QT_PREFIX="$candidate"
                        return
                    fi
                    ;;
            esac
        fi
    done

    for candidate in /opt/homebrew/opt/qt /usr/local/opt/qt \
                     "$HOME"/Qt/6.*/macos /Applications/Qt/6.*/macos; do
        if is_qt_prefix "$candidate"; then
            QT_PREFIX="$candidate"
            return
        fi
    done
}

command -v cmake >/dev/null 2>&1 || fail "未找到 cmake，请先安装 CMake 3.21+"
detect_qt

info "配置 LiveMix Studio"
printf '  源码目录: %s\n' "$ROOT_DIR"
printf '  构建目录: %s\n' "$BUILD_DIR"
printf '  构建类型: %s\n' "$BUILD_TYPE"
if [ -n "$QT_PREFIX" ]; then
    printf '  Qt 前缀:  %s\n' "$QT_PREFIX"
else
    printf '  Qt 前缀:  由 CMake 自动查找（可使用 --qt 指定）\n'
fi
if [ -n "$FFMPEG_DIR" ]; then
    printf '  FFmpeg:   %s\n' "$FFMPEG_DIR"
fi

configure_args=(-S "$ROOT_DIR" -B "$BUILD_DIR" "-DCMAKE_BUILD_TYPE=$BUILD_TYPE")
if [ -n "$QT_PREFIX" ]; then
    configure_args+=("-DCMAKE_PREFIX_PATH=$QT_PREFIX")
fi
if [ -n "$FFMPEG_DIR" ]; then
    configure_args+=("-DLIVEMIX_FFMPEG_DIR=$FFMPEG_DIR")
fi
if [ "${#EXTRA_CMAKE_ARGS[@]}" -gt 0 ]; then
    configure_args+=("${EXTRA_CMAKE_ARGS[@]}")
fi

if ! cmake "${configure_args[@]}"; then
    printf '\n配置失败。请确认已安装 Qt 6.5+；Homebrew 用户可尝试 --qt "$(brew --prefix qt 2>/dev/null || printf /opt/homebrew/opt/qt)"。\n' >&2
    exit 1
fi

info "编译 LiveMix Studio ($BUILD_TYPE)"
build_args=(--build "$BUILD_DIR" --config "$BUILD_TYPE" --parallel)
if [ -n "$JOBS" ]; then
    build_args+=("$JOBS")
fi
cmake "${build_args[@]}"

APP_PATH=""
BIN_PATH=""

case "$(uname -s)" in
    Darwin)
        for candidate in \
            "$BUILD_DIR/LiveMixStudio.app" \
            "$BUILD_DIR/$BUILD_TYPE/LiveMixStudio.app" \
            "$BUILD_DIR/Release/LiveMixStudio.app"; do
            if [ -d "$candidate" ]; then
                APP_PATH="$candidate"
                break
            fi
        done
        if [ -z "$APP_PATH" ]; then
            APP_PATH="$(find "$BUILD_DIR" -maxdepth 4 -type d -name LiveMixStudio.app -print -quit 2>/dev/null || true)"
        fi
        ;;
    *)
        for candidate in \
            "$BUILD_DIR/LiveMixStudio" \
            "$BUILD_DIR/$BUILD_TYPE/LiveMixStudio" \
            "$BUILD_DIR/Release/LiveMixStudio"; do
            if [ -x "$candidate" ]; then
                BIN_PATH="$candidate"
                break
            fi
        done
        if [ -z "$BIN_PATH" ]; then
            BIN_PATH="$(find "$BUILD_DIR" -maxdepth 4 -type f -name LiveMixStudio -perm -111 -print -quit 2>/dev/null || true)"
        fi
        ;;
esac

info "编译完成"
if [ -n "$APP_PATH" ]; then
    printf '  应用: %s\n' "$APP_PATH"
elif [ -n "$BIN_PATH" ]; then
    printf '  可执行文件: %s\n' "$BIN_PATH"
else
    printf '  警告: 未在构建目录中找到可执行文件。\n' >&2
fi

if [ "$RUN" -eq 1 ]; then
    if [ -n "$APP_PATH" ]; then
        info "启动 LiveMix Studio"
        open "$APP_PATH"
    elif [ -n "$BIN_PATH" ]; then
        info "启动 LiveMix Studio"
        nohup "$BIN_PATH" >/dev/null 2>&1 &
    else
        fail "构建产物不存在，无法启动"
    fi
fi
