//go:build windows

package main

import (
	"bytes"
	"image"
	_ "image/png"
	"math"
	"syscall"
	"unsafe"
)

// Minimal Direct2D + DirectWrite bridge used by PowerPilot's custom UI.
// Native EDIT controls remain Win32 so keyboard/IME/accessibility behavior stays reliable;
// all custom shapes, cards, borders, text, toggles and the settings icon are rendered here.

var (
	d2d1DLL              = syscall.NewLazyDLL("d2d1.dll")
	dwriteDLL            = syscall.NewLazyDLL("dwrite.dll")
	pD2D1CreateFactory   = d2d1DLL.NewProc("D2D1CreateFactory")
	pDWriteCreateFactory = dwriteDLL.NewProc("DWriteCreateFactory")
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var iidID2D1Factory = guid{0x06152247, 0x6f50, 0x465a, [8]byte{0x92, 0x45, 0x11, 0x8b, 0xfd, 0x3b, 0x60, 0x07}}
var iidIDWriteFactory = guid{0xb859ee5a, 0xd838, 0x4b5b, [8]byte{0xa2, 0xe8, 0x1a, 0xdc, 0x7d, 0x93, 0xdb, 0x48}}

type d2dPixelFormat struct {
	Format    uint32
	AlphaMode uint32
}
type d2dRenderTargetProperties struct {
	Type        uint32
	PixelFormat d2dPixelFormat
	DpiX        float32
	DpiY        float32
	Usage       uint32
	MinLevel    uint32
}
type d2dColorF struct{ R, G, B, A float32 }
type d2dRectF struct{ Left, Top, Right, Bottom float32 }
type d2dRoundedRect struct {
	Rect             d2dRectF
	RadiusX, RadiusY float32
}
type d2dBrushProperties struct {
	Opacity   float32
	Transform [6]float32
}
type d2dBitmapProperties struct {
	PixelFormat d2dPixelFormat
	DpiX, DpiY  float32
}
type d2dBitmapBrushProperties struct {
	ExtendModeX       uint32
	ExtendModeY       uint32
	InterpolationMode uint32
}

type d2dTextKey struct {
	Size   int
	Weight int
	Align  uint32
	Para   uint32
	Wrap   uint32
}

type d2dRenderer struct {
	factory         uintptr
	target          uintptr
	dwrite          uintptr
	brushes         map[uint32]uintptr
	textFormats     map[d2dTextKey]uintptr
	settingsBitmap  uintptr
	settingsBrush   uintptr
	bellBitmap      uintptr
	bellBrush       uintptr
	appBitmap       uintptr
	appBrush        uintptr
	scenarioBitmaps [8]uintptr
	scenarioBrushes [8]uintptr
	captionBitmaps  [7]uintptr
	captionBrushes  [7]uintptr
	active          bool
}

var ui2d d2dRenderer

var d2dBaseScale040 float32 = 1

func d2dSetBaseScale040(scale float32) {
	if scale <= 0 {
		scale = 1
	}
	d2dBaseScale040 = scale
	if ui2d.active {
		d2dResetTransform()
	}
}

const (
	d2dFactorySingleThreaded     = 0
	d2dRenderTargetTypeDefault   = 0
	dxgiFormatUnknown            = 0
	dxgiFormatB8G8R8A8UNorm      = 87
	d2dAlphaUnknown              = 0
	d2dAlphaPremultiplied        = 1
	d2dUsageNone                 = 0
	d2dFeatureLevelDefault       = 0
	d2dAntialiasPerPrimitive     = 0
	d2dTextAntialiasClearType    = 1
	d2dExtendClamp               = 0
	d2dBitmapInterpolationLinear = 1
	dwriteFactoryShared          = 0
	dwriteFontStyleNormal        = 0
	dwriteFontStretchNormal      = 5
	dwriteTextAlignLeading       = 0
	dwriteTextAlignTrailing      = 1
	dwriteTextAlignCenter        = 2
	dwriteParagraphNear          = 0
	dwriteParagraphCenter        = 2
	dwriteWordWrapWrap           = 0
	dwriteWordWrapNoWrap         = 1
	d2dDrawTextNone              = 0
	d2dDrawTextClip              = 2
	dwriteMeasuringNatural       = 0
)

func d2dInit() bool {
	if ui2d.target != 0 && ui2d.dwrite != 0 {
		return true
	}
	if ui2d.factory != 0 || ui2d.target != 0 || ui2d.dwrite != 0 {
		d2dReleaseAll()
	}
	ui2d.brushes = make(map[uint32]uintptr)
	ui2d.textFormats = make(map[d2dTextKey]uintptr)

	var factory uintptr
	hr, _, _ := pD2D1CreateFactory.Call(d2dFactorySingleThreaded, uintptr(unsafe.Pointer(&iidID2D1Factory)), 0, uintptr(unsafe.Pointer(&factory)))
	if int32(hr) < 0 || factory == 0 {
		return false
	}
	ui2d.factory = factory

	props := d2dRenderTargetProperties{
		Type:        d2dRenderTargetTypeDefault,
		PixelFormat: d2dPixelFormat{Format: dxgiFormatB8G8R8A8UNorm, AlphaMode: d2dAlphaPremultiplied},
		DpiX:        0, DpiY: 0, Usage: d2dUsageNone, MinLevel: d2dFeatureLevelDefault,
	}
	var target uintptr
	hr = d2dCall(factory, 16, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&target)))
	if int32(hr) < 0 || target == 0 {
		d2dReleaseAll()
		return false
	}
	ui2d.target = target
	d2dCall(target, 32, d2dAntialiasPerPrimitive)
	d2dCall(target, 34, d2dTextAntialiasClearType)

	var dw uintptr
	hr, _, _ = pDWriteCreateFactory.Call(dwriteFactoryShared, uintptr(unsafe.Pointer(&iidIDWriteFactory)), uintptr(unsafe.Pointer(&dw)))
	if int32(hr) < 0 || dw == 0 {
		d2dReleaseAll()
		return false
	}
	ui2d.dwrite = dw
	return true
}

