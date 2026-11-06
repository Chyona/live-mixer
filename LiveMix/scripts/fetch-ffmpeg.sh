#!/usr/bin/env bash
#
# LiveMix Studio — download pinned static FFmpeg binaries for macOS.
#
# The binaries are NOT committed to the repository. CI and local packagers run
# this script before building so ffmpeg/ffprobe can be bundled into the .app.
#
# Examples:
#   ./scripts/fetch-ffmpeg.sh
#   ./scripts/fetch-ffmpeg.sh --output third_party/ffmpeg/macos-arm64
#   ./scripts/fetch-ffmpeg.sh --force
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Pinned ffmpeg-static release (currently FFmpeg 6.1.1). Keep the tag and the
# SHA-256 values below in sync; never silently follow a moving "latest" URL.
FFMPEG_RELEASE="b6.1.1"

OUTPUT_DIR=""
FORCE=0

usage() {
    cat <<'EOF'
下载 LiveMix Studio 打包所需的静态 FFmpeg 二进制（macOS）

用法:
  ./scripts/fetch-ffmpeg.sh [选项]

选项:
  --output DIR   输出平台目录（默认: third_party/ffmpeg/macos-<arch>）
  --force        重新下载并覆盖已有文件
  -h, --help     显示帮助

产物:
  <output>/bin/ffmpeg
  <output>/bin/ffprobe
  <output>/bin/ffmpeg-LICENSE.txt
  <output>/bin/ffmpeg-BUILD-README.txt
  <output>/bin/ffmpeg-SOURCE.txt
EOF
}

info() {
    printf '\n==> %s\n' "$1" >&2
}

fail() {
    printf '错误: %s\n' "$1" >&2
    exit 1
}

while [ "$#" -gt 0 ]; do
    arg="$1"
    case "$arg" in
        --output)
            [ "$#" -ge 2 ] || fail "--output 需要一个路径"
            OUTPUT_DIR="$2"
            shift
            ;;
        --output=*)
            OUTPUT_DIR="${arg#*=}"
            ;;
        --force)
            FORCE=1
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            printf '错误: 未知参数 %s\n\n' "$arg" >&2
            usage >&2
            exit 1
            ;;
    esac
    shift
done

[ "$(uname -s)" = "Darwin" ] || fail "此脚本仅支持 macOS；Windows 请运行 scripts/fetch-ffmpeg.ps1。"

case "$(uname -m)" in
    arm64)
        FFMPEG_ASSET="darwin-arm64"
        EXPECTED_ARCH="arm64"
        OUTPUT_ARCH="macos-arm64"
        FFMPEG_SHA256="8923876afa8db5585022d7860ec7e589af192f441c56793971276d450ed3bbfa"
        FFPROBE_SHA256="d986a8ec7b030899fe66a8a288ed809a3543338705a3ce178cfb85869c5d80be"
        LICENSE_SHA256="cb48bf09a11f5fb576cddb0431c8f5ed0a60157a9ec942adffc13907cbe083f2"
        README_SHA256="05ba4b92c96605434b1aaae3eedf5a2c280c9607bf78ffca9a5b536d9af2dc6a"
        ;;
    x86_64)
        FFMPEG_ASSET="darwin-x64"
        EXPECTED_ARCH="x86_64"
        OUTPUT_ARCH="macos-x64"
        FFMPEG_SHA256="929b375c1182d956c51f7ac25e0b2b0411fb01f6f407aa15c9758efeb4242106"
        FFPROBE_SHA256="d4da574d6e2e197bd259b47d69cf262df9e312af24ad960444f6d806d3d4c186"
        LICENSE_SHA256="2e1d16c72fd74e12063776371da757322f8b77589386532f4fd8634bde7de1af"
        README_SHA256="e88a0325f8e5b75210355e37341824f074d3cd82def2125be54c914b62848a36"
        ;;
    *)
        fail "不支持的 CPU 架构: $(uname -m)"
        ;;
esac

if [ -z "$OUTPUT_DIR" ]; then
    OUTPUT_DIR="$ROOT_DIR/third_party/ffmpeg/$OUTPUT_ARCH"
