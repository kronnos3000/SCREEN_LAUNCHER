//go:build windows

package main

// Construction Corps logo: shown at the top of the menu and used as the window icon.
// Decoded and scaled once at startup; the bitmaps are a few KB.

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
	"unsafe"
)

//go:embed assets/logo.png
var logoPNG []byte

var (
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap       = gdi32.NewProc("CreateBitmap")
	pCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	pGetSysColor        = user32.NewProc("GetSysColor")
	pGetSystemMetrics   = user32.NewProc("GetSystemMetrics")

	logoSrc *image.NRGBA
	logoBmp uintptr // menu logo, pre-blended on the window background
	logoW   int
	logoH   int
)

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

func loadLogo() bool {
	if logoSrc != nil {
		return true
	}
	img, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		return false
	}
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, n.Bounds(), img, img.Bounds().Min, draw.Src)
	logoSrc = n
	return true
}

// scale area-averages region r of the logo into w x h premultiplied RGBA (4 bytes/pixel).
func scale(r image.Rectangle, w, h int) []uint32 {
	out := make([]uint32, w*h*4)
	sw, sh := r.Dx(), r.Dy()
	for y := 0; y < h; y++ {
		y0, y1 := r.Min.Y+y*sh/h, r.Min.Y+(y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := r.Min.X+x*sw/w, r.Min.X+(x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var pr, pg, pb, pa, cnt uint32
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cnt++
					if !(image.Point{sx, sy}.In(logoSrc.Rect)) {
						continue // padding outside the image is transparent
					}
					i := logoSrc.PixOffset(sx, sy)
					p := logoSrc.Pix[i : i+4]
					a := uint32(p[3])
					pr += uint32(p[0]) * a
					pg += uint32(p[1]) * a
					pb += uint32(p[2]) * a
					pa += a
				}
			}
			o := (y*w + x) * 4
			out[o], out[o+1], out[o+2], out[o+3] = pr/(cnt*255), pg/(cnt*255), pb/(cnt*255), pa/cnt
		}
	}
	return out
}

// dib creates a top-down 32-bit bitmap and lets fill write its BGRA bytes.
func dib(w, h int, fill func(px []byte)) uintptr {
	bi := bitmapInfoHeader{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer // pixel memory owned by GDI, not the Go heap
	hb, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if hb == 0 || bits == nil {
		return 0
	}
	fill(unsafe.Slice((*byte)(bits), w*h*4))
	return hb
}

// menuLogo returns the full logo, height h, blended on the window background colour.
func menuLogo(h int) (uintptr, int, int) {
	if logoBmp != 0 || !loadLogo() {
		return logoBmp, logoW, logoH
	}
	b := logoSrc.Rect
	w := h * b.Dx() / b.Dy()
	px := scale(b, w, h)
	bg, _, _ := pGetSysColor.Call(15) // COLOR_BTNFACE, 0x00BBGGRR
	bgR, bgG, bgB := uint32(bg&0xFF), uint32(bg>>8&0xFF), uint32(bg>>16&0xFF)
	logoBmp = dib(w, h, func(d []byte) {
		for i := 0; i < w*h; i++ {
			r, g, bl, a := px[i*4], px[i*4+1], px[i*4+2], px[i*4+3]
			d[i*4+0] = byte(bl + bgB*(255-a)/255)
			d[i*4+1] = byte(g + bgG*(255-a)/255)
			d[i*4+2] = byte(r + bgR*(255-a)/255)
			d[i*4+3] = 255
		}
	})
	logoW, logoH = w, h
	return logoBmp, w, h
}

// shieldIcon makes a square icon from the shield (the logo without its banner).
func shieldIcon(size int) uintptr {
	if !loadLogo() || size <= 0 {
		return 0
	}
	b := logoSrc.Rect
	top, bottom := 0, b.Dy()*72/100 // shield occupies roughly the top 72%
	side := b.Dx()
	if bottom-top > side {
		side = bottom - top
	}
	cx, cy := b.Dx()/2, (top+bottom)/2
	r := image.Rect(cx-side/2, cy-side/2, cx-side/2+side, cy-side/2+side)
	px := scale(r, size, size)
	color := dib(size, size, func(d []byte) {
		for i := 0; i < size*size; i++ {
			a := px[i*4+3]
			un := func(v uint32) byte { // icons want straight (not premultiplied) alpha
				if a == 0 {
					return 0
				}
				x := v * 255 / a
				if x > 255 {
					x = 255
				}
				return byte(x)
			}
			d[i*4+0], d[i*4+1], d[i*4+2], d[i*4+3] = un(px[i*4+2]), un(px[i*4+1]), un(px[i*4]), byte(a)
		}
	})
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	ii := iconInfo{FIcon: 1, HbmMask: mask, HbmColor: color}
	ic, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return ic
}

// setWindowIcon puts the shield in a window's title bar and taskbar button.
var bigIcon, smallIcon uintptr

func setWindowIcon(h uintptr) {
	if bigIcon == 0 {
		cx, _, _ := pGetSystemMetrics.Call(11)  // SM_CXICON
		scx, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
		bigIcon, smallIcon = shieldIcon(int(cx)), shieldIcon(int(scx))
	}
	pSendMessage.Call(h, 0x0080, 1, bigIcon)   // WM_SETICON, ICON_BIG
	pSendMessage.Call(h, 0x0080, 0, smallIcon) // ICON_SMALL
}