func d2dBegin(hdc uintptr, rc RECT) bool {
	if !d2dInit() {
		return false
	}
	hr := d2dCall(ui2d.target, 57, hdc, uintptr(unsafe.Pointer(&rc)))
	if int32(hr) < 0 {
		return false
	}
	d2dCall(ui2d.target, 48)
	ui2d.active = true
	return true
}

func d2dEnd() {
	if !ui2d.active || ui2d.target == 0 {
		return
	}
	ui2d.active = false
	hr := d2dCall(ui2d.target, 49, 0, 0)
	if int32(hr) < 0 {
		// A DC render target can normally be rebound, but if Direct2D asks for recreation,
		// clear target-owned resources so the next paint starts cleanly.
		d2dReleaseTargetResources()
	}
}

func d2dReleaseTargetResources() {
	for _, b := range ui2d.brushes {
		comRelease(b)
	}
	ui2d.brushes = make(map[uint32]uintptr)
	if ui2d.settingsBrush != 0 {
		comRelease(ui2d.settingsBrush)
		ui2d.settingsBrush = 0
	}
	if ui2d.settingsBitmap != 0 {
		comRelease(ui2d.settingsBitmap)
		ui2d.settingsBitmap = 0
	}
	if ui2d.bellBrush != 0 {
		comRelease(ui2d.bellBrush)
		ui2d.bellBrush = 0
	}
	if ui2d.bellBitmap != 0 {
		comRelease(ui2d.bellBitmap)
		ui2d.bellBitmap = 0
	}
	if ui2d.appBrush != 0 {
		comRelease(ui2d.appBrush)
		ui2d.appBrush = 0
	}
	if ui2d.appBitmap != 0 {
		comRelease(ui2d.appBitmap)
		ui2d.appBitmap = 0
	}
	for i := range ui2d.scenarioBrushes {
		if ui2d.scenarioBrushes[i] != 0 {
			comRelease(ui2d.scenarioBrushes[i])
			ui2d.scenarioBrushes[i] = 0
		}
		if ui2d.scenarioBitmaps[i] != 0 {
			comRelease(ui2d.scenarioBitmaps[i])
			ui2d.scenarioBitmaps[i] = 0
		}
	}
	for i := range ui2d.captionBrushes {
		if ui2d.captionBrushes[i] != 0 {
			comRelease(ui2d.captionBrushes[i])
			ui2d.captionBrushes[i] = 0
		}
		if ui2d.captionBitmaps[i] != 0 {
			comRelease(ui2d.captionBitmaps[i])
			ui2d.captionBitmaps[i] = 0
		}
	}
	if ui2d.target != 0 {
		comRelease(ui2d.target)
		ui2d.target = 0
	}
}

func d2dReleaseAll() {
	ui2d.active = false
	d2dReleaseTargetResources()
	for _, f := range ui2d.textFormats {
		comRelease(f)
	}
	ui2d.textFormats = make(map[d2dTextKey]uintptr)
	if ui2d.dwrite != 0 {
		comRelease(ui2d.dwrite)
		ui2d.dwrite = 0
	}
	if ui2d.factory != 0 {
		comRelease(ui2d.factory)
		ui2d.factory = 0
	}
}

