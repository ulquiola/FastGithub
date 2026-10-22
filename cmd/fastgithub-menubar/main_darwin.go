//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "menubar.h"
*/
import "C"

import "runtime"

func main() {
	// NSApplication 必须跑在主线程上。
	runtime.LockOSThread()
	C.fg_run_menu()
}
