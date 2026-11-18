//go:build windows

package main

// Preferences window: a form with fields, so nobody edits a text file.

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	idSave     = 1 // IDOK: Enter key
	idCancel   = 2 // IDCANCEL: Esc key
	idMtg      = 1001
	idAdd      = 1002
	idDel      = 1003
	idName     = 1004
	idMeet     = 1005
	idMeetScr  = 1006
	idOtherScr = 1007
	idOther    = 1008
	idView     = 1009
	idCloseChk = 1010
	idChrome   = 1011
	// SD-specific
	idSDListView   = 1020
	idSDKanbanView = 1021

	wmViewsLoaded = 0x8001 // WM_APP + 1
	cbnSelChange  = 1

	cbAddString    = 0x0143
	cbDeleteString = 0x0144
	cbInsertString = 0x014A
	cbSetCurSel    = 0x014E
	cbGetCurSel    = 0x0147
	cbResetContent = 0x014B
	bmGetCheck     = 0x00F0
	bmSetCheck     = 0x00F1
	emSetSel       = 0x00B1

	esMultiline     = 0x0004
	esAutoVScroll   = 0x0040
	esAutoHScroll   = 0x0080
	esWantReturn    = 0x1000
	cbsDropDownList = 0x0003
	bsAutoCheckbox  = 0x0003
	bsDefPushButton = 0x0001
	wsVScroll       = 0x00200000
	wsExClientEdge  = 0x00000200
	wsExDlgModal    = 0x00000001
)

var screenOpts = []struct{ label, val string }{
	{"Left screen", "left"}, {"Right screen", "right"}, {"Main screen", "main"},
	{"Second screen", "second"}, {"Third screen", "third"},
	{"Main screen (minimized)", "minimized"},
}

var (
	prefsWnd   uintptr
	prefsCls   = u16("CCScreenLauncherPrefs")
	prefsCtl   = map[int]uintptr{}
	viewsCh    = make(chan []jtView, 1)
	prefViews  []jtView  // loaded list; nil until JobTread answers
	pm         []meeting // working copy: [0] = Gantt, then the meetings
	pmCur      int
	prefsProcC = syscall.NewCallback(prefsProc)
	pSetFocus  = user32.NewProc("SetFocus")
)

func registerPrefsClass(cursor uintptr) {
	wc := wndClassEx{WndProc: prefsProcC, Instance: hInst, Cursor: cursor, Background: 16, ClassName: prefsCls}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
}

func screenIndex(v string) int {
	for i, o := range screenOpts {
		if o.val == strings.ToLower(strings.TrimSpace(v)) {
			return i
		}
	}
	return 0
}

func comboAdd(h uintptr, s string) {
	pSendMessage.Call(h, cbAddString, 0, uintptr(unsafe.Pointer(u16(s))))
}
func setText(h uintptr, s string) { pSetWindowText.Call(h, uintptr(unsafe.Pointer(u16(s)))) }
func comboSel(id int) int {
	i, _, _ := pSendMessage.Call(prefsCtl[id], cbGetCurSel, 0, 0)
	return int(int32(i))
}

func normURL(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"`)
	if s != "" && !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return s
}

