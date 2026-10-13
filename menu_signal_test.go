package tray

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) snapshot() []byte {
	l.mu.Lock()
	defer l.mu.Unlock()
	return bytes.Clone(l.b.Bytes())
}

func (l *lockedBuf) waitQuiet() []byte {
	prev := -1
	var snap []byte
	for range 40 {
		time.Sleep(5 * time.Millisecond)
		snap = l.snapshot()
		if len(snap) == prev {
			return snap
		}
		prev = len(snap)
	}
	return snap
}

type countReader struct {
	r io.Reader
	n int
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

type dbusSignal struct {
	member string
	sig    string
	body   []any
}

func newCaptureMenu(t *testing.T) (*Menu, *lockedBuf) {
	t.Helper()
	local, remote := net.Pipe()
	buf := &lockedBuf{}
	go func() { _, _ = io.Copy(buf, remote) }()
	conn, err := dbus.NewConn(local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		remote.Close()
	})
	return &Menu{
		item:  &Item{conn: conn},
		nodes: make(map[int]*MenuItem),
		dirty: make(set.Set[int]),
	}, buf
}

func decodeSignals(raw []byte) []dbusSignal {
	var out []dbusSignal
	rest := raw
	for len(rest) > 0 {
		cr := &countReader{r: bytes.NewReader(rest)}
		msg, err := dbus.DecodeMessage(cr)
		if err != nil || cr.n == 0 {
			rest = rest[1:]
			continue
		}
		member, _ := msg.Headers[dbus.FieldMember].Value().(string)
		sig, _ := msg.Headers[dbus.FieldSignature].Value().(dbus.Signature)
		if member != "" {
			out = append(out, dbusSignal{member: member, sig: sig.String(), body: msg.Body})
		}
		rest = rest[cr.n:]
	}
	return out
}

func signalBytes(buf *lockedBuf, from int) []byte {
	raw := buf.waitQuiet()
	if from > len(raw) {
		return nil
	}
	return raw[from:]
}

type propUpdate struct {
	id    int
	props map[string]string
}

func propertyUpdates(body []any) []propUpdate {
	if len(body) == 0 {
		return nil
	}
	rows, ok := body[0].([][]any)
	if !ok {
		return nil
	}
	var out []propUpdate
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		id32, ok := row[0].(int32)
		if !ok {
			continue
		}
		raw, ok := row[1].(map[string]dbus.Variant)
		if !ok {
			continue
		}
		props := make(map[string]string, len(raw))
		for name, v := range raw {
			s, ok := v.Value().(string)
			if ok {
				props[name] = s
			}
		}
		out = append(out, propUpdate{id: int(id32), props: props})
	}
	return out
}

func TestAddChildAndRemoveEmitChildrenDisplay(t *testing.T) {
	menu, buf := newCaptureMenu(t)
	parent, err := menu.AddChild(MenuItemLabel("parent"))
	if err != nil {
		t.Fatal(err)
	}
	mark := len(buf.waitQuiet())

	child, err := parent.AddChild(MenuItemLabel("child"))
	if err != nil {
		t.Fatal(err)
	}
	var sawChild, sawDisplay bool
	for _, sig := range decodeSignals(signalBytes(buf, mark)) {
		if sig.member != "ItemsPropertiesUpdated" {
			continue
		}
		if sig.sig != "a(ia{sv})a(ias)" {
			t.Fatalf("ItemsPropertiesUpdated signature = %s, want a(ia{sv})a(ias)", sig.sig)
		}
		for _, upd := range propertyUpdates(sig.body) {
			if upd.id == child.id && upd.props["label"] == "child" {
				sawChild = true
			}
			if upd.id == parent.id && upd.props["children-display"] == "submenu" {
				sawDisplay = true
			}
		}
	}
	if !sawChild || !sawDisplay {
		t.Fatalf("child label signal=%v children-display signal=%v", sawChild, sawDisplay)
	}

	mark = len(buf.waitQuiet())
	if err := child.Remove(); err != nil {
		t.Fatal(err)
	}
	sawDisplay = false
	for _, sig := range decodeSignals(signalBytes(buf, mark)) {
		if sig.member != "ItemsPropertiesUpdated" || sig.sig != "a(ia{sv})a(ias)" {
			continue
		}
		for _, upd := range propertyUpdates(sig.body) {
			if upd.id == parent.id && upd.props["children-display"] == "" {
				sawDisplay = true
			}
		}
	}
	if !sawDisplay {
		t.Fatal("Remove did not emit children-display \"\"")
	}
	if parent.props["children-display"] != "" || len(parent.children) != 0 {
		t.Fatalf("after remove props=%#v children=%v", parent.props["children-display"], parent.children)
	}
}

