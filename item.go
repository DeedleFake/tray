package tray

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

type Item struct {
	conn *dbus.Conn
	sni  dbus.BusObject
}

func New() (*Item, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}

	item := Item{
		conn: conn,
	}
	err = item.export()
	if err != nil {
		return nil, err
	}

	return &item, nil
}

func (item *Item) export() error {
	space, err := getSpace(item.conn)
	if err != nil {
		return err
	}
	name := getName(space)
	item.sni = item.conn.Object(name, "/StatusNotifierItem")

	err = item.conn.Export(
		&statusNotifierItem{item: item},
		item.sni.Path(),
		item.sni.Destination(),
	)
	if err != nil {
		return err
	}

	reply, err := item.conn.RequestName(item.sni.Destination(), 0)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bad reply to name request: %v", reply)
	}

	return nil
}

func (item *Item) Close() error {
	reply, err := item.conn.ReleaseName(item.sni.Destination())
	if err != nil {
		return err
	}
	if reply != dbus.ReleaseNameReplyReleased {
		return fmt.Errorf("bad reply to name release: %v", reply)
	}

	return nil
}

func (item *Item) Register() error {
	space, err := getSpace(item.conn)
	if err != nil {
		return err
	}

	return item.conn.Object(
		fmt.Sprintf("org.%v.StatusNotifierWatcher", space),
		"/StatusNotifierWatcher",
	).Call(
		fmt.Sprintf("org.%v.StatusNotifierWatcher.RegisterStatusNotifierItem", space),
		0,
		item.sni.Destination(),
	).Store()
}

type statusNotifierItem struct {
	item *Item
}