func d2dCall(obj uintptr, index uintptr, args ...uintptr) uintptr {
	if obj == 0 {
		return ^uintptr(0)
	}
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + index*unsafe.Sizeof(uintptr(0))))
	callArgs := make([]uintptr, 0, len(args)+1)
	callArgs = append(callArgs, obj)
	callArgs = append(callArgs, args...)
	r1, _, _ := syscall.SyscallN(fn, callArgs...)
	return r1
}

func comRelease(obj uintptr) {
	if obj != 0 {
		d2dCall(obj, 2)
	}
}

func d2dColor(c uint32) d2dColorF {
	return d2dColorF{
		R: float32(c&0xff) / 255.0,
		G: float32((c>>8)&0xff) / 255.0,
		B: float32((c>>16)&0xff) / 255.0,
		A: 1,
	}
}

func d2dBrush(c uint32) uintptr {
	if b := ui2d.brushes[c]; b != 0 {
		return b
	}
	col := d2dColor(c)
	props := d2dBrushProperties{Opacity: 1, Transform: [6]float32{1, 0, 0, 1, 0, 0}}
	var brush uintptr
	hr := d2dCall(ui2d.target, 8, uintptr(unsafe.Pointer(&col)), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&brush)))
	if int32(hr) < 0 {
		return 0
	}
	ui2d.brushes[c] = brush
	return brush
}

func d2dFillRect(r RECT, c uint32) {
	if !ui2d.active {
		return
	}
	rf := d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}
	b := d2dBrush(c)
	if b != 0 {
		d2dCall(ui2d.target, 17, uintptr(unsafe.Pointer(&rf)), b)
	}
}

func d2dFillRounded(r RECT, c uint32, radius int32) {
	if !ui2d.active {
		return
	}
	rr := d2dRoundedRect{
		Rect:    d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)},
		RadiusX: float32(radius), RadiusY: float32(radius),
	}
	b := d2dBrush(c)
	if b != 0 {
		d2dCall(ui2d.target, 19, uintptr(unsafe.Pointer(&rr)), b)
	}
}

func d2dTextFormat(size, weight int, flags uint32) uintptr {
	align := uint32(dwriteTextAlignLeading)
	if flags&DT_CENTER != 0 {
		align = dwriteTextAlignCenter
	} else if flags&DT_RIGHT != 0 {
		align = dwriteTextAlignTrailing
	}
	para := uint32(dwriteParagraphNear)
	if flags&DT_VCENTER != 0 {
		para = dwriteParagraphCenter
	}
	wrap := uint32(dwriteWordWrapWrap)
	if flags&DT_SINGLELINE != 0 {
		wrap = dwriteWordWrapNoWrap
	}
	key := d2dTextKey{Size: size, Weight: weight, Align: align, Para: para, Wrap: wrap}
	if f := ui2d.textFormats[key]; f != 0 {
		return f
	}
	var format uintptr
	family := wstr("Segoe UI")
	locale := wstr("ru-RU")
	// CreateTextFormat is IDWriteFactory vtable slot 15. fontSize is the 7th ABI
	// argument (after this) and therefore resides in a stack slot on Win64.
	fontSizeBits := uintptr(math.Float32bits(float32(size)))
	hr := d2dCall(ui2d.dwrite, 15,
		uintptr(unsafe.Pointer(family)), 0,
		uintptr(weight), dwriteFontStyleNormal, dwriteFontStretchNormal,
		fontSizeBits, uintptr(unsafe.Pointer(locale)), uintptr(unsafe.Pointer(&format)))
	if int32(hr) < 0 || format == 0 {
		return 0
	}
	d2dCall(format, 3, uintptr(align))
	d2dCall(format, 4, uintptr(para))
	d2dCall(format, 5, uintptr(wrap))
	ui2d.textFormats[key] = format
	return format
}

func d2dDrawText(text string, x, y, w, h, size, weight int, color uint32, flags uint32) {
	if !ui2d.active || text == "" {
		return
	}
	format := d2dTextFormat(size, weight, flags)
	brush := d2dBrush(color)
	if format == 0 || brush == 0 {
		return
	}
	u := syscall.StringToUTF16(text)
	if len(u) == 0 {
		return
	}
	rf := d2dRectF{float32(x), float32(y), float32(x + w), float32(y + h)}
	opts := uintptr(d2dDrawTextNone)
	if flags&DT_END_ELLIPSIS != 0 {
		opts = d2dDrawTextClip
	}
	d2dCall(ui2d.target, 27,
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), format,
		uintptr(unsafe.Pointer(&rf)), brush, opts, dwriteMeasuringNatural)
}