fi
case "$OUTPUT_DIR" in
    /*) ;;
    *) OUTPUT_DIR="$ROOT_DIR/$OUTPUT_DIR" ;;
esac
BIN_DIR="$OUTPUT_DIR/bin"

if [ "$FORCE" -eq 0 ] \
    && [ -x "$BIN_DIR/ffmpeg" ] \
    && [ -x "$BIN_DIR/ffprobe" ] \
    && [ -f "$BIN_DIR/ffmpeg-LICENSE.txt" ] \
    && [ -f "$BIN_DIR/ffmpeg-BUILD-README.txt" ]; then
    info "FFmpeg 已存在: $BIN_DIR"
    exit 0
fi

for tool in curl gzip shasum awk lipo codesign; do
    command -v "$tool" >/dev/null 2>&1 || fail "缺少所需工具: $tool"
done

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/livemix-ffmpeg.XXXXXX")"
cleanup() {
    rm -rf "$TMP_DIR"
}
trap cleanup EXIT

mkdir -p "$BIN_DIR"

# Download one release asset, verify it, and print the local path on stdout.
# Progress messages go to stderr so command substitution stays clean.
fetch_asset() {
    asset="$1"
    expected_sha="$2"
    url="https://github.com/eugeneware/ffmpeg-static/releases/download/${FFMPEG_RELEASE}/${asset}"
    file="$TMP_DIR/$asset"

    info "下载 $asset"
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 15 -o "$file" "$url" \
        || fail "下载失败: $url"

    actual_sha="$(shasum -a 256 "$file" | awk '{print $1}')"
    if [ "$actual_sha" != "$expected_sha" ]; then
        fail "SHA-256 校验失败: $asset（期望 $expected_sha，实际 $actual_sha）"
    fi
    printf '%s' "$file"
}

install_binary() {
    asset="$1"
    expected_sha="$2"
    name="$3"

    archive="$(fetch_asset "$asset" "$expected_sha")"
    gzip -dc "$archive" > "$BIN_DIR/$name"
    chmod 0755 "$BIN_DIR/$name"

    # Apple silicon refuses to execute unsigned Mach-O binaries. Ad-hoc signing
    # keeps the binary runnable locally; pack-macos.sh re-signs it with the app's
    # real identity during packaging.
    if ! codesign --force --sign - "$BIN_DIR/$name" >/dev/null 2>&1; then
        fail "ad-hoc 签名失败: $name"
    fi

    archs="$(lipo -archs "$BIN_DIR/$name" 2>/dev/null || true)"
    case " $archs " in
        *" $EXPECTED_ARCH "*) ;;
        *) fail "二进制架构不匹配: $name（$archs，期望 $EXPECTED_ARCH）" ;;
    esac

    if ! "$BIN_DIR/$name" -version >/dev/null 2>&1; then
        fail "无法运行 $name，请检查 macOS 安全策略。"
    fi
    info "已安装 $BIN_DIR/$name"
}

install_binary "ffmpeg-${FFMPEG_ASSET}.gz" "$FFMPEG_SHA256" "ffmpeg"
install_binary "ffprobe-${FFMPEG_ASSET}.gz" "$FFPROBE_SHA256" "ffprobe"

license_file="$(fetch_asset "${FFMPEG_ASSET}.LICENSE" "$LICENSE_SHA256")"
cp "$license_file" "$BIN_DIR/ffmpeg-LICENSE.txt"

readme_file="$(fetch_asset "${FFMPEG_ASSET}.README" "$README_SHA256")"
cp "$readme_file" "$BIN_DIR/ffmpeg-BUILD-README.txt"

cat > "$BIN_DIR/ffmpeg-SOURCE.txt" <<EOF
FFmpeg static binaries (ffmpeg, ffprobe)
Release: https://github.com/eugeneware/ffmpeg-static/releases/tag/${FFMPEG_RELEASE}
Upstream source: https://ffmpeg.org/releases/ffmpeg-6.1.1.tar.xz
License: see ffmpeg-LICENSE.txt
Build details: see ffmpeg-BUILD-README.txt

These executables are bundled as separate programs. They are invoked as
subprocesses and are not linked into LiveMix Studio.
EOF

info "FFmpeg 准备完成"
printf '  输出目录: %s\n' "$BIN_DIR"
