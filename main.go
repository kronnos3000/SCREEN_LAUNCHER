//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// ---------- config ----------

type winSpec struct {
	URL     string   `json:"url,omitempty"`
	URLs    []string `json:"urls,omitempty"` // more tabs in the same window (one Chrome call)
	Monitor string   `json:"monitor"`        // "main", "second", "third"
	App     bool     `json:"app,omitempty"`  // chromeless window; first URL only
	JT      string   `json:"jt,omitempty"`   // JobTread view jobs: "first", "second" or "all"
}

func (s winSpec) tabs() []string {
	var t []string
	if s.URL != "" {
		t = append(t, s.URL)
	}
	for _, u := range s.URLs {
		if u = strings.TrimSpace(u); u != "" {
			t = append(t, u)
		}
	}
	return t
}

// A meeting: Google Meet on one screen, other links as tabs in one window on another.
type meeting struct {
	Name        string   `json:"name"`
	MeetURL     string   `json:"meetUrl"`
	MeetScreen  string   `json:"meetScreen"`
	OtherScreen string   `json:"otherScreen"`
	OtherLinks  []string `json:"otherLinks"`
}

type config struct {
	Browser          string     `json:"browser"`
	CloseAfterLaunch bool       `json:"closeAfterLaunch"`
	Gantt            meeting    `json:"gantt"` // also opens the JobTread view's jobs
	Meetings         []meeting  `json:"meetings"`
	JobTread         jtSettings `json:"jobtread"`
}

// One button on the menu.
type button struct {
	Group string
	Label string
	part  string // Gantt: "first", "second", "all"
	mtg   int    // index into cfg.Meetings when part == ""
}

const ganttGroup = "Weekly Gantt meeting"

func defaultConfig() config {
	m := func(name, meet string, other ...string) meeting {
		return meeting{Name: name, MeetURL: meet, MeetScreen: "left", OtherScreen: "right", OtherLinks: other}
	}
	return config{
		Gantt: m(ganttGroup, "https://ccorps.net/productiongooglemeet"),
		Meetings: []meeting{
			m("In-office meeting", "https://ccorps.net/in-officegooglemeet", "https://ccorps.net/in-officemeetingnotes"),
			m("Company-wide meeting", "https://ccorps.net/companywidemeeting"),
			m("Field team meeting", "https://ccorps.net/fieldteamgooglemeet"),
		},
		JobTread: defaultJT(),
	}
}

func menuButtons() []button {
	b := []button{{ganttGroup, "1st half", "first", 0}, {ganttGroup, "2nd half", "second", 0}, {ganttGroup, "All", "all", 0}}
	for i, m := range cfg.Meetings {
		b = append(b, button{"Meetings", m.Name, "", i})
	}
	return b
}

// windows turns a button into the Chrome windows to open, using current Preferences.
func (b button) windows() []winSpec {
	m := cfg.Gantt
	if b.part == "" {
		m = cfg.Meetings[b.mtg]
	}
	w := []winSpec{{URL: m.MeetURL, Monitor: m.MeetScreen}}
	other := winSpec{URLs: append([]string(nil), m.OtherLinks...), Monitor: m.OtherScreen, JT: b.part}
	if len(other.URLs) > 0 || other.JT != "" {
		w = append(w, other)
	}
	return w
}

func configPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "presets.json"
	}
	return filepath.Join(filepath.Dir(exe), "ScreenLauncher-presets.json")
}

func saveConfig(c config) error {
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), out, 0644)
}

func fixMeeting(m *meeting) {
	if m.MeetScreen == "" {
		m.MeetScreen = "left"
	}
	if m.OtherScreen == "" {
		m.OtherScreen = "right"
	}
}

func loadConfig() (config, string) {
	b, err := os.ReadFile(configPath())
	if err != nil {
		c := defaultConfig()
		_ = saveConfig(c)
		return c, ""
	}
	var c config
	if err := json.Unmarshal(b, &c); err != nil {
		return defaultConfig(), "Settings file has an error - using defaults."
	}
	d := defaultConfig()
	if c.Gantt.MeetURL == "" {
		c.Gantt = d.Gantt
	}
	c.Gantt.Name = ganttGroup
	if c.Meetings == nil {
		c.Meetings = d.Meetings
	}
	fixMeeting(&c.Gantt)
	for i := range c.Meetings {
		fixMeeting(&c.Meetings[i])
	}
	c.JobTread.fillDefaults()
	return c, ""
}

