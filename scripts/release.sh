#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# 打包工具依次回退：zip → PowerShell Compress-Archive → bsdtar（Windows 自带）。
archive() {
  local name="$1"
  if command -v zip >/dev/null 2>&1; then
    (cd dist && zip -qr "$name.zip" "$name")
  elif command -v powershell.exe >/dev/null 2>&1; then
    powershell.exe -NoProfile -NonInteractive -Command \
      "Compress-Archive -Path 'dist/$name' -DestinationPath 'dist/$name.zip' -Force"
  elif command -v tar >/dev/null 2>&1; then
    (cd dist && tar -a -cf "$name.zip" "$name")
  else
    echo "缺少打包工具：需要 zip、powershell.exe 或 tar 之一" >&2
    exit 1
  fi
}

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X fastgithub-go/internal/app.version=$VERSION -X fastgithub-go/internal/app.commit=$COMMIT -X fastgithub-go/internal/app.buildTime=$BUILD_TIME"

rm -rf dist
trap 'status=$?; if [ "$status" -ne 0 ]; then echo "release 失败，清理 dist" >&2; rm -rf dist; fi' EXIT
mkdir -p dist
for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  name="fastgithub-$os-$arch"
  dir="dist/$name"
  mkdir -p "$dir"

  exe="fastgithub"
  if [ "$os" = "windows" ]; then
    exe="fastgithub.exe"
  fi
  echo "release $name ($VERSION)"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$dir/$exe" ./cmd/fastgithub
  cp fastgithub.toml README.md LICENSE "$dir/"
  cp -R conf.d "$dir/"
  # WinDivert 2.2.2 只有 x86/x64 驱动，arm64 包不携带。
  if [ -d windivert ] && [ "$os" = "windows" ] && [ "$arch" = "amd64" ]; then
    if [ ! -f windivert/WinDivert.dll ] || [ ! -f windivert/WinDivert64.sys ]; then
      echo "windivert/ 缺少 WinDivert.dll 或 WinDivert64.sys" >&2
      exit 1
    fi
    cp -R windivert "$dir/"
  fi
  archive "$name"
done
echo "发布产物已生成到 dist/"
