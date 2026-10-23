package app

import (
	"io"
	"log"
	"os"
)

// attachDiagnostics 让标准库 log 的诊断信息同时写进请求日志文件，返回恢复函数。
//
// 托盘模式启动后会 detachConsole()，stderr 无处可去：只写 stderr 的启动/降级诊断
// （“透明拦截未启用”“SSH 透明入口未启用”等）会彻底丢失，用户核查时日志文件里
// 一片空白。日志文件排在前面，这样 stderr 句柄失效也不影响落盘。
func attachDiagnostics(w io.Writer) (restore func()) {
	prev := log.Writer()
	log.SetOutput(io.MultiWriter(w, os.Stderr))
	return func() { log.SetOutput(prev) }
}