// ---------- win32 ----------

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	pRegisterClassEx     = user32.NewProc("RegisterClassExW")
	pCreateWindowEx      = user32.NewProc("CreateWindowExW")
	pDefWindowProc       = user32.NewProc("DefWindowProcW")
	pGetMessage          = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessage     = user32.NewProc("DispatchMessageW")
	pIsDialogMessage     = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pPostMessage         = user32.NewProc("PostMessageW")
	pSendMessage         = user32.NewProc("SendMessageW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pUpdateWindow        = user32.NewProc("UpdateWindow")
	pSetWindowPos        = user32.NewProc("SetWindowPos")
	pSetWindowText       = user32.NewProc("SetWindowTextW")
	pLoadCursor          = user32.NewProc("LoadCursorW")
	pAdjustWindowRectEx  = user32.NewProc("AdjustWindowRectEx")
	pEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	pEnumWindows         = user32.NewProc("EnumWindows")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
	pGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	pGetWindow           = user32.NewProc("GetWindow")
	pGetClassName        = user32.NewProc("GetClassNameW")
	pGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	pGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pSetWindowPlacement  = user32.NewProc("SetWindowPlacement")
	pMonitorFromWindow   = user32.NewProc("MonitorFromWindow")
	pIsZoomed            = user32.NewProc("IsZoomed")
	pGetDpiForSystem     = user32.NewProc("GetDpiForSystem")
	pSetDpiCtx           = user32.NewProc("SetProcessDpiAwarenessContext")
	pMessageBox          = user32.NewProc("MessageBoxW")
	pEnableWindow        = user32.NewProc("EnableWindow")
	pDestroyWindow       = user32.NewProc("DestroyWindow")
	pGetWindowText       = user32.NewProc("GetWindowTextW")
	pGetWindowRect       = user32.NewProc("GetWindowRect")

	pGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	pOpenProcess     = kernel32.NewProc("OpenProcess")
	pCloseHandle     = kernel32.NewProc("CloseHandle")
	pQueryImageName  = kernel32.NewProc("QueryFullProcessImageNameW")
	pCreateFont      = gdi32.NewProc("CreateFontW")
	pCreateActCtx    = kernel32.NewProc("CreateActCtxW")
	pActivateActCtx  = kernel32.NewProc("ActivateActCtx")
	pInitCommonCtrls = syscall.NewLazyDLL("comctl32.dll").NewProc("InitCommonControls")
)

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }
type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}
type windowPlacement struct {
	Length  uint32
	Flags   uint32
	ShowCmd uint32
	MinPos  point
	MaxPos  point
	Normal  rect
}
type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

const (
	wsOverlapped  = 0x00000000
	wsCaption     = 0x00C00000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsChild       = 0x40000000
	wsVisible     = 0x10000000
	wsTabStop     = 0x00010000
	ssCenter      = 0x00000001

	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmSetFont = 0x0030
	wmCommand = 0x0111

	swRestore  = 9
	swMaximize = 3
	swShow     = 5

	idPresetBase = 100
	idEdit       = 900
)

type actCtx struct {
	Size         uint32
	Flags        uint32
	Source       *uint16
	Arch         uint16
	LangID       uint16
	AssemblyDir  *uint16
	ResourceName uintptr
	AppName      *uint16
	Module       uintptr
}

// Turn on modern (themed) buttons without shipping a manifest:
// borrow the common-controls v6 manifest that lives inside shell32.dll.
func enableVisualStyles() {
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	ac := actCtx{Flags: 0x8, Source: u16(filepath.Join(sys, "System32", "shell32.dll")), ResourceName: 124}
	ac.Size = uint32(unsafe.Sizeof(ac))
	h, _, _ := pCreateActCtx.Call(uintptr(unsafe.Pointer(&ac)))
	if h == 0 || h == ^uintptr(0) {
		return
	}
	var cookie uintptr
	pActivateActCtx.Call(h, uintptr(unsafe.Pointer(&cookie)))
	pInitCommonCtrls.Call()
}