func openPrefs() {
	if prefsWnd != 0 {
		pSetForegroundWindow.Call(prefsWnd)
		return
	}

	// SD variant gets a simpler preferences dialog
	if Variant == "SD" {
		openSDPrefs()
		return
	}

	pad, lw, fw, rh, gap := sc(18), sc(220), sc(380), sc(28), sc(10)
	otherH := 4 * rh
	clientW := pad + lw + fw + pad
	clientH := pad + 5*(rh+gap) + otherH + sc(20) + gap + sc(14) + 3*(rh+gap) + sc(6) + sc(32) + pad

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu)
	r := rect{0, 0, int32(clientW), int32(clientH)}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), style, 0, wsExDlgModal)
	ww, wh := int(r.Right-r.Left), int(r.Bottom-r.Top)
	var mr rect
	pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&mr)))
	x := int(mr.Left) + (int(mr.Right-mr.Left)-ww)/2
	y := int(mr.Top) + (int(mr.Bottom-mr.Top)-wh)/2
	if y < 0 {
		y = 0
	}

	// Owned by the menu (stays above the menu only, never "always on top").
	prefsWnd, _, _ = pCreateWindowEx.Call(wsExDlgModal, uintptr(unsafe.Pointer(prefsCls)),
		uintptr(unsafe.Pointer(u16("Preferences"))), style,
		uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), mainWnd, 0, hInst, 0)
	if prefsWnd == 0 {
		return
	}
	setWindowIcon(prefsWnd)
	prefsCtl = map[int]uintptr{}
	pm = append([]meeting{cfg.Gantt}, cfg.Meetings...)
	pmCur = 0

	cy := pad
	label := func(t string) {
		mkCtl(prefsWnd, 0, "STATIC", t, 0, pad, cy+sc(5), lw-sc(8), rh, 0, fontBtn)
	}
	combo := func(id, x, w int) uintptr {
		h := mkCtl(prefsWnd, 0, "COMBOBOX", "", cbsDropDownList|wsVScroll|wsTabStop, x, cy, w, sc(300), id, fontBtn)
		prefsCtl[id] = h
		return h
	}
	edit := func(id int, text string) {
		prefsCtl[id] = mkCtl(prefsWnd, wsExClientEdge, "EDIT", text, esAutoHScroll|wsTabStop, pad+lw, cy, fw, rh, id, fontBtn)
	}
	next := func() { cy += rh + gap }

	// --- per-meeting fields ---
	label("Meeting:")
	sbw := sc(80)
	hm := combo(idMtg, pad+lw, fw-2*(sbw+sc(8)))
	for _, m := range pm {
		comboAdd(hm, m.Name)
	}
	pSendMessage.Call(hm, cbSetCurSel, 0, 0)
	prefsCtl[idAdd] = mkCtl(prefsWnd, 0, "BUTTON", "Add", wsTabStop, pad+lw+fw-2*sbw-sc(8), cy, sbw, rh, idAdd, fontBtn)
	prefsCtl[idDel] = mkCtl(prefsWnd, 0, "BUTTON", "Delete", wsTabStop, pad+lw+fw-sbw, cy, sbw, rh, idDel, fontBtn)
	next()

	label("Button name:")
	edit(idName, "")
	next()
	label("Google Meet link:")
	edit(idMeet, "")
	next()
	for _, f := range []struct {
		lbl string
		id  int
	}{{"Meet opens on:", idMeetScr}, {"Other links open on:", idOtherScr}} {
		label(f.lbl)
		h := combo(f.id, pad+lw, fw)
		for _, o := range screenOpts {
			comboAdd(h, o.label)
		}
		next()
	}
	label("Other links (one per line):")
	prefsCtl[idOther] = mkCtl(prefsWnd, wsExClientEdge, "EDIT", "",
		esMultiline|esAutoVScroll|esWantReturn|wsVScroll|wsTabStop, pad+lw, cy, fw, otherH, idOther, fontBtn)
	cy += otherH + sc(2)
	mkCtl(prefsWnd, 0, "STATIC", "They open as tabs in one window. Gantt: the JobTread jobs open first in that window.",
		0, pad+lw, cy, fw, sc(20), 0, fontSmall)
	cy += sc(20) + gap

	// --- general settings ---
	mkCtl(prefsWnd, 0, "STATIC", "", 0x10, pad, cy+sc(6), clientW-2*pad, 2, 0, fontSmall) // SS_ETCHEDHORZ
	cy += sc(14)
	label("Gantt jobs list (JobTread):")
	hv := combo(idView, pad+lw, fw)
	comboAdd(hv, cfg.JobTread.ViewName)
	pSendMessage.Call(hv, cbSetCurSel, 0, 0)
	next()

	ck := mkCtl(prefsWnd, 0, "BUTTON", "Close this menu after opening the windows", bsAutoCheckbox|wsTabStop,
		pad+lw, cy, fw, rh, idCloseChk, fontBtn)
	prefsCtl[idCloseChk] = ck
	if cfg.CloseAfterLaunch {
		pSendMessage.Call(ck, bmSetCheck, 1, 0)
	}
	next()

	label("Chrome location (blank = auto):")
	edit(idChrome, cfg.Browser)
	next()

	cy += sc(6)
	bw := sc(110)
	mkCtl(prefsWnd, 0, "BUTTON", "Cancel", wsTabStop, clientW-pad-bw, cy, bw, sc(32), idCancel, fontBtn)
	mkCtl(prefsWnd, 0, "BUTTON", "Save", bsDefPushButton|wsTabStop, clientW-pad-2*bw-sc(10), cy, bw, sc(32), idSave, fontBtn)

	loadFields()
	pEnableWindow.Call(mainWnd, 0) // menu waits while Preferences is open
	pShowWindow.Call(prefsWnd, swShow)
	pSetForegroundWindow.Call(prefsWnd)

	// Load the list of saved Jobs views in the background.
	select {
	case <-viewsCh:
	default:
	}
	prefViews = nil
	h, jts := prefsWnd, cfg.JobTread
	go func() {
		if v, err := jtViews(jts); err == nil && len(v) > 0 {
			viewsCh <- v
			pPostMessage.Call(h, wmViewsLoaded, 0, 0)
		}
	}()
}

