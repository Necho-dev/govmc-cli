package esxi

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestConsoleKeyboardAndRelativePointerPackets(t *testing.T) {
	actions, err := compileActions([]ConsoleAction{
		{Type: "key", Key: "Down", Modifiers: []string{"shift"}},
		{Type: "relativeMove", DX: -20, DY: 30},
		{Type: "relativeClick", Button: "right"},
		{Type: "text", Text: "A!"},
	}, 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	if got := actions[0][1].packet; !bytes.Equal(got, []byte{127, 0, 0, 8, 1, 0x50, 1, 0}) {
		t.Fatalf("Down packet = %v", got)
	}
	move := actions[1][0].packet
	if move[4] != 0 || int32(binary.BigEndian.Uint32(move[5:])) != -20 || binary.BigEndian.Uint32(move[9:]) != 30 {
		t.Fatalf("relative motion packet = %v", move)
	}
	if actions[2][0].packet[16] != 2 || actions[2][1].packet[16] != 0 {
		t.Fatal("relative right button must press then release")
	}
	for _, i := range []int{0, 4} {
		if actions[3][i].packet[5] != 0x2a || actions[3][i+3].packet[6] != 0 {
			t.Fatal("shifted character did not balance the shift modifier")
		}
	}
}

func TestConsoleRejectsInvalidBatchBeforeSending(t *testing.T) {
	x, y := 1920, 0
	for _, action := range []ConsoleAction{
		{Type: "click", X: &x, Y: &y},
		{Type: "click", Y: &y},
		{Type: "key", Key: "a", Modifiers: []string{"ctrl", "ctrl"}},
		{Type: "text", Text: "中文"},
		{Type: "relativeMove", DX: -1001},
	} {
		if actions, err := compileActions([]ConsoleAction{{Type: "key", Key: "escape"}, action}, 1920, 1080); err == nil || actions != nil {
			t.Fatalf("accepted invalid batch: %+v", action)
		}
	}
}
