//go:build windows

// Installer for Construction Corps Meeting Launcher.
// Per-user install (no admin): %LOCALAPPDATA%\Programs\CCorps Meeting Launcher
// Run with /uninstall to remove (Windows "Installed apps" does this).
package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed payload/app.exe
var appExe []byte

//go:embed payload/app.ico
var appIco []byte

//go:embed payload/settings.json
var settings []byte

//go:embed payload/key.txt
var grantKey []byte

const (
	appName   = "Construction Corps Meeting Launcher"
	exeName   = "CCorps Meeting Launcher.exe"
	uninstExe = "Uninstall CCorps Meeting Launcher.exe"
	lnkName   = "CCorps Meeting Launcher.lnk"
	regKey    = `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\CCorpsMeetingLauncher`
	version   = "1.0.0"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	pMessageBox       = user32.NewProc("MessageBoxW")
	pCryptProtectData = syscall.NewLazyDLL("crypt32.dll").NewProc("CryptProtectData")
	pLocalFree        = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func box(text string, flags uintptr) int {
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(appName+" Setup"))), flags|0x00010000) // MB_SETFOREGROUND
	return int(r)
}

func fail(err error) {
	box("Setup could not finish:\n\n"+err.Error(), 0x10)
	os.Exit(1)
}

func hidden(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return c.Run()
}

func ps(script string) error {
	return hidden("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func q(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" } // PowerShell single-quote

type dataBlob struct {
	Size uint32
	Data *byte
}

// protect encrypts b with DPAPI for the current Windows user.
func protect(b []byte) ([]byte, error) {
	in := dataBlob{uint32(len(b)), &b[0]}
	var out dataBlob
	r, _, e := pCryptProtectData.Call(uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0, 1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, fmt.Errorf("could not protect the JobTread key: %v", e)
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

func installDir() string {
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "CCorps Meeting Launcher")
}

func stopApp() {
	_ = hidden("taskkill.exe", "/IM", exeName, "/F")
	time.Sleep(500 * time.Millisecond)
}

func install() {
	dir := installDir()
	if box("Install "+appName+" for this Windows user?\n\nIt goes in:\n"+dir+"\n\nShortcuts are added to the Desktop and Start menu.", 0x4|0x20) != 6 {
		return
	}
	stopApp()
	if err := os.MkdirAll(dir, 0755); err != nil {
		fail(err)
	}
	exe := filepath.Join(dir, exeName)
	ico := filepath.Join(dir, "app.ico")
	for p, b := range map[string][]byte{exe: appExe, ico: appIco} {
		if err := os.WriteFile(p, b, 0755); err != nil {
			fail(err)
		}
	}
	// Keep existing preferences on a reinstall.
	cfg := filepath.Join(dir, "ScreenLauncher-presets.json")
	if _, err := os.Stat(cfg); err != nil {
		if err := os.WriteFile(cfg, settings, 0644); err != nil {
			fail(err)
		}
	}
	if k := strings.TrimSpace(string(grantKey)); k != "" {
		enc, err := protect([]byte(k))
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "jobtread.key"), enc, 0600); err != nil {
			fail(err)
		}
	}
	// Copy this installer in as the uninstaller.
	if self, err := os.Executable(); err == nil {
		if b, err := os.ReadFile(self); err == nil {
			_ = os.WriteFile(filepath.Join(dir, uninstExe), b, 0755)
		}
	}

	script := fmt.Sprintf(`$w = New-Object -ComObject WScript.Shell
foreach ($d in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  $s = $w.CreateShortcut((Join-Path $d %s)); $s.TargetPath = %s; $s.WorkingDirectory = %s
  $s.IconLocation = %s; $s.Description = %s; $s.Save() }`, q(lnkName), q(exe), q(dir), q(ico+",0"), q(appName))
	if err := ps(script); err != nil {
		box("Installed, but the shortcuts could not be created.\nStart it from:\n"+exe, 0x30)
	}

	un := filepath.Join(dir, uninstExe)
	for _, v := range [][3]string{
		{"DisplayName", "REG_SZ", appName},
		{"DisplayVersion", "REG_SZ", version},
		{"Publisher", "REG_SZ", "Construction Corps"},
		{"DisplayIcon", "REG_SZ", ico},
		{"InstallLocation", "REG_SZ", dir},
		{"UninstallString", "REG_SZ", `"` + un + `" /uninstall`},
		{"NoModify", "REG_DWORD", "1"},
		{"NoRepair", "REG_DWORD", "1"},
	} {
		_ = hidden("reg.exe", "add", regKey, "/v", v[0], "/t", v[1], "/d", v[2], "/f")
	}

	if box(appName+" is installed.\n\nOpen it now?", 0x4|0x40) == 6 {
		c := exec.Command(exe)
		c.Dir = dir
		_ = c.Start()
	}
}

func uninstall() {
	dir := installDir()
	if box("Remove "+appName+" from this PC?", 0x4|0x20) != 6 {
		return
	}
	stopApp()
	_ = ps(fmt.Sprintf(`foreach ($d in @([Environment]::GetFolderPath('Desktop'), [Environment]::GetFolderPath('Programs'))) {
  Remove-Item -LiteralPath (Join-Path $d %s) -ErrorAction SilentlyContinue }`, q(lnkName)))
	_ = hidden("reg.exe", "delete", regKey, "/f")
	// This uninstaller lives in the folder: delete the folder after it exits.
	c := exec.Command("cmd.exe", "/c", "ping -n 3 127.0.0.1 >nul & rmdir /s /q \""+dir+"\"")
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = c.Start()
	box(appName+" was removed.", 0x40)
}

func main() {
	if len(os.Args) > 1 && strings.EqualFold(os.Args[1], "/uninstall") {
		uninstall()
		return
	}
	install()
}
