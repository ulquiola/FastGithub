#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

go test ./...
go vet ./...

if ! gofmt_out="$(gofmt -l .)"; then
  echo "gofmt 执行失败" >&2
  exit 1
fi
if [ -n "$gofmt_out" ]; then
  echo "以下文件未格式化:" >&2
  echo "$gofmt_out" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for target in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  echo "build $os/$arch"
  GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -o "$tmp/fastgithub-$os-$arch" ./cmd/fastgithub
done
echo "ci ok"
