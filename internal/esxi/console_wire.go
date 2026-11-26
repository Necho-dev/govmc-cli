package esxi

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/vmware/govmomi/vim25/types"
)

// ConsoleURL routes the ticket through the configured ESXi endpoint (including NAT).
// The returned URL contains a bearer credential and must not be logged.
func ConsoleURL(ticket *types.VirtualMachineTicket, reach *url.URL) string {
	u := url.URL{Scheme: "wss", Host: reach.Host, Path: "/ticket/" + strings.TrimSpace(ticket.Ticket)}
	if reach.Port() == "" && reach.Scheme == "http" {
		u.Host = net.JoinHostPort(reach.Hostname(), "80")
	}
	if parsed, err := url.Parse(strings.TrimSpace(ticket.Url)); err == nil && strings.HasPrefix(parsed.Path, "/") {
		u.Path, u.RawPath, u.RawQuery = parsed.Path, parsed.RawPath, parsed.RawQuery
	}
	return u.String()
}

type consoleWire struct {
	conn          *websocket.Conn
	reader        io.Reader
	width, height int
}

func (w *consoleWire) Read(p []byte) (int, error) {
	for {
		if w.reader == nil {
			kind, reader, err := w.conn.NextReader()
			if err != nil {
				return 0, err
			}
			if kind != websocket.BinaryMessage {
				return 0, errors.New("expected binary console data")
			}
			w.reader = reader
		}
		n, err := w.reader.Read(p)
		if err == io.EOF {
			w.reader = nil
			if n == 0 {
				continue
			}
			err = nil
		}
		return n, err
	}
}

func (w *consoleWire) send(packet []byte) error {
	return w.conn.WriteMessage(websocket.BinaryMessage, packet)
}

// ESXi can send ServerInit immediately or negotiate RFB 3.8 with security
// type None, since the WebSocket upgrade has already authenticated the ticket.
func (w *consoleWire) handshake() error {
	const version = "RFB 003.008\n"
	var header [24]byte
	if _, err := io.ReadFull(w, header[:12]); err != nil {
		return err
	}
	offset := 12
	if string(header[:4]) == "RFB " {
		if string(header[:12]) != version {
			return errors.New("unsupported console protocol version")
		}
		if err := w.send([]byte(version)); err != nil {
			return err
		}
		var count [1]byte
		if _, err := io.ReadFull(w, count[:]); err != nil {
			return err
		}
		offers := make([]byte, count[0])
		if _, err := io.ReadFull(w, offers); err != nil {
			return err
		}
		supported := false
		for _, offer := range offers {
			supported = supported || offer == 1
		}
		if !supported {
			return errors.New("unsupported console authentication method")
		}
		if err := w.send([]byte{1}); err != nil {
			return err
		}
		var status [4]byte
		if _, err := io.ReadFull(w, status[:]); err != nil {
			return err
		}
		if status != [4]byte{} {
			return errors.New("console authentication failed")
		}
		if err := w.send([]byte{1}); err != nil {
			return err
		}
		offset = 0
	}
	if _, err := io.ReadFull(w, header[offset:]); err != nil {
		return err
	}
	w.width = int(binary.BigEndian.Uint16(header[:2]))
	w.height = int(binary.BigEndian.Uint16(header[2:4]))
	nameLen := binary.BigEndian.Uint32(header[20:24])
	if w.width == 0 || w.height == 0 || nameLen > 65536 {
		return errors.New("invalid console dimensions or desktop name")
	}
	_, err := io.CopyN(io.Discard, w, int64(nameLen))
	return err
}

func dialConsole(ctx context.Context, dialer websocket.Dialer, address string) (*consoleWire, error) {
	conn, resp, err := dialer.DialContext(ctx, address, nil)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("ESXi console upgrade returned HTTP %d", resp.StatusCode)
		}
		// Dial errors can include the full ticket URL.
		return nil, errors.New("could not connect to ESXi console WebSocket")
	}
	w := &consoleWire{conn: conn}
	deadline := time.Now().Add(8 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	_ = conn.SetWriteDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	err = w.handshake()
	stop()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ESXi console handshake: %w", err)
	}
	return w, nil
}

func (w *consoleWire) runActions(ctx context.Context, batches [][]consoleEvent, result *ConsoleResult) error {
	_ = w.conn.SetReadDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { _ = w.conn.Close() })
	defer stop()
	// Drain server messages so the connection can keep processing input.
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_, _ = io.Copy(io.Discard, w)
	}()
	wait := func(delay time.Duration) error {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-closed:
			return errors.New("ESXi console disconnected")
		case <-timer.C:
			return nil
		}
	}
	_ = w.conn.SetWriteDeadline(time.Now().Add(12 * time.Second))
	for _, batch := range batches {
		for _, event := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if event.delay > 0 {
				if err := wait(event.delay); err != nil {
					return err
				}
				continue
			}
			select {
			case <-closed:
				return errors.New("ESXi console disconnected")
			default:
			}
			if err := w.send(event.packet); err != nil {
				return err
			}
		}
		result.Completed++
	}
	// RFB has no input acknowledgement. Leave time for the peer to consume
	// the final packet before closing; a screenshot is the UI verification.
	return wait(150 * time.Millisecond)
}
