package tray

import (
	"strings"
	"testing"

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
