package tray

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	menuPath  dbus.ObjectPath = "/StatusNotifierMenu"
	menuInter                 = "com.canonical.dbusmenu"
)

type Menu struct {
	item  *Item
	props *prop.Properties
}

func (item *Item) Menu() (*Menu, error) {
	return item.menu.Get(func() (*Menu, error) {
		menu := Menu{
			item: item,
		}
		err := menu.export()
		if err != nil {
			return nil, err
		}

		return &menu, nil
	})
}

func (menu *Menu) export() error {
	err := menu.item.conn.Export((*dbusmenu)(menu), menuPath, menuInter)
	if err != nil {
		return err
	}

	err = menu.exportProps()
	if err != nil {
		return err
	}

	err = menu.exportIntrospect()
	if err != nil {
		return err
	}

	return nil
}

func (menu *Menu) exportProps() error {
	m := prop.Map{
		menuInter: map[string]*prop.Prop{
			"Version":       makeConstProp(20),
			"TextDirection": makeProp(LeftToRight),
			"Status":        makeProp(Normal),
			"IconThemePath": makeProp(""),
		},
	}

	props, err := prop.Export(menu.item.conn, menuPath, m)
	if err != nil {
		return err
	}
	menu.props = props
	return nil
}

func (menu *Menu) exportIntrospect() error {
	node := introspect.Node{
		Name: string(menuPath),
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{
				Name:       menuInter,
				Methods:    introspect.Methods((*dbusmenu)(menu)),
				Properties: menu.props.Introspection(menuInter),
			},
		},
	}

	return menu.item.conn.Export(introspect.NewIntrospectable(&node), menuPath, "org.freedesktop.DBus.Introspectable")
}

type dbusmenu Menu

type TextDirectiuon string

const (
	LeftToRight TextDirectiuon = "ltr"
	RightToLeft TextDirectiuon = "rtl"
)

type MenuStatus string

const (
	Normal MenuStatus = "normal"
	Notice MenuStatus = "notice"
)
