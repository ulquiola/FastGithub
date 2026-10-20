package app

import "runtime"

var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// Version 返回构建版本，发布时由 -ldflags 注入。
func Version() string { return version }

// BuildInfo 返回版本、提交、构建时间与 Go 运行时信息。
func BuildInfo() string {
	return "fastgithub " + version + " (" + commit + ", " + buildTime + ", " + runtime.Version() + ")"
}
