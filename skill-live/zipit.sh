#!/usr/bin/env bash
# 将 skill-live 目录打包为 LiveMixStudio.zip
# 解压后得到 LiveMixStudio/ 目录，且不包含 .gitignore 与 zipit.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PKG_NAME="LiveMixStudio"
OUT_ZIP="$SCRIPT_DIR/$PKG_NAME.zip"

STAGE_DIR="$(mktemp -d)"
trap 'rm -rf "$STAGE_DIR"' EXIT

# 先拷贝到暂存目录，顶层目录名即为 LiveMixStudio
mkdir -p "$STAGE_DIR/$PKG_NAME"
rsync -a \
  --exclude=".gitignore" \
  --exclude="zipit.sh" \
  --exclude="$PKG_NAME.zip" \
  "$SCRIPT_DIR"/ "$STAGE_DIR/$PKG_NAME"/

# 在暂存目录内打包，避免把输出文件自身或临时文件路径带进压缩包
rm -f "$OUT_ZIP"
(cd "$STAGE_DIR" && zip -rq "$OUT_ZIP" "$PKG_NAME")

echo "已生成: $OUT_ZIP"
