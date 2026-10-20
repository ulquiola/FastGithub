#!/usr/bin/env bash
set -euo pipefail
ORIG_PWD="$PWD"
cd "$(dirname "$0")/.."

VERSION="2.2.2"
SHA256="63cb41763bb4b20f600b6de04e991a9c2be73279e317d4d82f237b150c5f3f15"
DEST_IN="${1:-windivert}"
case "$DEST_IN" in
  /*) DEST="$DEST_IN" ;;
  *) DEST="$ORIG_PWD/$DEST_IN" ;;
esac
URL="https://github.com/basil00/WinDivert/releases/download/v${VERSION}/WinDivert-${VERSION}-A.zip"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [ -n "${WINDIVERT_ZIP:-}" ]; then
  case "$WINDIVERT_ZIP" in
    /*) ZIP_SRC="$WINDIVERT_ZIP" ;;
    *) ZIP_SRC="$ORIG_PWD/$WINDIVERT_ZIP" ;;
  esac
  echo "使用本地包 $ZIP_SRC"
  cp "$ZIP_SRC" "$tmp/windivert.zip"
else
  echo "下载 $URL"
  curl -fsSL --retry 3 --max-time 300 "$URL" -o "$tmp/windivert.zip"
fi

if command -v sha256sum >/dev/null 2>&1; then
  echo "$SHA256  $tmp/windivert.zip" | sha256sum -c -
else
  echo "$SHA256  $tmp/windivert.zip" | shasum -a 256 -c -
fi

mkdir -p "$DEST"
unzip -j -o "$tmp/windivert.zip" \
  "WinDivert-${VERSION}-A/x64/WinDivert.dll" \
  "WinDivert-${VERSION}-A/x64/WinDivert64.sys" \
  "WinDivert-${VERSION}-A/README" \
  "WinDivert-${VERSION}-A/LICENSE" \
  "WinDivert-${VERSION}-A/include/windivert.h" \
  -d "$DEST"
echo "WinDivert ${VERSION} 已解压到 $DEST/"
