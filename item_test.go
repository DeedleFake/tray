package tray

import (
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func listBusNames(t *testing.T, conn *dbus.Conn) []string {
	t.Helper()
	var names []string
	if err := conn.BusObject().Call("org.freedesktop.DBus.ListNames", 0).Store(&names); err != nil {
		t.Fatal(err)
	}
	return names
}

func TestFailedNewClosesConn(t *testing.T) {
	addr := startBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	probe, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { probe.Close() })

	before := make(map[string]struct{})
	for _, name := range listBusNames(t, probe) {
		before[name] = struct{}{}
	}

	item, err := New()
	if err == nil {
		_ = item.Close()
		t.Fatal("New succeeded without a StatusNotifierWatcher")
	}
	if item != nil {
		t.Fatalf("New returned %v with error %v", item, err)
	}

	var leaked []string
	for _, name := range listBusNames(t, probe) {
		if _, ok := before[name]; ok {
			continue
		}
		leaked = append(leaked, name)
	}
	if len(leaked) != 0 {
		t.Fatalf("failed New left bus names %v", leaked)
	}
	for _, name := range listBusNames(t, probe) {
		if strings.HasPrefix(name, "org.freedesktop.StatusNotifierItem-") {
			t.Fatalf("failed New left well-known name %s", name)
		}
	}
}

const kdeWatcherName = "org.kde.StatusNotifierWatcher"

type fakeWatcher struct {
	registered chan string
}

func (w *fakeWatcher) RegisterStatusNotifierItem(service string) *dbus.Error {
	w.registered <- service
	return nil
}

func startKDEWatcher(t *testing.T, addr string) (*fakeWatcher, *dbus.Conn) {
	t.Helper()
	conn, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	w := &fakeWatcher{registered: make(chan string, 8)}
	if err := conn.Export(w, "/StatusNotifierWatcher", kdeWatcherName); err != nil {
		t.Fatal(err)
	}
	reply, err := conn.RequestName(kdeWatcherName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request %s: reply=%v err=%v", kdeWatcherName, reply, err)
	}
	return w, conn
}

func waitRegistered(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case name := <-ch:
		return name
	case <-time.After(5 * time.Second):
		t.Fatal("RegisterStatusNotifierItem was not called")
		return ""
	}
}

func waitNameOwner(t *testing.T, conn *dbus.Conn, name string, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var owned bool
		if err := conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, name).Store(&owned); err != nil {
			t.Fatal(err)
		}
		if owned == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s owner=%v", name, want)
}

func TestItemReregistersWhenWatcherRestarts(t *testing.T) {
	addr := startBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	probe, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { probe.Close() })

	first, firstConn := startKDEWatcher(t, addr)
	item, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = item.Close() })
	waitRegistered(t, first.registered)

	if err := firstConn.Close(); err != nil {
		t.Fatal(err)
	}
	waitNameOwner(t, probe, kdeWatcherName, false)

	second, _ := startKDEWatcher(t, addr)
	waitNameOwner(t, probe, kdeWatcherName, true)
	waitRegistered(t, second.registered)
}

func TestCloseStopsWatcherWatch(t *testing.T) {
	addr := startBus(t)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", addr)

	probe, err := dbus.Connect(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { probe.Close() })

	first, firstConn := startKDEWatcher(t, addr)
	item, err := New()
	if err != nil {
		t.Fatal(err)
	}
	waitRegistered(t, first.registered)

	if err := item.Close(); err != nil {
		t.Fatal(err)
	}
	if err := item.Close(); err != nil {
		t.Fatal(err)
	}

	if err := firstConn.Close(); err != nil {
		t.Fatal(err)
	}
	waitNameOwner(t, probe, kdeWatcherName, false)

	second, _ := startKDEWatcher(t, addr)
	waitNameOwner(t, probe, kdeWatcherName, true)
	select {
	case name := <-second.registered:
		t.Fatalf("RegisterStatusNotifierItem called after Close: %s", name)
	case <-time.After(500 * time.Millisecond):
	}
}
