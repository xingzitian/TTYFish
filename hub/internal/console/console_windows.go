//go:build windows

// Package console 负责把 Windows 终端调到能显示彩色中文的状态。
//
// 直接跑在 Windows 上（不是通过 SSH）的命令都要先调一次 Setup：
// 否则中文是乱码（GBK 代码页），颜色是一堆 "←[38;5;203m"（没开 VT 转义）。
package console

import (
	"syscall"
	"unsafe"
)

const (
	stdOutputHandle = 0xFFFFFFF5 // (DWORD)-11
	enableVT        = 0x0004
)

var (
	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procGetConsoleMode     = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode     = kernel32.NewProc("SetConsoleMode")
	procGetStdHandle       = kernel32.NewProc("GetStdHandle")
)

// Setup 切 UTF-8 代码页并打开 VT 转义。失败时静默返回：调不动的终端也没别的办法。
func Setup() {
	const utf8CodePage = 65001
	_, _, _ = procSetConsoleOutputCP.Call(utf8CodePage)
	_, _, _ = procSetConsoleCP.Call(utf8CodePage)

	handle, _, _ := procGetStdHandle.Call(uintptr(stdOutputHandle))
	if handle == 0 {
		return
	}
	var mode uint32
	if ok, _, _ := procGetConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode))); ok == 0 {
		return // 不是控制台（输出被重定向），不用管
	}
	_, _, _ = procSetConsoleMode.Call(handle, uintptr(mode|enableVT))
}
