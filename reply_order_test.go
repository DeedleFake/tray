package tray

import (
	"bufio"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
)

// replyStall holds back the next method reply the item sends, after
// the method has returned but before godbus writes the reply.
type replyStall struct {
	m       sync.Mutex
	armed   bool
	held    chan struct{}
	release chan struct{}
}

func (s *replyStall) arm() {
	s.m.Lock()
	defer s.m.Unlock()
	s.armed = true
	s.held = make(chan struct{})
	s.release = make(chan struct{})
}

func (s *replyStall) intercept(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodReply {
		return
	}
	s.m.Lock()
	armed := s.armed
	s.armed = false
	held, release := s.held, s.release
	s.m.Unlock()
	if !armed {
		return
	}
	close(held)
	<-release
}

// hostView records every message the host receives, in arrival order.
type hostView struct {
	m    sync.Mutex
	msgs []*dbus.Message
}

func (h *hostView) record(msg *dbus.Message) {
	h.m.Lock()
	defer h.m.Unlock()
	h.msgs = append(h.msgs, msg)
}

// layoutUpdates returns how many LayoutUpdated signals the host has
// received.
func (h *hostView) layoutUpdates() int {
	h.m.Lock()
	defer h.m.Unlock()

	var n int
	for _, msg := range h.msgs {
		member, _ := msg.Headers[dbus.FieldMember].Value().(string)
		if msg.Type == dbus.TypeSignal && member == "LayoutUpdated" {
			n++
		}
	}
	return n
}

// labels applies the recorded signals and replies in arrival order,
// the way the GNOME AppIndicator extension does, and returns the
// label it ends up showing for each item.
func (h *hostView) labels() map[int]string {
	h.m.Lock()
	defer h.m.Unlock()

	labels := make(map[int]string)
	apply := func(id int32, props map[string]dbus.Variant) {
		if label, ok := props["label"].Value().(string); ok {
			labels[int(id)] = label
		}
	}
	var walk func(node []any)
	walk = func(node []any) {
		apply(node[0].(int32), node[1].(map[string]dbus.Variant))
		for _, child := range node[2].([]dbus.Variant) {
			walk(child.Value().([]any))
		}
	}

	for _, msg := range h.msgs {
		sig, _ := msg.Headers[dbus.FieldSignature].Value().(dbus.Signature)
		switch {
		case msg.Type == dbus.TypeSignal:
			member, _ := msg.Headers[dbus.FieldMember].Value().(string)
			if member != "ItemsPropertiesUpdated" {
				continue
			}
			for _, row := range msg.Body[0].([][]any) {
				apply(row[0].(int32), row[1].(map[string]dbus.Variant))
			}

		case msg.Type == dbus.TypeMethodReply && sig.String() == "a(ia{sv})":
			for _, row := range msg.Body[0].([][]any) {
				apply(row[0].(int32), row[1].(map[string]dbus.Variant))
			}

		case msg.Type == dbus.TypeMethodReply && sig.String() == "u(ia{sv}av)":
			walk(msg.Body[1].([]any))
		}
	}
	return labels
}

// startBus starts a private dbus-daemon and returns its address.
func startBus(t *testing.T) string {
	t.Helper()

	daemon, err := exec.LookPath("dbus-daemon")
	if err != nil {
		t.Skip("dbus-daemon not found")
	}
	cmd := exec.Command(daemon, "--session", "--nofork", "--print-address", "--address=unix:dir="+t.TempDir())
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})

	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(addr)
}

// newHostedMenu returns a Menu exported on a private bus and the
// menu object as seen by a host connection that records what it
// receives.
func newHostedMenu(t *testing.T, stall *replyStall) (*Menu, dbus.BusObject, *hostView) {
	t.Helper()

	addr := startBus(t)
	item := &Item{}
	conn, err := dbus.Connect(addr, dbus.WithOutgoingInterceptor(stall.intercept))
	if err != nil {
		t.Fatal(err)
	}
	item.conn = conn

	view := &hostView{}
	host, err := dbus.Connect(addr, dbus.WithIncomingInterceptor(view.record))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		host.Close()
		conn.Close()
	})
	err = host.AddMatchSignal(dbus.WithMatchSender(conn.Names()[0]), dbus.WithMatchInterface(menuInter))
	if err != nil {
		t.Fatal(err)
	}

	item.menu = &Menu{
		item:  item,
		nodes: make(map[int]*MenuItem),
		dirty: make(set.Set[int]),
	}
	if err := item.menu.export(); err != nil {
		t.Fatal(err)
	}
	return item.menu, host.Object(conn.Names()[0], menuPath), view
}

// TestSetPropsBeforeUnwrittenReply checks that a host ends up showing
// the current label when SetProps runs after GetGroupProperties or
// GetLayout has read the old label but before godbus writes the reply.
func TestSetPropsBeforeUnwrittenReply(t *testing.T) {
	calls := map[string][]any{
		"GetGroupProperties": {[]int32{1}, []string{}},
		"GetLayout":          {int32(0), int32(-1), []string{}},
	}
	for method, args := range calls {
		t.Run(method, func(t *testing.T) {
			stall := &replyStall{}
			menu, obj, view := newHostedMenu(t, stall)

			item, err := menu.AddChild(MenuItemLabel("Ticks: 1"))
			if err != nil {
				t.Fatal(err)
			}

			for view.labels()[item.id] != "Ticks: 1" {
				time.Sleep(time.Millisecond)
			}
			layoutUpdates := view.layoutUpdates()

			stall.arm()
			call := obj.Go(menuInter+"."+method, 0, nil, args...)
			select {
			case <-stall.held:
			case <-time.After(5 * time.Second):
				t.Fatal("reply was never sent")
			}

			if err := item.SetProps(MenuItemLabel("Ticks: 2")); err != nil {
				t.Fatal(err)
			}
			close(stall.release)

			select {
			case <-call.Done:
			case <-time.After(5 * time.Second):
				t.Fatal("reply never arrived")
			}
			if call.Err != nil {
				t.Fatal(call.Err)
			}

			var got string
			for range 100 {
				got = view.labels()[item.id]
				if got == "Ticks: 2" {
					if n := view.layoutUpdates(); n != layoutUpdates {
						t.Fatalf("label change sent %d LayoutUpdated signals", n-layoutUpdates)
					}
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatalf("host shows %q, want %q", got, "Ticks: 2")
		})
	}
}
