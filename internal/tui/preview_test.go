package tui

import (
	"bytes"
	"encoding/base64"
	"github.com/rivo/tview"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/Necho-dev/govmc-cli/internal/esxi"
	"github.com/gdamore/tcell/v2"
)

type previewTestTTY struct{ bytes.Buffer }

func (*previewTestTTY) Start() error             { return nil }
func (*previewTestTTY) Stop() error              { return nil }
func (*previewTestTTY) Drain() error             { return nil }
func (*previewTestTTY) NotifyResize(func())      {}
func (*previewTestTTY) Close() error             { return nil }
func (*previewTestTTY) Read([]byte) (int, error) { return 0, io.EOF }
func (*previewTestTTY) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: 100, Height: 40, PixelWidth: 1000, PixelHeight: 800}, nil
}

type previewTestScreen struct {
	tcell.Screen
	tty *previewTestTTY
}

func (s previewTestScreen) Tty() (tcell.Tty, bool) { return s.tty, true }

func TestKittyPNGChunksPreservePayload(t *testing.T) {
	data := bytes.Repeat([]byte{0, 128, 255, 17}, 3000)
	var output bytes.Buffer
	writeKittyPNG(&output, data, 40, 12)
	var payload strings.Builder
	chunks := strings.Split(strings.TrimSuffix(output.String(), "\x1b\\"), "\x1b\\")
	if len(chunks) < 2 {
		t.Fatal("large images must be chunked")
	}
	for i, chunk := range chunks {
		command, encoded, ok := strings.Cut(strings.TrimPrefix(chunk, "\x1b_G"), ";")
		if !ok || len(encoded) > 4096 {
			t.Fatal("invalid graphics chunk")
		}
		if i == 0 && (!strings.Contains(command, "q=2") || !strings.Contains(command, "c=40,r=12")) {
			t.Fatal("missing placement or quiet mode")
		}
		if i == len(chunks)-1 && !strings.Contains(command, "m=0") {
			t.Fatal("missing final chunk")
		}
		payload.WriteString(encoded)
	}
	decoded, err := base64.StdEncoding.DecodeString(payload.String())
	if err != nil || !bytes.Equal(decoded, data) {
		t.Fatal("image payload was corrupted")
	}
}

func TestTUIPreviewClearsImagesAndRepositions(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "ghostty")
	t.Setenv("TMUX", "")
	t.Setenv("STY", "")
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(100, 40)
	defer screen.Fini()
	tty := &previewTestTTY{}
	native := previewTestScreen{Screen: screen, tty: tty}
	img := image.NewRGBA(image.Rect(0, 0, 100, 60))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	shot, err := decodeTUIScreenshot(pngData.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	p := newTUIPreview("en")
	p.setFrame(shot, "")
	p.SetRect(10, 8, 50, 20)
	p.Draw(native)
	p.afterDraw(native, true)
	if !strings.Contains(tty.String(), "a=T") || p.sent != shot {
		t.Fatal("image was not transmitted")
	}
	tty.Reset()
	p.beforeShow(native, true)
	if p.locked != [4]int{} {
		t.Fatal("old image cells were locked during the cell flush")
	}
	native.Show()
	p.afterDraw(native, true)
	if !strings.Contains(tty.String(), "a=p") || strings.Contains(tty.String(), "a=T") {
		t.Fatal("cell flush lost the cached image")
	}
	tty.Reset()
	p.SetRect(20, 10, 40, 15)
	p.Draw(native)
	p.afterDraw(native, true)
	if !strings.Contains(tty.String(), "a=p") || strings.Contains(tty.String(), "a=T") {
		t.Fatal("resize should reposition the cached image")
	}
	tty.Reset()
	p.afterDraw(native, false)
	if !strings.Contains(tty.String(), "a=d,d=I") || p.sent != nil || p.locked != [4]int{} {
		t.Fatal("modal did not clear the image and release its cells")
	}
	tty.Reset()
	p.Draw(native)
	p.afterDraw(native, true)
	if !strings.Contains(tty.String(), "a=T") {
		t.Fatal("image was not restored after modal closed")
	}
	tty.Reset()
	p.drawn = false
	p.afterDraw(native, true)
	if !strings.Contains(tty.String(), "a=d,d=I") {
		t.Fatal("leaving the overview retained the image")
	}
}

func TestNetworkRatesUsePairedDirections(t *testing.T) {
	rx, tx := .5, 2.5
	text := tuiNetworkRates(esxi.VMInfo{NetRxMbps: &rx, NetTxMbps: &tx})
	if !strings.Contains(text, "↓ 0.50") || !strings.Contains(text, "↑ 2.50") {
		t.Fatal(text)
	}
	if text = tuiNetworkRates(esxi.VMInfo{}); strings.Count(text, "—") != 2 {
		t.Fatal("missing samples must not become zero")
	}
}

func TestTUITableSpacingAndFixedUnits(t *testing.T) {
	if got := tuiTableCell("8.2%", 7, true); got != "    8.2% " {
		t.Fatalf("numeric column not right aligned: %q", got)
	}
	if got := tuiTableCell("虚拟机", 8, false); got != " 虚拟机   " {
		t.Fatalf("CJK width not respected: %q", got)
	}
	small, large := .01, 128.0
	a := tview.TaggedStringWidth(tuiNetworkRates(esxi.VMInfo{NetRxMbps: &small, NetTxMbps: &large}))
	b := tview.TaggedStringWidth(tuiNetworkRates(esxi.VMInfo{NetRxMbps: &large, NetTxMbps: &small}))
	if a != b {
		t.Fatal("paired rates change column width when direction changes")
	}
}