type dwriteTextMetrics struct {
	Left, Top                     float32
	Width, WidthIncludingTrailing float32
	Height                        float32
	LayoutWidth, LayoutHeight     float32
	MaxBidiReorderingDepth        uint32
	LineCount                     uint32
}

// dwriteMeasureTextWidth measures text with the same DirectWrite format used for rendering.
// Keeping measurement and drawing on the same typography engine removes the visible,
// DPI-dependent spacing drift that appeared when GDI measured a DirectWrite string.
func dwriteMeasureTextWidth(text string, size, weight int) int {
	if text == "" {
		return 0
	}
	if ui2d.dwrite == 0 && !d2dInit() {
		return 0
	}
	format := d2dTextFormat(size, weight, DT_LEFT|DT_SINGLELINE)
	if format == 0 || ui2d.dwrite == 0 {
		return 0
	}
	u := syscall.StringToUTF16(text)
	if len(u) <= 1 {
		return 0
	}
	var layout uintptr
	maxW := uintptr(math.Float32bits(4096))
	maxH := uintptr(math.Float32bits(128))
	// IDWriteFactory::CreateTextLayout, vtable slot 18.
	hr := d2dCall(ui2d.dwrite, 18,
		uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), format,
		maxW, maxH, uintptr(unsafe.Pointer(&layout)))
	if int32(hr) < 0 || layout == 0 {
		return 0
	}
	defer comRelease(layout)
	m := dwriteTextMetrics{}
	// IDWriteTextLayout::GetMetrics, vtable slot 60.
	hr = d2dCall(layout, 60, uintptr(unsafe.Pointer(&m)))
	if int32(hr) < 0 {
		return 0
	}
	w := m.WidthIncludingTrailing
	if w <= 0 {
		w = m.Width
	}
	if w <= 0 {
		return 0
	}
	return int(math.Ceil(float64(w)))
}

func d2dClear(c uint32) {
	if !ui2d.active {
		return
	}
	col := d2dColor(c)
	d2dCall(ui2d.target, 47, uintptr(unsafe.Pointer(&col)))
}

func d2dFillEllipse(cx, cy, rx, ry float32, c uint32) {
	if !ui2d.active {
		return
	}
	type ellipse struct {
		Point            struct{ X, Y float32 }
		RadiusX, RadiusY float32
	}
	e := ellipse{}
	e.Point.X, e.Point.Y, e.RadiusX, e.RadiusY = cx, cy, rx, ry
	b := d2dBrush(c)
	if b != 0 {
		d2dCall(ui2d.target, 21, uintptr(unsafe.Pointer(&e)), b)
	}
}

func d2dDrawLine(x1, y1, x2, y2, stroke float32, c uint32) {
	if !ui2d.active {
		return
	}
	b := d2dBrush(c)
	if b == 0 {
		return
	}
	// D2D1_POINT_2F values are passed by value; on Win64 each point occupies one 64-bit slot.
	p1 := uintptr(uint64(math.Float32bits(x1)) | uint64(math.Float32bits(y1))<<32)
	p2 := uintptr(uint64(math.Float32bits(x2)) | uint64(math.Float32bits(y2))<<32)
	// strokeWidth is the fifth ABI argument including this, so it is stack-passed as raw float bits.
	d2dCall(ui2d.target, 15, p1, p2, b, uintptr(math.Float32bits(stroke)), 0)
}

func d2dCreateImageBrush(data []byte, outW, outH int) (uintptr, uintptr) {
	if len(data) == 0 || ui2d.target == 0 || outW <= 0 || outH <= 0 {
		return 0, 0
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, 0
	}
	pix := resampleBGRA(img, outW, outH)
	if len(pix) == 0 {
		return 0, 0
	}
	sizePacked := uintptr(uint64(outW) | uint64(outH)<<32)
	props := d2dBitmapProperties{PixelFormat: d2dPixelFormat{Format: dxgiFormatB8G8R8A8UNorm, AlphaMode: d2dAlphaPremultiplied}, DpiX: 96, DpiY: 96}
	var bitmap uintptr
	hr := d2dCall(ui2d.target, 4, sizePacked, uintptr(unsafe.Pointer(&pix[0])), uintptr(outW*4), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&bitmap)))
	if int32(hr) < 0 || bitmap == 0 {
		return 0, 0
	}
	bp := d2dBitmapBrushProperties{ExtendModeX: d2dExtendClamp, ExtendModeY: d2dExtendClamp, InterpolationMode: d2dBitmapInterpolationLinear}
	brushProps := d2dBrushProperties{Opacity: 1, Transform: [6]float32{1, 0, 0, 1, 0, 0}}
	var brush uintptr
	hr = d2dCall(ui2d.target, 7, bitmap, uintptr(unsafe.Pointer(&bp)), uintptr(unsafe.Pointer(&brushProps)), uintptr(unsafe.Pointer(&brush)))
	if int32(hr) < 0 || brush == 0 {
		comRelease(bitmap)
		return 0, 0
	}
	return bitmap, brush
}

