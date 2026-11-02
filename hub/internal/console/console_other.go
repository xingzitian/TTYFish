//go:build !windows

package console

// Setup 在非 Windows 上是空操作：终端本来就是 UTF-8 + ANSI。
func Setup() {}
