package esxi

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type ConsoleAction struct {
	Type      string   `json:"type"`
	X         *int     `json:"x,omitempty"`
	Y         *int     `json:"y,omitempty"`
	ToX       *int     `json:"toX,omitempty"`
	ToY       *int     `json:"toY,omitempty"`
	Button    string   `json:"button,omitempty"`
	DX        int      `json:"dx,omitempty"`
	DY        int      `json:"dy,omitempty"`
	Key       string   `json:"key,omitempty"`
	Text      string   `json:"text,omitempty"`
	Modifiers []string `json:"modifiers,omitempty"`
	MS        int      `json:"ms,omitempty"`
}

type ConsoleResult struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Completed int    `json:"completed"`
	VMID      string `json:"vmId"`
}

type consoleEvent struct {
	packet []byte
	delay  time.Duration
}

var scanCodes = map[string]uint16{
	"escape": 1, "backspace": 14, "tab": 15, "enter": 28, "space": 57,
	"ctrl": 0x1d, "shift": 0x2a, "alt": 0x38, "meta": 0x15b,
	"home": 0x147, "up": 0x148, "pageup": 0x149, "left": 0x14b,
	"right": 0x14d, "end": 0x14f, "down": 0x150, "pagedown": 0x151,
	"insert": 0x152, "delete": 0x153,
}

var usKeyboardRows = []struct {
	plain, shifted string
	first          uint16
}{
	{"1234567890-=", "!@#$%^&*()_+", 2},
	{"qwertyuiop[]", "QWERTYUIOP{}", 16},
	{"asdfghjkl;'", "ASDFGHJKL:\"", 30},
	{"`", "~", 41},
	{"\\", "|", 43},
	{"zxcvbnm,./", "ZXCVBNM<>?", 44},
}

func scanChar(r rune) (code uint16, shift bool) {
	if r == ' ' {
		return 57, false
	}
	for _, row := range usKeyboardRows {
		if i := strings.IndexRune(row.plain, r); i >= 0 {
			return row.first + uint16(i), false
		}
		if i := strings.IndexRune(row.shifted, r); i >= 0 {
			return row.first + uint16(i), true
		}
	}
	return 0, false
}

func scanKey(key string) uint16 {
	key = strings.ToLower(key)
	if code := scanCodes[key]; code != 0 {
		return code
	}
	if len(key) == 1 {
		code, shifted := scanChar(rune(key[0]))
		if !shifted {
			return code
		}
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(key, "f")); strings.HasPrefix(key, "f") && err == nil {
		if n >= 1 && n <= 10 {
			return 58 + uint16(n)
		}
		if n == 11 || n == 12 {
			return 76 + uint16(n)
		}
	}
	return 0
}