func d2dDrawImageBrush(brush uintptr, r RECT) {
	if !ui2d.active || brush == 0 {
		return
	}
	transform := [6]float32{1, 0, 0, 1, float32(r.Left), float32(r.Top)}
	d2dCall(brush, 5, uintptr(unsafe.Pointer(&transform)))
	rf := d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}
	d2dCall(ui2d.target, 17, uintptr(unsafe.Pointer(&rf)), brush)
}

func d2dPushClip(r RECT) {
	if !ui2d.active || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	rf := d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}
	d2dCall(ui2d.target, 45, uintptr(unsafe.Pointer(&rf)), 0)
}

func d2dPopClip() {
	if !ui2d.active {
		return
	}
	d2dCall(ui2d.target, 46)
}

func d2dDrawImageBrushRotated(brush uintptr, r RECT, degrees float64) {
	if !ui2d.active || brush == 0 {
		return
	}
	rad := degrees * math.Pi / 180
	c, sn := float32(math.Cos(rad)), float32(math.Sin(rad))
	cx := float32(r.Left+r.Right) / 2
	cy := float32(r.Top+r.Bottom) / 2
	sx := float32(r.Right-r.Left) / 2
	sy := float32(r.Bottom-r.Top) / 2
	transform := [6]float32{c, sn, -sn, c, cx - sx*c + sy*sn, cy - sx*sn - sy*c}
	d2dCall(brush, 5, uintptr(unsafe.Pointer(&transform)))
	rf := d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}
	d2dCall(ui2d.target, 17, uintptr(unsafe.Pointer(&rf)), brush)
}

func d2dEnsureSettingsIcon() bool {
	if ui2d.settingsBrush != 0 {
		return true
	}
	bmp, brush := d2dCreateImageBrush(settingsPNGData, 28, 28)
	if bmp == 0 || brush == 0 {
		return false
	}
	ui2d.settingsBitmap, ui2d.settingsBrush = bmp, brush
	return true
}

func d2dEnsureAppIcon() bool {
	if ui2d.appBrush != 0 {
		return true
	}
	bmp, brush := d2dCreateImageBrush(appPNGData, 26, 26)
	if bmp == 0 || brush == 0 {
		return false
	}
	ui2d.appBitmap, ui2d.appBrush = bmp, brush
	return true
}

func d2dEnsureBellIcon() bool {
	if ui2d.bellBrush != 0 {
		return true
	}
	bmp, brush := d2dCreateImageBrush(bellPNGData, 24, 24)
	if bmp == 0 || brush == 0 {
		return false
	}
	ui2d.bellBitmap, ui2d.bellBrush = bmp, brush
	return true
}

const (
	scenarioIconPaste = iota
	scenarioIconPasteAll
	scenarioIconCopy
	scenarioIconDelete
	scenarioIconPause
	scenarioIconPlay
	scenarioIconNotificationClear
	scenarioIconNotificationRead
)

func d2dEnsureScenarioIcon(kind int) bool {
	if kind < 0 || kind >= len(ui2d.scenarioBrushes) {
		return false
	}
	if ui2d.scenarioBrushes[kind] != 0 {
		return true
	}
	data := [][]byte{pastePNGData, pasteAllPNGData, copyPNGData, deletePNGData, pausePNGData, playPNGData, notificationClearPNGData, notificationReadPNGData}[kind]
	bmp, brush := d2dCreateImageBrush(data, 22, 22)
	if bmp == 0 || brush == 0 {
		return false
	}
	ui2d.scenarioBitmaps[kind], ui2d.scenarioBrushes[kind] = bmp, brush
	return true
}

func d2dEnsureCaptionIcon(kind int) bool {
	if kind < 0 || kind >= len(ui2d.captionBrushes) {
		return false
	}
	if ui2d.captionBrushes[kind] != 0 {
		return true
	}
	data := [][]byte{captionClosePNGData, captionFullscreenPNGData, captionMinimizePNGData, captionMiniPNGData, captionExitMiniPNGData, captionPinPNGData, captionRestorePNGData}[kind]
	bmp, brush := d2dCreateImageBrush(data, 22, 22)
	if bmp == 0 || brush == 0 {
		return false
	}
	ui2d.captionBitmaps[kind], ui2d.captionBrushes[kind] = bmp, brush
	return true
}

