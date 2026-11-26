package httpserver

import (
	"strings"
	"testing"
)

func TestStartupNotice(t *testing.T) {
	for _, tc := range []struct {
		address, language, url, label string
		wildcard                      bool
	}{
		{"127.0.0.1:8765", "en", "http://127.0.0.1:8765", "GoVMC is running", false},
		{"127.0.0.1:43210", "zh", "http://127.0.0.1:43210", "GoVMC 已启动", false},
		{"0.0.0.0:8080", "zh", "http://127.0.0.1:8080", "局域网", true},
		{"[::]:8081", "en", "http://[::1]:8081", "LAN IP", true},
	} {
		notice := startupNotice(tc.address, tc.language)
		if !strings.Contains(notice, tc.url) || !strings.Contains(notice, tc.label) || !strings.Contains(notice, "Ctrl+C") {
			t.Fatal(notice)
		}
		if tc.wildcard && !strings.Contains(notice, tc.address) {
			t.Fatal(notice)
		}
	}
}