func keyPacket(code uint16, down bool) []byte {
	p := []byte{127, 0, 0, 8, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(p[4:], code)
	if down {
		p[6] = 1
	}
	return p
}

func pointerPacket(x, y int, mask byte) []byte {
	p := []byte{5, mask, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(p[2:], uint16(x))
	binary.BigEndian.PutUint16(p[4:], uint16(y))
	return p
}

func pointer2Packet(absolute bool, x, y int, mask byte, dx, dy int) []byte {
	p := []byte{127, 2, 0, 19, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, mask, byte(dy), byte(dx)}
	if !absolute {
		p[4] = 0
	}
	binary.BigEndian.PutUint32(p[5:], uint32(x))
	binary.BigEndian.PutUint32(p[9:], uint32(y))
	return p
}

func compileAction(a ConsoleAction, width, height int) ([]consoleEvent, error) {
	var events []consoleEvent
	add := func(p []byte) { events = append(events, consoleEvent{packet: p}) }
	wait := func(ms int) { events = append(events, consoleEvent{delay: time.Duration(ms) * time.Millisecond}) }
	chord := func(key uint16, mods []uint16) {
		for _, mod := range mods {
			add(keyPacket(mod, true))
		}
		add(keyPacket(key, true))
		add(keyPacket(key, false))
		for i := len(mods) - 1; i >= 0; i-- {
			add(keyPacket(mods[i], false))
		}
	}
	switch a.Type {
	case "wait":
		if a.MS < 1 || a.MS > 2000 {
			return nil, fmt.Errorf("wait ms must be 1–2000")
		}
		wait(a.MS)
	case "key":
		key := scanKey(a.Key)
		if key == 0 {
			return nil, fmt.Errorf("unsupported key %q", a.Key)
		}
		var mods []uint16
		seen := map[uint16]bool{key: true}
		for _, mod := range a.Modifiers {
			code := scanKey(mod)
			if code != 0x1d && code != 0x2a && code != 0x38 && code != 0x15b || seen[code] {
				return nil, fmt.Errorf("invalid or duplicate modifier %q", mod)
			}
			seen[code] = true
			mods = append(mods, code)
		}
		chord(key, mods)
	case "text":
		if len(a.Text) < 1 || len(a.Text) > 256 {
			return nil, fmt.Errorf("text must be 1–256 printable ASCII characters")
		}
		for _, r := range a.Text {
			code, shift := scanChar(r)
			if code == 0 {
				return nil, fmt.Errorf("text supports printable ASCII on a US keyboard layout")
			}
			var mods []uint16
			if shift {
				mods = []uint16{0x2a}
			}
			chord(code, mods)
		}
	case "relativeMove", "relativeClick", "relativeDrag", "relativeScroll":
		mask, ok := map[string]byte{"": 1, "left": 1, "middle": 4, "right": 2}[a.Button]
		if !ok {
			return nil, fmt.Errorf("button must be left, middle or right")
		}
		if a.DX < -1000 || a.DX > 1000 || a.DY < -1000 || a.DY > 1000 {
			return nil, fmt.Errorf("relative dx/dy must be -1000–1000")
		}
		switch a.Type {
		case "relativeMove":
			add(pointer2Packet(false, a.DX, a.DY, 0, 0, 0))
		case "relativeClick":
			add(pointer2Packet(false, 0, 0, mask, 0, 0))
			add(pointer2Packet(false, 0, 0, 0, 0, 0))
		case "relativeDrag":
			add(pointer2Packet(false, 0, 0, mask, 0, 0))
			for i := 1; i <= 8; i++ {
				wait(20)
				dx := a.DX*i/8 - a.DX*(i-1)/8
				dy := a.DY*i/8 - a.DY*(i-1)/8
				add(pointer2Packet(false, dx, dy, mask, 0, 0))
			}
			add(pointer2Packet(false, 0, 0, 0, 0, 0))
		case "relativeScroll":
			if a.DX < -10 || a.DX > 10 || a.DY < -10 || a.DY > 10 || (a.DX == 0 && a.DY == 0) {
				return nil, fmt.Errorf("scroll dx/dy must be -10–10 with a nonzero axis")
			}
			add(pointer2Packet(false, 0, 0, 0, a.DX, a.DY))
		}
	case "move", "click", "doubleClick", "drag", "scroll":
		valid := func(x, y *int) bool { return x != nil && y != nil && *x >= 0 && *x < width && *y >= 0 && *y < height }
		if !valid(a.X, a.Y) {
			return nil, fmt.Errorf("x/y must be inside the %dx%d console", width, height)
		}
		x, y := *a.X, *a.Y
		mask := map[string]byte{"": 1, "left": 1, "middle": 2, "right": 4}[a.Button]
		if mask == 0 {
			return nil, fmt.Errorf("button must be left, middle or right")
		}
		switch a.Type {
		case "move":
			add(pointerPacket(x, y, 0))
		case "click", "doubleClick":
			add(pointerPacket(x, y, mask))
			add(pointerPacket(x, y, 0))
			if a.Type == "doubleClick" {
				wait(75)
				add(pointerPacket(x, y, mask))
				add(pointerPacket(x, y, 0))
			}
		case "drag":
			if !valid(a.ToX, a.ToY) {
				return nil, fmt.Errorf("toX/toY must be inside the console")
			}
			add(pointerPacket(x, y, mask))
			for i := 1; i <= 8; i++ {
				wait(20)
				add(pointerPacket(x+(*a.ToX-x)*i/8, y+(*a.ToY-y)*i/8, mask))
			}
			add(pointerPacket(*a.ToX, *a.ToY, 0))
		case "scroll":
			if a.DX < -10 || a.DX > 10 || a.DY < -10 || a.DY > 10 || (a.DX == 0 && a.DY == 0) {
				return nil, fmt.Errorf("scroll dx/dy must be -10–10 with a nonzero axis")
			}
			add(pointer2Packet(true, x, y, 0, a.DX, a.DY))
		}
	default:
		return nil, fmt.Errorf("unknown console action %q", a.Type)
	}
	return events, nil
}

func compileActions(actions []ConsoleAction, width, height int) ([][]consoleEvent, error) {
	if len(actions) < 1 || len(actions) > 32 {
		return nil, fmt.Errorf("provide 1–32 console actions")
	}
	compiled := make([][]consoleEvent, len(actions))
	totalDelay := time.Duration(0)
	totalEvents := 0
	for i, action := range actions {
		events, err := compileAction(action, width, height)
		if err != nil {
			return nil, fmt.Errorf("action %d: %w", i+1, err)
		}
		for _, event := range events {
			totalDelay += event.delay
		}
		totalEvents += len(events)
		compiled[i] = events
	}
	if totalDelay > 5*time.Second || totalEvents > 2048 {
		return nil, fmt.Errorf("action batch exceeds the 5-second wait or 2048-event limit")
	}
	return compiled, nil
}

// ValidateConsoleActions rejects malformed batches before connecting to ESXi.
// Actual screen bounds are checked again after the console handshake.
func ValidateConsoleActions(actions []ConsoleAction) error {
	_, err := compileActions(actions, 65535, 65535)
	return err
}

func (c *Client) ConsoleInput(ctx context.Context, id string, actions []ConsoleAction) (ConsoleResult, error) {
	var result ConsoleResult
	if strings.TrimSpace(id) == "" {
		return result, fmt.Errorf("VM id is required")
	}
	if _, err := compileActions(actions, 65535, 65535); err != nil {
		return result, err
	}
	if !c.consoleMu.TryLock() {
		return result, conflict("another console input batch is in progress")
	}
	defer c.consoleMu.Unlock()
	ticket, vm, err := c.AcquireWebMKS(ctx, id)
	if err != nil {
		return result, err
	}
	result.VMID = vm.ID
	dialer := websocket.Dialer{
		TLSClientConfig:  c.Gov.Client.Client.DefaultTransport().TLSClientConfig.Clone(),
		HandshakeTimeout: 8 * time.Second,
		Subprotocols:     []string{"binary"},
	}
	w, err := dialConsole(ctx, dialer, ConsoleURL(ticket, c.Reach))
	if err != nil {
		return result, err
	}
	defer w.conn.Close()
	result.Width, result.Height = w.width, w.height
	compiled, err := compileActions(actions, w.width, w.height)
	if err != nil {
		return result, err
	}
	if err := w.runActions(ctx, compiled, &result); err != nil {
		return result, errf(http.StatusBadGateway, "console_input_failed", "console input interrupted after %d complete actions; the current action may have been partially sent: %v", result.Completed, err)
	}
	return result, nil
}