func d2dDrawCaptionIcon(kind int, r RECT) bool {
	if !ui2d.active || !d2dEnsureCaptionIcon(kind) {
		return false
	}
	d2dDrawImageBrushSizedRotated(ui2d.captionBrushes[kind], r, 0, 22, 22)
	return true
}

func d2dDrawScenarioIcon(kind int, r RECT) {
	if !ui2d.active || !d2dEnsureScenarioIcon(kind) {
		return
	}
	d2dDrawImageBrushSizedRotated(ui2d.scenarioBrushes[kind], r, 0, 22, 22)
}

func resampleBGRA(img image.Image, outW, outH int) []byte {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || outW <= 0 || outH <= 0 {
		return nil
	}
	pix := make([]byte, outW*outH*4)
	for y := 0; y < outH; y++ {
		fy := (float64(y)+0.5)*float64(sh)/float64(outH) - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		if y0 < 0 {
			y0, ty = 0, 0
		}
		y1 := y0 + 1
		if y1 >= sh {
			y1 = sh - 1
		}
		for x := 0; x < outW; x++ {
			fx := (float64(x)+0.5)*float64(sw)/float64(outW) - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			if x0 < 0 {
				x0, tx = 0, 0
			}
			x1 := x0 + 1
			if x1 >= sw {
				x1 = sw - 1
			}
			r00, g00, b00, a00 := img.At(b.Min.X+x0, b.Min.Y+y0).RGBA()
			r10, g10, b10, a10 := img.At(b.Min.X+x1, b.Min.Y+y0).RGBA()
			r01, g01, b01, a01 := img.At(b.Min.X+x0, b.Min.Y+y1).RGBA()
			r11, g11, b11, a11 := img.At(b.Min.X+x1, b.Min.Y+y1).RGBA()
			lerp := func(v00, v10, v01, v11 uint32) uint8 {
				v0 := float64(v00>>8)*(1-tx) + float64(v10>>8)*tx
				v1 := float64(v01>>8)*(1-tx) + float64(v11>>8)*tx
				v := v0*(1-ty) + v1*ty
				if v < 0 {
					v = 0
				}
				if v > 255 {
					v = 255
				}
				return uint8(v + 0.5)
			}
			rr := lerp(r00, r10, r01, r11)
			gg := lerp(g00, g10, g01, g11)
			bb := lerp(b00, b10, b01, b11)
			aa := lerp(a00, a10, a01, a11)
			rr = uint8(uint16(rr) * uint16(aa) / 255)
			gg = uint8(uint16(gg) * uint16(aa) / 255)
			bb = uint8(uint16(bb) * uint16(aa) / 255)
			i := (y*outW + x) * 4
			pix[i+0], pix[i+1], pix[i+2], pix[i+3] = bb, gg, rr, aa
		}
	}
	return pix
}

func d2dDrawSettingsIcon(r RECT) {
	d2dDrawSettingsIconRotated(r, 0)
}

func d2dDrawSettingsIconRotated(r RECT, degrees float64) {
	if !ui2d.active || !d2dEnsureSettingsIcon() {
		return
	}
	d2dDrawImageBrushRotated(ui2d.settingsBrush, r, degrees)
}

func d2dDrawAppIcon(r RECT) {
	if !ui2d.active || !d2dEnsureAppIcon() {
		return
	}
	d2dDrawImageBrush(ui2d.appBrush, r)
}

func d2dDrawBellIcon(r RECT) {
	d2dDrawBellIconRotated(r, 0)
}

