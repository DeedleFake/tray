package tray

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
)

// newTestMenu returns a Menu whose D-Bus connection writes into a
// pipe that is drained and discarded, so that signals can be emitted
// without a bus.
func newTestMenu(t *testing.T) *Menu {
	t.Helper()

	local, remote := net.Pipe()
	go io.Copy(io.Discard, remote)

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
	}
}

// testConcurrentWithGetGroupProperties runs write n times while a
// D-Bus client repeatedly reads the menu's properties, and fails if
// the two deadlock.
func testConcurrentWithGetGroupProperties(t *testing.T, menu *Menu, write func()) {
	t.Helper()

	const n = 10000
	var wg sync.WaitGroup
	wg.Go(func() {
		for range n {
			write()
		}
	})
	wg.Go(func() {
		for range n {
			(*dbusmenu)(menu).GetGroupProperties(dbus.Message{}, nil, nil)
		}
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: writer and GetGroupProperties did not finish")
	}
}

func TestSetPropsConcurrentWithGetGroupProperties(t *testing.T) {
	menu := newTestMenu(t)
	item, err := menu.AddChild(MenuItemLabel("item"))
	if err != nil {
		t.Fatal(err)
	}

	testConcurrentWithGetGroupProperties(t, menu, func() {
		item.SetProps(MenuItemLabel("item"))
	})
}

func TestAppendChildConcurrentWithGetGroupProperties(t *testing.T) {
	menu := newTestMenu(t)
	a, err := menu.AddChild(MenuItemLabel("a"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := menu.AddChild(MenuItemLabel("b"))
	if err != nil {
		t.Fatal(err)
	}
	child, err := a.AddChild(MenuItemLabel("child"))
	if err != nil {
		t.Fatal(err)
	}

	dst := []*MenuItem{a, b}
	var i int
	testConcurrentWithGetGroupProperties(t, menu, func() {
		i = (i + 1) % len(dst)
		dst[i].AppendChild(child)
	})
}