func TestMoveBeforeSameParent(t *testing.T) {
	menu, _ := newCaptureMenu(t)
	parent, err := menu.AddChild(MenuItemLabel("parent"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := parent.AddChild(MenuItemLabel("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := parent.AddChild(MenuItemLabel("second"))
	if err != nil {
		t.Fatal(err)
	}

	if err := first.MoveBefore(second); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(parent.children, []int{first.id, second.id}) {
		t.Fatalf("MoveBefore earlier sibling children = %v, want %v", parent.children, []int{first.id, second.id})
	}

	if err := second.MoveBefore(first); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(parent.children, []int{second.id, first.id}) {
		t.Fatalf("MoveBefore later sibling children = %v, want %v", parent.children, []int{second.id, first.id})
	}

	if err := first.Remove(); err != nil {
		t.Fatal(err)
	}
	if err := second.Remove(); err != nil {
		t.Fatal(err)
	}
	if len(parent.children) != 0 || parent.props["children-display"] != "" {
		t.Fatalf("after removing moved children = %v children-display=%#v", parent.children, parent.props["children-display"])
	}
}

func TestMoveBeforeAcrossParents(t *testing.T) {
	menu, _ := newCaptureMenu(t)
	src, err := menu.AddChild(MenuItemLabel("src"))
	if err != nil {
		t.Fatal(err)
	}
	dst, err := menu.AddChild(MenuItemLabel("dst"))
	if err != nil {
		t.Fatal(err)
	}
	moving, err := src.AddChild(MenuItemLabel("moving"))
	if err != nil {
		t.Fatal(err)
	}
	anchor, err := dst.AddChild(MenuItemLabel("anchor"))
	if err != nil {
		t.Fatal(err)
	}

	if err := moving.MoveBefore(anchor); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(dst.children, []int{moving.id, anchor.id}) {
		t.Fatalf("dst children = %v, want %v", dst.children, []int{moving.id, anchor.id})
	}
	if len(src.children) != 0 || src.props["children-display"] != "" {
		t.Fatalf("src children = %v children-display=%#v", src.children, src.props["children-display"])
	}
}

func TestSetPropsEmitsPropertyChangesOnly(t *testing.T) {
	menu, buf := newCaptureMenu(t)
	item, err := menu.AddChild(
		MenuItemLabel("Quit"),
		MenuItemToggleType(Checkmark),
		MenuItemToggleState(Off),
	)
	if err != nil {
		t.Fatal(err)
	}
	mark := len(buf.waitQuiet())

	if err := item.SetProps(MenuItemLabel("Quit"), MenuItemToggleState(Off)); err != nil {
		t.Fatal(err)
	}
	if sigs := decodeSignals(signalBytes(buf, mark)); len(sigs) != 0 {
		t.Fatalf("unchanged SetProps signals = %+v", sigs)
	}

	mark = len(buf.waitQuiet())
	if err := item.SetProps(MenuItemLabel("Exit"), MenuItemToggleState(On)); err != nil {
		t.Fatal(err)
	}
	sigs := decodeSignals(signalBytes(buf, mark))
	if len(sigs) != 1 || sigs[0].member != "ItemsPropertiesUpdated" {
		t.Fatalf("changed SetProps signals = %+v", sigs)
	}
	rows, ok := sigs[0].body[0].([][]any)
	if !ok || len(rows) != 1 || len(rows[0]) < 2 {
		t.Fatalf("ItemsPropertiesUpdated body = %#v", sigs[0].body)
	}
	props, ok := rows[0][1].(map[string]dbus.Variant)
	if !ok {
		t.Fatalf("updated props = %#v", rows[0][1])
	}
	if props["label"].Value() != "Exit" || props["toggle-state"].Value() != int32(On) {
		t.Fatalf("updated props = %#v", props)
	}

	mark = len(buf.waitQuiet())
	if _, err := item.AddChild(MenuItemLabel("child")); err != nil {
		t.Fatal(err)
	}
	var sawLayout bool
	for _, sig := range decodeSignals(signalBytes(buf, mark)) {
		if sig.member == "LayoutUpdated" {
			sawLayout = true
		}
	}
	if !sawLayout {
		t.Fatal("AddChild did not emit LayoutUpdated")
	}
}

func TestSetPropsAfterParentRemoved(t *testing.T) {
	menu, _ := newCaptureMenu(t)
	parent, err := menu.AddChild(MenuItemLabel("parent"))
	if err != nil {
		t.Fatal(err)
	}
	child, err := parent.AddChild(MenuItemLabel("child"))
	if err != nil {
		t.Fatal(err)
	}
	if err := parent.Remove(); err != nil {
		t.Fatal(err)
	}
	err = child.SetProps(MenuItemLabel("renamed"))
	want := fmt.Sprintf("menu item %d has no parent", child.id)
	if err == nil || err.Error() != want {
		t.Fatalf("SetProps err = %v, want %s", err, want)
	}
	if child.props["label"] != "child" {
		t.Fatalf("label = %#v", child.props["label"])
	}
}
