//go:build !windows

package main

// setupConsole 在非 Windows 上是空操作：终端本来就是 UTF-8 + ANSI。
func setupConsole() {}
