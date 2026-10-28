package tray

import (
	"bufio"
	"context"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// replyStall holds back the next method reply the item sends, inside
// godbus's write path, before the bytes reach the socket. With the fix
// that is while the method still holds Menu.m.
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
	m     sync.Mutex
	msgs  []*dbus.Message
	calls []uint32
}

// sent records the serial of each method call the host sends.
func (h *hostView) sent(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodCall || member(msg) == "AddMatch" {
		return
	}
	h.m.Lock()
	defer h.m.Unlock()
	h.calls = append(h.calls, msg.Serial())
}

// lastCall returns the serial of the last method call the host sent.
func (h *hostView) lastCall() uint32 {
	h.m.Lock()
	defer h.m.Unlock()
	return h.calls[len(h.calls)-1]
}

func (h *hostView) record(msg *dbus.Message) {
	h.m.Lock()
	defer h.m.Unlock()
	h.msgs = append(h.msgs, msg)
}

func (h *hostView) snapshot() []*dbus.Message {
	h.m.Lock()
	defer h.m.Unlock()
	return slices.Clone(h.msgs)
}

func member(msg *dbus.Message) string {
	m, _ := msg.Headers[dbus.FieldMember].Value().(string)
	return m
}

func replySerial(msg *dbus.Message) uint32 {
	s, _ := msg.Headers[dbus.FieldReplySerial].Value().(uint32)
	return s
}

// replies returns the replies the host got to the call with serial.
func (h *hostView) replies(serial uint32) []*dbus.Message {
	var r []*dbus.Message
	for _, msg := range h.snapshot() {
		if (msg.Type == dbus.TypeMethodReply || msg.Type == dbus.TypeError) && replySerial(msg) == serial {
			r = append(r, msg)
		}
	}
	return r
}

func (h *hostView) layoutUpdates() int {
	var n int
	for _, msg := range h.snapshot() {
		if msg.Type == dbus.TypeSignal && member(msg) == "LayoutUpdated" {
			n++
		}
	}
	return n
}

// labels applies signals and replies in arrival order, the way the GNOME
// AppIndicator extension does, and returns the label it ends up showing.
func (h *hostView) labels() map[int]string {
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
	for _, msg := range h.snapshot() {
		sig, _ := msg.Headers[dbus.FieldSignature].Value().(dbus.Signature)
		switch {
		case msg.Type == dbus.TypeSignal && member(msg) == "ItemsPropertiesUpdated":
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

// newHostedMenu connects the item the way New does, plus a reply stall.
func newHostedMenu(t *testing.T, stall *replyStall) (*Menu, dbus.BusObject, *hostView) {
	t.Helper()
	addr := startBus(t)
	item := &Item{}
	conn, err := dbus.Connect(addr,
		dbus.WithIncomingInterceptor(claimMenuReplies),
		dbus.WithOutgoingInterceptor(stall.intercept),
	)
	if err != nil {
		t.Fatal(err)
	}
	item.conn = conn

	view := &hostView{}
	host, err := dbus.Connect(addr, dbus.WithIncomingInterceptor(view.record), dbus.WithOutgoingInterceptor(view.sent))
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
	item.menu = &Menu{item: item, nodes: make(map[int]*MenuItem), dirty: make(set.Set[int])}
	if err := item.menu.export(); err != nil {
		t.Fatal(err)
	}
	return item.menu, host.Object(conn.Names()[0], menuPath), view
}

// replyingArgs has valid arguments for every member of replyingMethods,
// for a menu whose only item has ID 1. TestMenuRepliesOnce fails when a
// member is missing, so opting a method in forces a test for it.
var replyingArgs = map[string][]any{
	"GetLayout":          {int32(0), int32(-1), []string{}},
	"GetGroupProperties": {[]int32{1}, []string{}},
	"GetProperty":        {int32(1), "label"},
}

// TestSetPropsBeforeUnwrittenReply holds a props reply in godbus's write
// path and runs SetProps meanwhile. SetProps must wait for the reply, and
// the host must get the reply before the newer signal and end up showing
// the new label.
func TestSetPropsBeforeUnwrittenReply(t *testing.T) {
	for method, args := range replyingArgs {
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
			serial := view.lastCall()
			select {
			case <-stall.held:
			case <-time.After(5 * time.Second):
				t.Fatal("reply was never sent")
			}

			done := make(chan error, 1)
			go func() { done <- item.SetProps(MenuItemLabel("Ticks: 2")) }()
			select {
			case err := <-done:
				t.Fatalf("SetProps returned (%v) while the reply was unwritten", err)
			case <-time.After(50 * time.Millisecond):
			}
			close(stall.release)

			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("SetProps never returned")
			}
			select {
			case <-call.Done:
			case <-time.After(5 * time.Second):
				t.Fatal("reply never arrived")
			}
			if call.Err != nil {
				t.Fatal(call.Err)
			}

			var replyAt, signalAt = -1, -1
			for i := time.Now(); signalAt < 0 && time.Since(i) < 5*time.Second; time.Sleep(10 * time.Millisecond) {
				for j, msg := range view.snapshot() {
					switch {
					case msg.Type == dbus.TypeMethodReply && replySerial(msg) == serial:
						replyAt = j
					case msg.Type == dbus.TypeSignal && member(msg) == "ItemsPropertiesUpdated" && j > replyAt && replyAt >= 0:
						signalAt = j
					}
				}
			}
			if replyAt < 0 || signalAt < 0 {
				t.Fatalf("reply at %d, newer signal at %d", replyAt, signalAt)
			}
			if got := len(view.replies(serial)); got != 1 {
				t.Fatalf("got %d replies, want 1", got)
			}
			if method != "GetProperty" {
				if got := view.labels()[item.id]; got != "Ticks: 2" {
					t.Fatalf("host shows %q, want %q", got, "Ticks: 2")
				}
			}
			if n := view.layoutUpdates(); n != layoutUpdates {
				t.Fatalf("label change sent %d LayoutUpdated signals", n-layoutUpdates)
			}
		})
	}
}