func d2dDrawImageBrushSizedRotated(brush uintptr, r RECT, degrees float64, sourceW, sourceH float32) {
	if !ui2d.active || brush == 0 || r.Right <= r.Left || r.Bottom <= r.Top || sourceW <= 0 || sourceH <= 0 {
		return
	}
	// Bitmap brushes keep their source pixel coordinate system.  A plain translation
	// is only correct when the destination rect has exactly the same size as the
	// bitmap.  The notification bell deliberately animates from 28 to 30 px while
	// its cached bitmap is 24 px; without the scale below Direct2D clamps the extra
	// destination pixels on the right/bottom and the visible bell is anchored to
	// the top-left of its nominal rect.  Map the source centre to the destination
	// centre so the icon remains optically centred at every hover scale.
	rad := degrees * math.Pi / 180
	c, sn := float32(math.Cos(rad)), float32(math.Sin(rad))
	dstW := float32(r.Right - r.Left)
	dstH := float32(r.Bottom - r.Top)
	sx := dstW / sourceW
	sy := dstH / sourceH
	m11 := c * sx
	m12 := sn * sx
	m21 := -sn * sy
	m22 := c * sy
	srcCX := sourceW / 2
	srcCY := sourceH / 2
	dstCX := float32(r.Left+r.Right) / 2
	dstCY := float32(r.Top+r.Bottom) / 2
	dx := dstCX - m11*srcCX - m21*srcCY
	dy := dstCY - m12*srcCX - m22*srcCY
	transform := [6]float32{m11, m12, m21, m22, dx, dy}
	d2dCall(brush, 5, uintptr(unsafe.Pointer(&transform)))
	rf := d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}
	d2dCall(ui2d.target, 17, uintptr(unsafe.Pointer(&rf)), brush)
}

func d2dDrawBellIconRotated(r RECT, degrees float64) {
	if !ui2d.active || !d2dEnsureBellIcon() {
		return
	}
	d2dDrawImageBrushSizedRotated(ui2d.bellBrush, r, degrees, 24, 24)
}

func d2dDrawRectOutline(r RECT, stroke float32, c uint32) {
	if !ui2d.active || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	b := d2dBrush(c)
	if b == 0 {
		return
	}
	rf := d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}
	d2dCall(ui2d.target, 16, uintptr(unsafe.Pointer(&rf)), b, uintptr(math.Float32bits(stroke)), 0)
}

func d2dDrawRoundedOutline(r RECT, radius float32, stroke float32, c uint32) {
	if !ui2d.active || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	b := d2dBrush(c)
	if b == 0 {
		return
	}
	rr := d2dRoundedRect{Rect: d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}, RadiusX: radius, RadiusY: radius}
	d2dCall(ui2d.target, 18, uintptr(unsafe.Pointer(&rr)), b, uintptr(math.Float32bits(stroke)), 0)
}

func d2dSetTranslation(x, y float32) {
	if !ui2d.active || ui2d.target == 0 {
		return
	}
	sc := d2dBaseScale040
	m := [6]float32{sc, 0, 0, sc, x * sc, y * sc}
	d2dCall(ui2d.target, 30, uintptr(unsafe.Pointer(&m)))
}

func d2dResetTransform() {
	if !ui2d.active || ui2d.target == 0 {
		return
	}
	sc := d2dBaseScale040
	m := [6]float32{sc, 0, 0, sc, 0, 0}
	d2dCall(ui2d.target, 30, uintptr(unsafe.Pointer(&m)))
}

// Opacity helpers used by the Windows recreation of Apple's Liquid Glass ideas.
// They reuse cached Direct2D solid brushes and temporarily change brush opacity, avoiding
// COM allocation churn when many glass surfaces are visible in the same frame.
func d2dSetBrushOpacity(brush uintptr, opacity float32) {
	if brush == 0 {
		return
	}
	if opacity < 0 {
		opacity = 0
	}
	if opacity > 1 {
		opacity = 1
	}
	d2dCall(brush, 4, uintptr(math.Float32bits(opacity)))
}

func d2dFillRoundedOpacity(r RECT, c uint32, radius int32, opacity float32) {
	if !ui2d.active || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	b := d2dBrush(c)
	if b == 0 {
		return
	}
	d2dSetBrushOpacity(b, opacity)
	rr := d2dRoundedRect{Rect: d2dRectF{float32(r.Left), float32(r.Top), float32(r.Right), float32(r.Bottom)}, RadiusX: float32(radius), RadiusY: float32(radius)}
	d2dCall(ui2d.target, 19, uintptr(unsafe.Pointer(&rr)), b)
	d2dSetBrushOpacity(b, 1)
}

func d2dFillEllipseOpacity(cx, cy, rx, ry float32, c uint32, opacity float32) {
	if !ui2d.active {
		return
	}
	b := d2dBrush(c)
	if b == 0 {
		return
	}
	d2dSetBrushOpacity(b, opacity)
	type ellipse struct {
		Point            struct{ X, Y float32 }
		RadiusX, RadiusY float32
	}
	var e ellipse
	e.Point.X, e.Point.Y, e.RadiusX, e.RadiusY = cx, cy, rx, ry
	d2dCall(ui2d.target, 21, uintptr(unsafe.Pointer(&e)), b)
	d2dSetBrushOpacity(b, 1)
}

