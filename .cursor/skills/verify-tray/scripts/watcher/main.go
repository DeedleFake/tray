// Command watcher is a verification stand-in for a desktop's
// StatusNotifierWatcher. It owns org.kde.StatusNotifierWatcher and
// org.freedesktop.StatusNotifierWatcher on the bus named by
// DBUS_SESSION_BUS_ADDRESS, records every RegisterStatusNotifierItem
// call, and claims that a host is registered so items treat the bus
// like a real tray. It is not part of the tray package.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const path = dbus.ObjectPath("/StatusNotifierWatcher")

var spaces = []string{"kde", "freedesktop"}

type watcher struct {
	conn  *dbus.Conn
	m     sync.Mutex
	items []string
	props *prop.Properties
}

func (w *watcher) RegisterStatusNotifierItem(service string, sender dbus.Sender) *dbus.Error {
	full := service
	if !strings.HasPrefix(service, "/") {
		full = service + "/StatusNotifierItem"
	} else {
		full = string(sender) + service
	}
	w.m.Lock()
	w.items = append(w.items, full)
	items := append([]string(nil), w.items...)
	w.m.Unlock()

	log.Printf("RegisterStatusNotifierItem service=%q sender=%q registered=%q", service, sender, full)
	for _, space := range spaces {
		inter := fmt.Sprintf("org.%v.StatusNotifierWatcher", space)
		w.props.SetMust(inter, "RegisteredStatusNotifierItems", items)
		w.conn.Emit(path, inter+".StatusNotifierItemRegistered", full)
	}
	return nil
}

func (w *watcher) RegisterStatusNotifierHost(service string) *dbus.Error {
	log.Printf("RegisterStatusNotifierHost service=%q", service)
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	w := &watcher{conn: conn}

	// Both interfaces must share one prop.Export: a second Export on the
	// same path replaces the first Properties handler.
	propMap := prop.Map{}
	for _, space := range spaces {
		inter := fmt.Sprintf("org.%v.StatusNotifierWatcher", space)
		if err := conn.Export(w, path, inter); err != nil {
			log.Fatalf("export %v: %v", inter, err)
		}
		propMap[inter] = map[string]*prop.Prop{
			"RegisteredStatusNotifierItems":  {Value: []string{}, Emit: prop.EmitTrue},
			"IsStatusNotifierHostRegistered": {Value: true, Emit: prop.EmitTrue},
			"ProtocolVersion":                {Value: int32(0), Emit: prop.EmitConst},
		}
	}
	p, err := prop.Export(conn, path, propMap)
	if err != nil {
		log.Fatalf("export props: %v", err)
	}
	w.props = p

	var ifaces []introspect.Interface
	for _, space := range spaces {
		inter := fmt.Sprintf("org.%v.StatusNotifierWatcher", space)
		ifaces = append(ifaces, introspect.Interface{
			Name:       inter,
			Methods:    introspect.Methods(w),
			Properties: p.Introspection(inter),
			Signals: []introspect.Signal{
				{Name: "StatusNotifierItemRegistered", Args: []introspect.Arg{{Name: "service", Type: "s"}}},
				{Name: "StatusNotifierItemUnregistered", Args: []introspect.Arg{{Name: "service", Type: "s"}}},
				{Name: "StatusNotifierHostRegistered"},
			},
		})
	}
	node := introspect.Node{Name: string(path), Interfaces: append([]introspect.Interface{introspect.IntrospectData, prop.IntrospectData}, ifaces...)}
	conn.Export(introspect.NewIntrospectable(&node), path, "org.freedesktop.DBus.Introspectable")

	for _, space := range spaces {
		name := fmt.Sprintf("org.%v.StatusNotifierWatcher", space)
		reply, err := conn.RequestName(name, dbus.NameFlagDoNotQueue)
		if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			log.Fatalf("request %v: reply=%v err=%v", name, reply, err)
		}
		log.Printf("owns %v", name)
	}
	log.Printf("watcher ready")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}