// Timing log, only when SCREENLAUNCHER_LOG is set (path of a log file).
var logT0 = time.Now()

func logf(f string, a ...any) {
	p := os.Getenv("SCREENLAUNCHER_LOG")
	if p == "" {
		return
	}
	if fh, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		fmt.Fprintf(fh, "%6dms  %s\r\n", time.Since(logT0).Milliseconds(), fmt.Sprintf(f, a...))
		fh.Close()
	}
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

// ---------- monitors ----------

type monitor struct {
	h       uintptr
	mon     rect
	work    rect
	primary bool
}

var monAcc []monitor
var monCB = syscall.NewCallback(func(h, hdc, lprc, lp uintptr) uintptr {
	var mi monitorInfo
	mi.Size = uint32(unsafe.Sizeof(mi))
	if r, _, _ := pGetMonitorInfo.Call(h, uintptr(unsafe.Pointer(&mi))); r != 0 {
		monAcc = append(monAcc, monitor{h, mi.Monitor, mi.Work, mi.Flags&1 == 1})
	}
	return 1
})

// returns [main, second, third...] : primary first, the rest left-to-right
func monitors() []monitor {
	monAcc = nil
	pEnumDisplayMonitors.Call(0, 0, monCB, 0)
	var prim *monitor
	var others []monitor
	for i := range monAcc {
		if monAcc[i].primary && prim == nil {
			m := monAcc[i]
			prim = &m
		} else {
			others = append(others, monAcc[i])
		}
	}
	sort.Slice(others, func(a, b int) bool { return others[a].work.Left < others[b].work.Left })
	if prim == nil {
		return others
	}
	wsOffX, wsOffY = prim.work.Left-prim.mon.Left, prim.work.Top-prim.mon.Top
	return append([]monitor{*prim}, others...)
}

func pickMonitor(ms []monitor, name string) monitor {
	idx := 0
	switch n := strings.ToLower(strings.TrimSpace(name)); n {
	case "left", "right": // physical leftmost / rightmost screen
		for i := range ms {
			if (n == "left" && ms[i].work.Left < ms[idx].work.Left) ||
				(n == "right" && ms[i].work.Left > ms[idx].work.Left) {
				idx = i
			}
		}
	case "second", "2":
		idx = 1
	case "third", "3":
		idx = 2
	case "fourth", "4":
		idx = 3
	}
	if idx >= len(ms) {
		idx = 0
	}
	return ms[idx]
}

// ---------- browser windows ----------

var browserExe string // lower-case base name, e.g. chrome.exe
var winAcc map[uintptr]bool
var pidIsBrowser = map[uint32]bool{} // cache: avoids OpenProcess on every poll
var clsBuf = make([]uint16, 64)
var imgBuf = make([]uint16, 1024)
var winCB = syscall.NewCallback(func(h, lp uintptr) uintptr {
	pGetClassName.Call(h, uintptr(unsafe.Pointer(&clsBuf[0])), uintptr(len(clsBuf)))
	if syscall.UTF16ToString(clsBuf) != "Chrome_WidgetWin_1" {
		return 1
	}
	if v, _, _ := pIsWindowVisible.Call(h); v == 0 {
		return 1
	}
	if o, _, _ := pGetWindow.Call(h, 4); o != 0 { // GW_OWNER
		return 1
	}
	if n, _, _ := pGetWindowTextLength.Call(h); n == 0 {
		return 1
	}
	var pid uint32
	pGetWindowThreadPID.Call(h, uintptr(unsafe.Pointer(&pid)))
	ok, seen := pidIsBrowser[pid]
	if !seen {
		if ph, _, _ := pOpenProcess.Call(0x1000, 0, uintptr(pid)); ph != 0 {
			sz := uint32(len(imgBuf))
			r, _, _ := pQueryImageName.Call(ph, 0, uintptr(unsafe.Pointer(&imgBuf[0])), uintptr(unsafe.Pointer(&sz)))
			pCloseHandle.Call(ph)
			ok = r != 0 && strings.ToLower(filepath.Base(syscall.UTF16ToString(imgBuf[:sz]))) == browserExe
		}
		pidIsBrowser[pid] = ok
	}
	if ok {
		winAcc[h] = true
	}
	return 1
})