// loadFields shows pm[pmCur] in the per-meeting fields.
func loadFields() {
	m := pm[pmCur]
	setText(prefsCtl[idName], m.Name)
	setText(prefsCtl[idMeet], m.MeetURL)
	setText(prefsCtl[idOther], strings.Join(m.OtherLinks, "\r\n"))
	pSendMessage.Call(prefsCtl[idMeetScr], cbSetCurSel, uintptr(screenIndex(m.MeetScreen)), 0)
	pSendMessage.Call(prefsCtl[idOtherScr], cbSetCurSel, uintptr(screenIndex(m.OtherScreen)), 0)
	notGantt := uintptr(0)
	if pmCur > 0 {
		notGantt = 1
	}
	pEnableWindow.Call(prefsCtl[idName], notGantt) // the Gantt meeting keeps its name
	pEnableWindow.Call(prefsCtl[idDel], notGantt)
}

// storeFields copies the per-meeting fields back into pm[pmCur].
func storeFields() {
	m := &pm[pmCur]
	if pmCur > 0 {
		if n := strings.TrimSpace(getText(prefsCtl[idName])); n != "" && n != m.Name {
			m.Name = n
			hm := prefsCtl[idMtg]
			pSendMessage.Call(hm, cbDeleteString, uintptr(pmCur), 0)
			pSendMessage.Call(hm, cbInsertString, uintptr(pmCur), uintptr(unsafe.Pointer(u16(n))))
			pSendMessage.Call(hm, cbSetCurSel, uintptr(pmCur), 0)
		}
	}
	m.MeetURL = normURL(getText(prefsCtl[idMeet]))
	m.OtherLinks = nil
	for _, l := range strings.Split(getText(prefsCtl[idOther]), "\n") {
		if u := normURL(l); u != "" {
			m.OtherLinks = append(m.OtherLinks, u)
		}
	}
	if i := comboSel(idMeetScr); i >= 0 && i < len(screenOpts) {
		m.MeetScreen = screenOpts[i].val
	}
	if i := comboSel(idOtherScr); i >= 0 && i < len(screenOpts) {
		m.OtherScreen = screenOpts[i].val
	}
}

func selectMeeting(i int) {
	pmCur = i
	pSendMessage.Call(prefsCtl[idMtg], cbSetCurSel, uintptr(i), 0)
	loadFields()
}

func closePrefs() {
	if prefsWnd == 0 {
		return
	}
	pEnableWindow.Call(mainWnd, 1)
	pDestroyWindow.Call(prefsWnd)
	prefsWnd = 0
	pSetForegroundWindow.Call(mainWnd)
}

func msgBox(text string, flags uintptr) int {
	r, _, _ := pMessageBox.Call(prefsWnd, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16("Preferences"))), flags)
	return int(r)
}

func savePrefs() bool {
	storeFields()
	chrome := strings.Trim(strings.TrimSpace(getText(prefsCtl[idChrome])), `"`)
	if chrome != "" {
		if _, err := os.Stat(chrome); err != nil {
			msgBox("Chrome was not found at that location.\nLeave it blank to find Chrome automatically.", 0x30)
			return false
		}
	}
	c := cfg
	c.Gantt = pm[0]
	c.Gantt.Name = ganttGroup
	c.Meetings = append([]meeting{}, pm[1:]...)
	if i := comboSel(idView); prefViews != nil && i >= 0 && i < len(prefViews) {
		c.JobTread.ViewID, c.JobTread.ViewName = prefViews[i].ID, prefViews[i].Name
	}
	chk, _, _ := pSendMessage.Call(prefsCtl[idCloseChk], bmGetCheck, 0, 0)
	c.CloseAfterLaunch = chk == 1
	c.Browser = chrome

	if err := saveConfig(c); err != nil {
		msgBox("Could not save: "+err.Error(), 0x10)
		return false
	}
	cfg = c
	return true
}

