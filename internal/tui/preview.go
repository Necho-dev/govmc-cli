package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type tuiScreenshot struct {
	png   []byte
	image image.Image
	time  time.Time
}

func decodeTUIScreenshot(data []byte) (*tuiScreenshot, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32*1024*1024 {
		return nil, fmt.Errorf("screenshot dimensions are too large")
	}
	frame, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &tuiScreenshot{png: data, image: frame, time: time.Now()}, nil
}

type tuiPreview struct {
	*tview.Box
	fallback *tview.Image
	shot     *tuiScreenshot
	sent     *tuiScreenshot
	status   string
	lang     language
	native   bool
	drawn    bool
	locked   [4]int
}

func newTUIPreview(lang language) *tuiPreview {
	// Multiplexers require their own image transport. Use the cell renderer there.
	native := (os.Getenv("TERM_PROGRAM") == "ghostty" || os.Getenv("TERM") == "xterm-kitty") && os.Getenv("TMUX") == "" && os.Getenv("STY") == ""
	p := &tuiPreview{Box: tview.NewBox().SetBackgroundColor(tuiSurface), fallback: tview.NewImage(), lang: lang, native: native}
	p.fallback.SetBackgroundColor(tuiSurface)
	p.SetBorder(true).SetBorderColor(tcell.NewHexColor(0x38546a)).SetTitleColor(tuiAccent).
		SetTitle(" "+lang.Text("Current screen")+" ").SetBorderPadding(1, 1, 2, 2)
	return p
}

func (p *tuiPreview) setFrame(shot *tuiScreenshot, status string) {
	if p.shot != shot {
		p.shot = shot
		if shot == nil {
			p.fallback.SetImage(nil)
		} else {
			p.fallback.SetImage(shot.image)
		}
	}
	p.status = status
}

func (p *tuiPreview) Draw(screen tcell.Screen) {
	p.Box.DrawForSubclass(screen, p)
	x, y, w, h := p.GetInnerRect()
	if w <= 0 || h < 3 {
		return
	}
	p.drawn = true
	tview.Print(screen, tview.Escape("s  "+p.lang.Text("Refresh screenshot")), x, y, w, tview.AlignRight, tuiMuted)
	if p.shot == nil {
		message := p.status
		if message == "" {
			message = p.lang.Text("No screenshot")
		}
		known := false
		for _, label := range []string{"No screenshot", "Loading screenshot…", "Refreshing screenshot…", "Power on the VM to view its screenshot", "Select a VM to view its screenshot"} {
			if message == p.lang.Text(label) {
				known = true
				break
			}
		}
		if !known {
			message = p.lang.Text("Screenshot unavailable")
		}
		lines := tview.WordWrap(tview.Escape(message), max(1, w-2))
		if !known {
			lines = append(lines, "", tview.Escape(p.lang.Text("Press s to retry")))
		}
		first := y + max(2, (h-len(lines))/2)
		for i, line := range lines {
			if first+i >= y+h-1 {
				break
			}
			tview.Print(screen, line, x+1, first+i, w-2, tview.AlignCenter, tuiMuted)
		}
		return
	}
	tty, ok := screen.Tty()
	native := p.native && ok && tty != nil
	if !native {
		p.fallback.SetRect(x, y+1, w, h-2)
		p.fallback.Draw(screen)
	}
	status := p.status
	if status == "" {
		bounds := p.shot.image.Bounds()
		status = fmt.Sprintf("%d × %d  ·  %s %s", bounds.Dx(), bounds.Dy(), p.lang.Text("Sampled"), p.shot.time.Local().Format("15:04:05"))
		if !native {
			status += " · " + p.lang.Text("Character preview")
		}
	}
	tview.Print(screen, tview.Escape(status), x, y+h-1, w, tview.AlignLeft, tuiMuted)
}

const tuiPreviewImageID = 6778742