func browserWindows() map[uintptr]bool {
	winAcc = map[uintptr]bool{}
	pEnumWindows.Call(winCB, 0)
	return winAcc
}

func newWindows(before map[uintptr]bool) []uintptr {
	var fresh []uintptr
	for h := range browserWindows() {
		if !before[h] {
			fresh = append(fresh, h)
		}
	}
	return fresh
}

func findBrowser(override string) string {
	var c []string
	if override != "" {
		c = append(c, override)
	}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		if d := os.Getenv(env); d != "" {
			c = append(c, filepath.Join(d, `Google\Chrome\Application\chrome.exe`))
		}
	}
	for _, p := range c {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// workspace coordinates (used by SetWindowPlacement) are offset by the primary taskbar
var wsOffX, wsOffY int32

func place(h uintptr, m monitor) {
	wp := windowPlacement{ShowCmd: swMaximize, Normal: rect{
		m.work.Left - wsOffX, m.work.Top - wsOffY, m.work.Right - wsOffX, m.work.Bottom - wsOffY}}
	wp.Length = uint32(unsafe.Sizeof(wp))
	// One atomic call, no fixed sleeps. Verify and retry briefly in case Chrome
	// repositions its brand-new window right after we move it.
	good := 0
	for i := 0; i < 40 && good < 2; i++ {
		on, _, _ := pMonitorFromWindow.Call(h, 2)
		z, _, _ := pIsZoomed.Call(h)
		if on == m.h && z != 0 {
			good++
		} else {
			good = 0
			if z != 0 {
				pShowWindow.Call(h, swRestore) // a maximized window ignores a new normal rect
			}
			pSetWindowPlacement.Call(h, uintptr(unsafe.Pointer(&wp)))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func openOne(chrome string, s winSpec, m monitor) error {
	tabs := s.tabs()
	if len(tabs) == 0 {
		return nil
	}
	before := browserWindows()
	cold := len(before) == 0
	logf("launch %d tab(s), cold=%v, existing=%d", len(tabs), cold, len(before))
	args := append([]string{"--new-window"}, tabs...) // all tabs in ONE launch
	if s.App {
		args = []string{"--app=" + tabs[0]}
	}
	cmd := exec.Command(chrome, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		fresh := newWindows(before)
		if len(fresh) == 0 {
			continue
		}
		logf("new window seen (%d)", len(fresh))
		if cold {
			// Cold start: session restore may still be adding windows. Wait until the set stops growing.
			stable := time.Now()
			for limit := time.Now().Add(3 * time.Second); time.Now().Before(limit) && time.Since(stable) < 500*time.Millisecond; {
				time.Sleep(100 * time.Millisecond)
				if f := newWindows(before); len(f) != len(fresh) {
					fresh, stable = f, time.Now()
				}
			}
		}
		target := fresh[0]
		fg, _, _ := pGetForegroundWindow.Call()
		for _, h := range fresh {
			if h == fg {
				target = h
			}
		}
		place(target, m)
		logf("placed")
		return nil
	}
	return fmt.Errorf("the browser window never appeared")
}

// ---------- UI ----------

var (
	mainWnd   uintptr
	statusWnd uintptr
	cfg       config
	shown     []button  // buttons on the menu, in id order
	menuCtls  []uintptr // controls on the menu (rebuilt after Preferences)
	busy      int32

	hInst                         uintptr
	dpiV                          = 96
	fontTitle, fontBtn, fontSmall uintptr
)

func sc(v int) int { return v * dpiV / 96 }

func setStatus(s string) { pSetWindowText.Call(statusWnd, uintptr(unsafe.Pointer(u16(s)))) }

func mkCtl(parent, ex uintptr, class, text string, st uintptr, x, y, w, h, id int, font uintptr) uintptr {
	c, _, _ := pCreateWindowEx.Call(ex, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
		wsChild|wsVisible|st, uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), hInst, 0)
	pSendMessage.Call(c, wmSetFont, font, 1)
	return c
}

func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLength.Call(h)
	buf := make([]uint16, n+1)
	pGetWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func runPreset(label string, wins []winSpec) {
	defer atomic.StoreInt32(&busy, 0)
	logf("click: %s", label)
	chrome := findBrowser(cfg.Browser)
	if chrome == "" {
		setStatus("Could not find Google Chrome on this PC.")
		return
	}
	browserExe = strings.ToLower(filepath.Base(chrome))
	pidIsBrowser = map[uint32]bool{}
	ms := monitors()
	if len(ms) == 0 {
		setStatus("No monitors detected.")
		return
	}
	jts := cfg.JobTread
	// Fetch JobTread jobs in the background while the other windows open.
	type jtResult struct {
		ids []string
		err error
	}
	var jtc chan jtResult
	for _, s := range wins {
		if s.JT != "" {
			jtc = make(chan jtResult, 1)
			go func() {
				ids, err := jtJobIDs(jts)
				logf("jobtread: %d jobs, err=%v", len(ids), err)
				jtc <- jtResult{ids, err}
			}()
			break
		}
	}
	var jt *jtResult
	problem := ""
	for i, s := range wins {
		setStatus(fmt.Sprintf("Opening window %d of %d...", i+1, len(wins)))
		if s.JT != "" {
			if jt == nil {
				setStatus("Loading JobTread jobs...")
				r := <-jtc
				jt = &r
			}
			if jt.err != nil {
				problem = jt.err.Error()
			} else {
				var jobs []string // job tabs first, then the extra links
				for _, id := range jtPart(jt.ids, s.JT) {
					jobs = append(jobs, strings.ReplaceAll(jts.ScheduleURL, "{id}", id))
				}
				s.URLs = append(jobs, s.URLs...)
			}
		}
		if len(s.tabs()) == 0 {
			continue
		}
		if err := openOne(chrome, s, pickMonitor(ms, s.Monitor)); err != nil {
			problem = err.Error()
		}
	}
	if problem != "" {
		setStatus("Problem: " + problem)
		return
	}
	logf("all done")
	if cfg.CloseAfterLaunch {
		pPostMessage.Call(mainWnd, wmClose, 0, 0)
		return
	}
	setStatus("Done. Pick another layout anytime.")
}

func wndProc(h uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmCommand:
		id := int(wp & 0xFFFF)
		if id == idEdit {
			openPrefs()
			return 0
		}
		if id >= idPresetBase && id < idPresetBase+len(shown) {
			if atomic.CompareAndSwapInt32(&busy, 0, 1) {
				b := shown[id-idPresetBase]
				go runPreset(b.Group+" / "+b.Label, b.windows())
			} else {
				setStatus("Still opening - one moment...")
			}
			return 0
		}
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(h, uintptr(m), wp, lp)
	return r
}

// buildMenu (re)creates the menu's buttons from cfg and sizes the window to fit.
func buildMenu(status string) {
	for _, c := range menuCtls {
		pDestroyWindow.Call(c)
	}
	menuCtls = nil
	shown = menuButtons()

	pad, bw, bh, gap := sc(18), sc(420), sc(42), sc(10)
	type row struct {
		head string
		idx  []int
	}
	// Gantt: one heading + a row of three. Meetings: one heading + one button per row.
	rows := []row{{ganttGroup, []int{0, 1, 2}}}
	for i := 3; i < len(shown); i++ {
		head := ""
		if i == 3 {
			head = "Meetings"
		}
		rows = append(rows, row{head, []int{i}})
	}
	ctl := func(class, text string, st uintptr, cx, cy, cw, ch, id int, font uintptr) uintptr {
		c := mkCtl(mainWnd, 0, class, text, st, cx, cy, cw, ch, id, font)
		menuCtls = append(menuCtls, c)
		return c
	}

	cy := pad
	if bmp, lw, lh := menuLogo(sc(150)); bmp != 0 {
		lg := ctl("STATIC", "", 0x0E, pad+(bw-lw)/2, cy, lw, lh, 0, fontTitle) // SS_BITMAP
		pSendMessage.Call(lg, 0x0172, 0, bmp)                                  // STM_SETIMAGE, IMAGE_BITMAP
		cy += lh + sc(8)
	}
	ctl("STATIC", "Meeting Launcher", ssCenter, pad, cy, bw, sc(26), 0, fontTitle)
	cy += sc(26) + gap
	for _, r := range rows {
		if r.head != "" {
			cy += sc(4)
			ctl("STATIC", r.head, 0, pad, cy, bw, sc(20), 0, fontBtn)
			cy += sc(22)
		}
		k := len(r.idx)
		w := (bw - (k-1)*gap) / k
		for j, i := range r.idx {
			ctl("BUTTON", shown[i].Label, wsTabStop, pad+j*(w+gap), cy, w, bh, idPresetBase+i, fontBtn)
		}
		cy += bh + gap
	}
	cy += sc(8)
	statusWnd = ctl("STATIC", status, ssCenter, pad, cy, bw, sc(22), 0, fontSmall)
	cy += sc(22) + gap
	ctl("BUTTON", "Preferences...", wsTabStop, pad+bw-sc(130), cy, sc(130), sc(28), idEdit, fontSmall)
	cy += sc(28) + pad

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	r := rect{0, 0, int32(bw + 2*pad), int32(cy)}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), style, 0, 0)
	pSetWindowPos.Call(mainWnd, 0, 0, 0, uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x0002|0x0004|0x0010) // NOMOVE|NOZORDER|NOACTIVATE
}

func main() {
	runtime.LockOSThread()
	pSetDpiCtx.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2 (-4); no-op if the manifest already set it

	enableVisualStyles()

	var warn string
	cfg, warn = loadConfig()

	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d != 0 {
			dpiV = int(d)
		}
	}

	hInst, _, _ = pGetModuleHandle.Call(0)
	cur, _, _ := pLoadCursor.Call(0, 32512)
	cls := u16("CCScreenLauncher")
	wc := wndClassEx{WndProc: syscall.NewCallback(wndProc), Instance: hInst, Cursor: cur, Background: 16, ClassName: cls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	registerPrefsClass(cur)

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox)
	mainWnd, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(u16(appTitle))),
		style, 200, 200, 400, 400, 0, 0, hInst, 0)
	if mainWnd == 0 {
		pMessageBox.Call(0, uintptr(unsafe.Pointer(u16("Could not create the window."))), uintptr(unsafe.Pointer(u16(appTitle))), 0x10)
		return
	}

	mkFont := func(pt int, weight int) uintptr {
		f, _, _ := pCreateFont.Call(uintptr(-(pt * dpiV / 72)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0,
			uintptr(unsafe.Pointer(u16("Segoe UI"))))
		return f
	}
	fontTitle, fontBtn, fontSmall = mkFont(12, 600), mkFont(10, 400), mkFont(9, 400)
	setWindowIcon(mainWnd)

	ms := monitors()
	st := fmt.Sprintf("%d screen(s) detected", len(ms))
	if warn != "" {
		st = warn
	}
	buildMenu(st)

	// center on the main screen
	if len(ms) > 0 {
		var wr rect
		pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&wr)))
		ww, wh := int(wr.Right-wr.Left), int(wr.Bottom-wr.Top)
		w := ms[0].work
		x := int(w.Left) + (int(w.Right-w.Left)-ww)/2
		y := int(w.Top) + (int(w.Bottom-w.Top)-wh)/2
		pSetWindowPos.Call(mainWnd, 0, uintptr(x), uintptr(y), 0, 0, 0x0001|0x0004) // NOSIZE|NOZORDER
	}

	pShowWindow.Call(mainWnd, swShow)
	pUpdateWindow.Call(mainWnd)
	pSetForegroundWindow.Call(mainWnd)

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		dlg := mainWnd
		if prefsWnd != 0 {
			dlg = prefsWnd
		}
		if d, _, _ := pIsDialogMessage.Call(dlg, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

const appTitle = "Construction Corps Meeting Launcher"