func prefsProc(h uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case wmCommand:
		id, code := int(wp&0xFFFF), int(wp>>16&0xFFFF)
		switch id {
		case idMtg:
			if code == cbnSelChange {
				if i := comboSel(idMtg); i >= 0 && i < len(pm) && i != pmCur {
					storeFields()
					selectMeeting(i)
				}
			}
			return 0
		case idAdd:
			storeFields()
			pm = append(pm, meeting{Name: "New meeting", MeetScreen: "left", OtherScreen: "right"})
			comboAdd(prefsCtl[idMtg], "New meeting")
			selectMeeting(len(pm) - 1)
			pSetFocus.Call(prefsCtl[idName])
			pSendMessage.Call(prefsCtl[idName], emSetSel, 0, ^uintptr(0))
			return 0
		case idDel:
			if pmCur > 0 && msgBox("Delete \""+pm[pmCur].Name+"\"?", 0x4|0x30) == 6 { // YES
				pm = append(pm[:pmCur], pm[pmCur+1:]...)
				pSendMessage.Call(prefsCtl[idMtg], cbDeleteString, uintptr(pmCur), 0)
				selectMeeting(pmCur - 1)
			}
			return 0
		case idSave:
			if Variant == "SD" {
				if saveSDPrefs() {
					buildMenu("Preferences saved.")
					closePrefs()
				}
			} else {
				if savePrefs() {
					buildMenu("Preferences saved.")
					closePrefs()
				}
			}
			return 0
		case idCancel:
			closePrefs()
			return 0
		}
	case wmClose:
		closePrefs()
		return 0
	case wmViewsLoaded:
		select {
		case v := <-viewsCh:
			if h != prefsWnd {
				return 0
			}
			prefViews = v
			// Update the appropriate view dropdowns based on variant
			if Variant == "SD" {
				// Update both list and kanban dropdowns for SD
				for _, pair := range []struct {
					id      int
					curID   string
				}{
					{idSDListView, cfg.SalesDisplay.ListViewID},
					{idSDKanbanView, cfg.SalesDisplay.KanbanViewID},
				} {
					hv := prefsCtl[pair.id]
					if hv == 0 {
						continue
					}
					pSendMessage.Call(hv, cbResetContent, 0, 0)
					cur := 0
					for i, x := range v {
						comboAdd(hv, x.Name)
						if x.ID == pair.curID {
							cur = i
						}
					}
					pSendMessage.Call(hv, cbSetCurSel, uintptr(cur), 0)
				}
			} else {
				hv := prefsCtl[idView]
				pSendMessage.Call(hv, cbResetContent, 0, 0)
				cur := 0
				for i, x := range v {
					comboAdd(hv, x.Name)
					if x.ID == cfg.JobTread.ViewID {
						cur = i
					}
				}
				pSendMessage.Call(hv, cbSetCurSel, uintptr(cur), 0)
			}
		default:
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(h, uintptr(m), wp, lp)
	return r
}

// ---------- SD Preferences ----------

func openSDPrefs() {
	pad, lw, fw, rh, gap := sc(18), sc(220), sc(380), sc(28), sc(10)
	clientW := pad + lw + fw + pad
	clientH := pad + 5*(rh+gap) + sc(14) + 2*(rh+gap) + sc(6) + sc(32) + pad

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu)
	r := rect{0, 0, int32(clientW), int32(clientH)}
	pAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&r)), style, 0, wsExDlgModal)
	ww, wh := int(r.Right-r.Left), int(r.Bottom-r.Top)
	var mr rect
	pGetWindowRect.Call(mainWnd, uintptr(unsafe.Pointer(&mr)))
	x := int(mr.Left) + (int(mr.Right-mr.Left)-ww)/2
	y := int(mr.Top) + (int(mr.Bottom-mr.Top)-wh)/2
	if y < 0 {
		y = 0
	}

	prefsWnd, _, _ = pCreateWindowEx.Call(wsExDlgModal, uintptr(unsafe.Pointer(prefsCls)),
		uintptr(unsafe.Pointer(u16("Sales Display Preferences"))), style,
		uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), mainWnd, 0, hInst, 0)
	if prefsWnd == 0 {
		return
	}
	setWindowIcon(prefsWnd)
	prefsCtl = map[int]uintptr{}

	// Ensure SalesDisplay config exists
	if cfg.SalesDisplay == nil {
		cfg.SalesDisplay = &sdSettings{
			MeetURL:       "https://ccorps.net/companywidemeeting",
			MeetMinimized: true,
			ListViewID:    "22PheCYEmTBN",
			KanbanViewID:  "22PggTBEEDCM",
		}
	}

	cy := pad
	label := func(t string) {
		mkCtl(prefsWnd, 0, "STATIC", t, 0, pad, cy+sc(5), lw-sc(8), rh, 0, fontBtn)
	}
	combo := func(id, x, w int) uintptr {
		h := mkCtl(prefsWnd, 0, "COMBOBOX", "", cbsDropDownList|wsVScroll|wsTabStop, x, cy, w, sc(300), id, fontBtn)
		prefsCtl[id] = h
		return h
	}
	edit := func(id int, text string) {
		prefsCtl[id] = mkCtl(prefsWnd, wsExClientEdge, "EDIT", text, esAutoHScroll|wsTabStop, pad+lw, cy, fw, rh, id, fontBtn)
	}
	next := func() { cy += rh + gap }

	// --- SD-specific fields ---
	label("Google Meet link:")
	edit(idMeet, cfg.SalesDisplay.MeetURL)
	next()

	label("Meet opens on:")
	hms := combo(idMeetScr, pad+lw, fw)
	for _, o := range screenOpts {
		comboAdd(hms, o.label)
	}
	// Set to minimized if that's the setting
	meetIdx := len(screenOpts) - 1 // default to minimized (last option)
	if !cfg.SalesDisplay.MeetMinimized {
		meetIdx = 2 // main screen
	}
	pSendMessage.Call(hms, cbSetCurSel, uintptr(meetIdx), 0)
	next()

	label("Sales Pipeline (List View):")
	hlv := combo(idSDListView, pad+lw, fw)
	comboAdd(hlv, "Loading views...")
	pSendMessage.Call(hlv, cbSetCurSel, 0, 0)
	next()

	label("Sales Pipeline (Kanban):")
	hkv := combo(idSDKanbanView, pad+lw, fw)
	comboAdd(hkv, "Loading views...")
	pSendMessage.Call(hkv, cbSetCurSel, 0, 0)
	next()

	// --- general settings ---
	mkCtl(prefsWnd, 0, "STATIC", "", 0x10, pad, cy+sc(6), clientW-2*pad, 2, 0, fontSmall) // SS_ETCHEDHORZ
	cy += sc(14)

	ck := mkCtl(prefsWnd, 0, "BUTTON", "Close this menu after launching", bsAutoCheckbox|wsTabStop,
		pad+lw, cy, fw, rh, idCloseChk, fontBtn)
	prefsCtl[idCloseChk] = ck
	if cfg.CloseAfterLaunch {
		pSendMessage.Call(ck, bmSetCheck, 1, 0)
	}
	next()

	label("Chrome location (blank = auto):")
	edit(idChrome, cfg.Browser)
	next()

	cy += sc(6)
	bw := sc(110)
	mkCtl(prefsWnd, 0, "BUTTON", "Cancel", wsTabStop, clientW-pad-bw, cy, bw, sc(32), idCancel, fontBtn)
	mkCtl(prefsWnd, 0, "BUTTON", "Save", bsDefPushButton|wsTabStop, clientW-pad-2*bw-sc(10), cy, bw, sc(32), idSave, fontBtn)

	pEnableWindow.Call(mainWnd, 0)
	pShowWindow.Call(prefsWnd, swShow)
	pSetForegroundWindow.Call(prefsWnd)

	// Load views in background
	select {
	case <-viewsCh:
	default:
	}
	prefViews = nil
	h, jts := prefsWnd, cfg.JobTread
	go func() {
		if v, err := jtViews(jts); err == nil && len(v) > 0 {
			viewsCh <- v
			pPostMessage.Call(h, wmViewsLoaded, 0, 0)
		}
	}()
}

