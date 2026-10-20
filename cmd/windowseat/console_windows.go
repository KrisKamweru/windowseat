package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	getConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	freeConsole           = kernel32.NewProc("FreeConsole")
)

// releaseOwnConsole closes the console window Windows opens for serve when a
// logon task starts it. A console shared with a terminal is left alone.
func releaseOwnConsole() {
	var pids [2]uint32
	count, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	if count == 1 {
		freeConsole.Call()
	}
}
