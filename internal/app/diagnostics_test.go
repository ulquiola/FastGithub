package app

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestAttachDiagnosticsWritesToLog 锁定托盘模式的排查能力：进程 detachConsole() 后
// stderr 无处可去，启动/降级诊断（“透明拦截未启用”等）必须同时落进日志文件，
// 否则 logs/fastgithub.log 会是空的，用户无法核查。
func TestAttachDiagnosticsWritesToLog(t *testing.T) {
	var buf bytes.Buffer
	restore := attachDiagnostics(&buf)
	log.Printf("透明拦截未启用：需要管理员权限")
	restore()
	if !strings.Contains(buf.String(), "透明拦截未启用") {
		t.Fatalf("诊断信息没有写入日志文件，得到 %q", buf.String())
	}
}

// TestAttachDiagnosticsRestoresOutput 保证不改坏调用方原有的 log 输出目标。
func TestAttachDiagnosticsRestoresOutput(t *testing.T) {
	prev := log.Writer()
	var buf bytes.Buffer
	restore := attachDiagnostics(&buf)
	restore()
	if log.Writer() != prev {
		t.Fatal("attachDiagnostics 的 restore 没有恢复标准 logger 的输出")
	}
}