// callTimeout calls method and fails the call after 2 seconds, so a
// method that never replies fails the test instead of hanging it.
func callTimeout(obj dbus.BusObject, method string, args ...any) *dbus.Call {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return obj.CallWithContext(ctx, method, 0, args...)
}

// TestMenuRepliesOnce checks every method in replyingMethods: one reply
// when a reply is expected, whose signature matches introspection, and
// none when the caller set NoReplyExpected.
func TestMenuRepliesOnce(t *testing.T) {
	outs := make(map[string]string)
	for _, m := range introspect.Methods((*dbusmenu)(nil)) {
		var sig string
		for _, a := range m.Args {
			if a.Direction == "out" {
				sig += a.Type
			}
		}
		outs[m.Name] = sig
	}

	for name := range replyingMethods {
		args, ok := replyingArgs[name]
		if !ok {
			t.Fatalf("%v writes its own reply but has no entry in replyingArgs", name)
		}
		t.Run(name, func(t *testing.T) {
			menu, obj, view := newHostedMenu(t, &replyStall{})
			if _, err := menu.AddChild(MenuItemLabel("x")); err != nil {
				t.Fatal(err)
			}

			call := callTimeout(obj, menuInter+"."+name, args...)
			if call.Err != nil {
				t.Fatal(call.Err)
			}
			serial := view.lastCall()
			// A second call as a barrier for duplicate replies.
			if err := callTimeout(obj, menuInter+"."+name, args...).Err; err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			r := view.replies(serial)
			if len(r) != 1 {
				t.Fatalf("got %d replies, want 1", len(r))
			}
			sig, _ := r[0].Headers[dbus.FieldSignature].Value().(dbus.Signature)
			if name != "GetProperty" && sig.String() != outs[name] { // GetProperty's `any` is sent as its dynamic type, as before
				t.Fatalf("reply signature %q, introspection says %q", sig, outs[name])
			}

			before := len(view.snapshot())
			obj.Go(menuInter+"."+name, dbus.FlagNoReplyExpected, nil, args...)
			if err := callTimeout(obj, menuInter+"."+name, args...).Err; err != nil {
				t.Fatal(err)
			}
			time.Sleep(50 * time.Millisecond)
			var n int
			for _, msg := range view.snapshot()[before:] {
				if msg.Type == dbus.TypeMethodReply {
					n++
				}
			}
			if n != 1 {
				t.Fatalf("NoReplyExpected call got a reply (%d replies, want 1 for the barrier)", n)
			}
		})
	}
}
