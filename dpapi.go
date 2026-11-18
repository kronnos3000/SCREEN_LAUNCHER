//go:build windows

package main

// The installer stores the JobTread key next to the exe, encrypted with Windows
// DPAPI for the current user (jobtread.key). Only that Windows user can read it.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	pCryptUnprotectData = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptUnprotectData")
	pLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	Size uint32
	Data *byte
}

func protectedKey() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(exe), "jobtread.key"))
	if err != nil || len(b) == 0 {
		return ""
	}
	in := dataBlob{uint32(len(b)), &b[0]}
	var out dataBlob
	r, _, _ := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if r == 0 || out.Data == nil {
		return ""
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.Data)))
	return strings.TrimSpace(string(unsafe.Slice(out.Data, out.Size)))
}