// d2dDrawFrostedPanel is the companion material for Liquid Glass mode.
// Large content areas deliberately stay stable and matte: a themed translucent-looking base,
// a restrained inner light and a soft rim. This avoids turning every container into "glass"
// and prevents pointer-driven highlights from fighting with native text fields.
func d2dDrawFrostedPanel(r RECT, radius int32) {
	if !ui2d.active || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	base := blendColor(theme.bg, theme.panel2, .52)
	d2dFillRounded(r, base, radius)
	w := float32(r.Right - r.Left)
	h := float32(r.Bottom - r.Top)
	d2dPushClip(r)
	// Static ambient tint: enough depth to separate an area from the background, no mouse tracking.
	d2dFillEllipseOpacity(float32(r.Left)+w*.24, float32(r.Top)+h*.08, w*.55, h*.34, blendColor(theme.accent, theme.accent2, .48), .035)
	d2dFillEllipseOpacity(float32(r.Right)-w*.10, float32(r.Bottom), w*.48, h*.42, theme.bg, .055)
	d2dPopClip()
	d2dDrawRoundedOutline(r, float32(radius), .85, blendColor(theme.border, theme.text, .20))
	if r.Right-r.Left > 6 && r.Bottom-r.Top > 6 {
		ri := RECT{r.Left + 2, r.Top + 2, r.Right - 2, r.Bottom - 2}
		d2dDrawRoundedOutline(ri, float32(max(1, int(radius)-2)), .45, blendColor(theme.border, theme.accent2, .16))
	}
}

// d2dDrawLiquidGlass is reserved for controls and interaction surfaces. It uses a clearer
// material than the old 0.2.6 version: a thin tinted substrate, edge thickness, a restrained
// caustic highlight and (only for explicitly hoverable controls) a tiny pointer response.
// Native EDIT containers use the same optical material but remain static, eliminating the
// old jump when the mouse crosses from the parent HWND into a child edit HWND.
func d2dDrawLiquidGlass(r RECT, radius int32, reactive bool) {
	if !ui2d.active || r.Right <= r.Left || r.Bottom <= r.Top {
		return
	}
	w := float32(r.Right - r.Left)
	h := float32(r.Bottom - r.Top)
	cx := float32(r.Left+r.Right) / 2
	cy := float32(r.Top+r.Bottom) / 2

	// Clearer substrate than before. It intentionally keeps enough contrast for text.
	base := blendColor(theme.bg, theme.panel2, .70)
	d2dFillRoundedOpacity(r, base, radius, .64)

	dx, dy := float32(0), float32(0)
	if reactive && pointIn(r, app.mouseX, app.mouseY) {
		dx = (float32(app.mouseX) - cx) / maxFloat32(w, 1)
		dy = (float32(app.mouseY) - cy) / maxFloat32(h, 1)
		if dx < -.5 {
			dx = -.5
		}
		if dx > .5 {
			dx = .5
		}
		if dy < -.5 {
			dy = -.5
		}
		if dy > .5 {
			dy = .5
		}
	}

	d2dPushClip(r)
	// A very subtle chromatic caustic; movement is intentionally tiny so it reads as material,
	// not as an object sliding under the label.
	d2dFillEllipseOpacity(cx+dx*w*.035, cy+dy*h*.025, w*.64, h*.80, blendColor(theme.accent, theme.accent2, .52), .055)
	// Upper specular sheet and opposite-side density provide the sense of thickness.
	d2dFillEllipseOpacity(float32(r.Left)+w*(.24+dx*.025), float32(r.Top)+h*(.08+dy*.015), w*.40, h*.24, theme.text, .085)
	d2dFillEllipseOpacity(float32(r.Right)-w*.06, float32(r.Bottom)+h*.06, w*.34, h*.34, theme.bg, .075)
	d2dPopClip()

	outer := blendColor(theme.border, theme.text, .34)
	inner := blendColor(theme.border, theme.accent2, .24)
	d2dDrawRoundedOutline(r, float32(radius), 1.0, outer)
	if r.Right-r.Left > 5 && r.Bottom-r.Top > 5 {
		ri := RECT{r.Left + 2, r.Top + 2, r.Right - 2, r.Bottom - 2}
		d2dDrawRoundedOutline(ri, float32(max(1, int(radius)-2)), .55, inner)
	}
	// Crisp top caustic, shorter than the full edge so corners remain clean.
	if r.Right-r.Left > radius*2+8 {
		d2dDrawLine(float32(r.Left+radius+4), float32(r.Top)+1.15, float32(r.Right-radius-4), float32(r.Top)+1.15, .7, blendColor(theme.border, theme.text, .52))
	}
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
