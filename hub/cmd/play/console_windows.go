//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// Windows 的终端默认是 GBK 代码页，而且老 conhost 不开 VT 转义。
// 不处理的话：中文变乱码、颜色全变成一堆 "←[38;5;203m"。
//
// 这里在启动时把代码页切成 UTF-8，并给 stdout 打开
// ENABLE_VIRTUAL_TERMINAL_PROCESSING。用 kernel32 的原始调用，
// 不为两行代码拉一个 Windows 依赖进来。
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

func setupConsole() {
	const utf8CodePage = 65001
	_, _, _ = procSetConsoleOutputCP.Call(utf8CodePage)
	_, _, _ = procSetConsoleCP.Call(utf8CodePage)

	handle, _, _ := procGetStdHandle.Call(uintptr(stdOutputHandle))
	if handle == 0 || handle == uintptr(unsafe.Pointer(nil)) {
		return
	}
	var mode uint32
	if ok, _, _ := procGetConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode))); ok == 0 {
		return // 不是控制台（比如输出被重定向），不用管
	}
	_, _, _ = procSetConsoleMode.Call(handle, uintptr(mode|enableVT))
}
