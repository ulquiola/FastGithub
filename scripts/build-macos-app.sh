#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "$(uname -s)" != "Darwin" ]; then
  echo "macOS 应用只能在 macOS 上构建" >&2
  exit 1
fi

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X fastgithub-go/internal/app.version=$VERSION -X fastgithub-go/internal/app.commit=$COMMIT -X fastgithub-go/internal/app.buildTime=$BUILD_TIME"

APP="dist/FastGithub.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

CGO_ENABLED=1 GOOS=darwin GOARCH="$(go env GOARCH)" \
  go build -trimpath -ldflags "$LDFLAGS" \
  -o "$APP/Contents/MacOS/FastGithub" ./cmd/fastgithub-menubar

cp fastgithub.toml "$APP/Contents/Resources/"
cp -R conf.d "$APP/Contents/Resources/"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>com.fastgithub.menubar</string>
  <key>CFBundleName</key><string>FastGithub</string>
  <key>CFBundleDisplayName</key><string>FastGithub</string>
  <key>CFBundleExecutable</key><string>FastGithub</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSUIElement</key><true/>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

codesign --force --deep -s - "$APP"
echo "已生成 $APP"

if [ "${INSTALL:-0}" = "1" ]; then
  mkdir -p "$HOME/Applications"
  rm -rf "$HOME/Applications/FastGithub.app"
  cp -R "$APP" "$HOME/Applications/"
  echo "已安装到 $HOME/Applications/FastGithub.app"
fi