func (p *tuiPreview) clear(screen tcell.Screen) {
	if screen == nil {
		return
	}
	if p.locked[2] > 0 {
		screen.LockRegion(p.locked[0], p.locked[1], p.locked[2], p.locked[3], false)
		p.locked = [4]int{}
	}
	if p.sent != nil {
		if tty, ok := screen.Tty(); ok && tty != nil {
			_, _ = fmt.Fprintf(tty, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", tuiPreviewImageID)
		}
		p.sent = nil
	}
}

// Flush ordinary cells before emitting graphics, including a terminal resize's
// clear-screen command. The later placement must survive that clear.
func (p *tuiPreview) beforeShow(screen tcell.Screen, mainVisible bool) {
	if !mainVisible || !p.drawn || p.shot == nil {
		p.clear(screen)
		return
	}
	if p.locked[2] > 0 {
		screen.LockRegion(p.locked[0], p.locked[1], p.locked[2], p.locked[3], false)
		p.locked = [4]int{}
	}
}

// Called on the UI goroutine after the cell buffer has been flushed.
// Lock only the image rectangle, so subsequent cell flushes cannot paint over it.
func (p *tuiPreview) afterDraw(screen tcell.Screen, mainVisible bool) {
	tty, ok := screen.Tty()
	if !p.native || !ok || tty == nil || !mainVisible || !p.drawn || p.shot == nil {
		p.clear(screen)
		return
	}
	x, y, w, h := p.GetInnerRect()
	y++
	h -= 2
	if w <= 0 || h <= 0 {
		p.clear(screen)
		return
	}
	cw, ch := 10, 20
	if size, err := tty.WindowSize(); err == nil {
		if cellW, cellH := size.CellDimensions(); cellW > 0 && cellH > 0 {
			cw, ch = cellW, cellH
		}
	}
	bounds := p.shot.image.Bounds()
	scale := math.Min(float64(w*cw)/float64(bounds.Dx()), float64(h*ch)/float64(bounds.Dy()))
	cols := min(w, max(1, int(math.Ceil(float64(bounds.Dx())*scale/float64(cw)))))
	rows := min(h, max(1, int(math.Ceil(float64(bounds.Dy())*scale/float64(ch)))))
	x += (w - cols) / 2
	y += (h - rows) / 2
	rect := [4]int{x, y, cols, rows}
	if p.locked != rect {
		if p.locked[2] > 0 {
			screen.LockRegion(p.locked[0], p.locked[1], p.locked[2], p.locked[3], false)
		}
		screen.LockRegion(x, y, cols, rows, true)
		p.locked = rect
	}
	var output strings.Builder
	fmt.Fprintf(&output, "\x1b7\x1b[%d;%dH", y+1, x+1)
	if p.sent != p.shot {
		if p.sent != nil {
			fmt.Fprintf(&output, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", tuiPreviewImageID)
		}
		writeKittyPNG(&output, p.shot.png, cols, rows)
	} else {
		fmt.Fprintf(&output, "\x1b_Ga=p,i=%d,p=1,q=2,c=%d,r=%d,C=1\x1b\\", tuiPreviewImageID, cols, rows)
	}
	output.WriteString("\x1b8")
	if _, err := io.WriteString(tty, output.String()); err == nil {
		p.sent = p.shot
	} else {
		p.clear(screen)
		p.native = false
	}
}

func writeKittyPNG(out io.Writer, data []byte, columns, rows int) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for offset := 0; offset < len(encoded); offset += 4096 {
		end := min(offset+4096, len(encoded))
		more := 0
		if end < len(encoded) {
			more = 1
		}
		if offset == 0 {
			fmt.Fprintf(out, "\x1b_Ga=T,t=d,f=100,i=%d,p=1,q=2,c=%d,r=%d,C=1,m=%d;", tuiPreviewImageID, columns, rows, more)
		} else {
			fmt.Fprintf(out, "\x1b_Gm=%d;", more)
		}
		io.WriteString(out, encoded[offset:end])
		io.WriteString(out, "\x1b\\")
	}
}