func saveSDPrefs() bool {
	chrome := strings.Trim(strings.TrimSpace(getText(prefsCtl[idChrome])), `"`)
	if chrome != "" {
		if _, err := os.Stat(chrome); err != nil {
			msgBox("Chrome was not found at that location.\nLeave it blank to find Chrome automatically.", 0x30)
			return false
		}
	}

	c := cfg
	if c.SalesDisplay == nil {
		c.SalesDisplay = &sdSettings{}
	}
	c.SalesDisplay.MeetURL = normURL(getText(prefsCtl[idMeet]))

	// Check if minimized is selected
	if i := comboSel(idMeetScr); i >= 0 && i < len(screenOpts) {
		c.SalesDisplay.MeetMinimized = screenOpts[i].val == "minimized"
	}

	// Get selected views
	if prefViews != nil {
		if i := comboSel(idSDListView); i >= 0 && i < len(prefViews) {
			c.SalesDisplay.ListViewID = prefViews[i].ID
		}
		if i := comboSel(idSDKanbanView); i >= 0 && i < len(prefViews) {
			c.SalesDisplay.KanbanViewID = prefViews[i].ID
		}
	}

	chk, _, _ := pSendMessage.Call(prefsCtl[idCloseChk], bmGetCheck, 0, 0)
	c.CloseAfterLaunch = chk == 1
	c.Browser = chrome

	if err := saveConfig(c); err != nil {
		msgBox("Could not save: "+err.Error(), 0x10)
		return false
	}
	cfg = c
	return true
}
