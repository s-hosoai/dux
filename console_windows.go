//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

func enableANSI() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r != 0 {
		// ENABLE_VIRTUAL_TERMINAL_PROCESSING (0x0004) を有効化
		setConsoleMode.Call(uintptr(handle), uintptr(mode|0x0004))
	}
}
