//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32              = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode    = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode    = kernel32.NewProc("SetConsoleMode")
	procGetCompressedSize = kernel32.NewProc("GetCompressedFileSizeW")
)

func enableANSI() {
	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r != 0 {
		procSetConsoleMode.Call(uintptr(handle), uintptr(mode|0x0004))
	}
}

// getDiskSize returns the actual on-disk allocation size via GetCompressedFileSizeW.
// Falls back to fallback on error (e.g., non-Windows FS, network paths).
func getDiskSize(path string, fallback int64) int64 {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fallback
	}
	var high uint32
	low, _, _ := procGetCompressedSize.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&high)),
	)
	// INVALID_FILE_SIZE = 0xFFFFFFFF; check both low and high for validity
	if low == 0xFFFFFFFF && high == 0 {
		return fallback
	}
	return int64(uint64(high)<<32 | uint64(uint32(low)))
}
