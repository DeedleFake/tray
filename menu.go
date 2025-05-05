package tray

import (
	"errors"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	menuPath  dbus.ObjectPath = "/StatusNotifierMenu"
	menuInter string          = "com.canonical.dbusmenu"
)

// Menu is the menu for a StatusNotifierItem as specififed by the
// dbusmenu interface. An instance of it is available via [Item.Menu].
type Menu struct {
	item  *Item
	props *prop.Properties

	m        sync.RWMutex
	id       int
	nodes    map[int]*MenuItem
	children []int
	revision uint32
	handler  MenuEventHandler
}

func (item *Item) createMenu() error {
	item.menu = &Menu{
		item:  item,
		nodes: make(map[int]*MenuItem),
	}
	return item.menu.export()
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
			"Version":       makeConstProp(3),
			"TextDirection": makeProp(LeftToRight),
			"Status":        makeProp(Normal),
			"IconThemePath": makeProp[[]string](nil),
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
				Signals: []introspect.Signal{
					{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{
						{Name: "updatedProps", Type: "a(ia{sv})", Direction: "out"},
						{Name: "removedProps", Type: "a(ias)", Direction: "out"},
					}},
					{Name: "LayoutUpdated", Args: []introspect.Arg{
						{Name: "revision", Type: "u", Direction: "out"},
						{Name: "parent", Type: "i", Direction: "out"},
					}},
					{Name: "ItemActivationRequested", Args: []introspect.Arg{
						{Name: "id", Type: "i", Direction: "out"},
						{Name: "timestamp", Type: "u", Direction: "out"},
					}},
				},
			},
		},
	}

	return menu.item.conn.Export(introspect.NewIntrospectable(&node), menuPath, "org.freedesktop.DBus.Introspectable")
}

func (menu *Menu) updateLayout(nodes ...menuNode) error {
	if len(nodes) == 0 {
		// If this happens, it's extremely likely to be a bug.
		panic("no nodes given")
	}

	menu.revision++

	errs := make([]error, 0, len(nodes))
	for _, node := range nodes {
		err := menu.item.conn.Emit(menuPath, "com.canonical.dbusmenu.LayoutUpdated", menu.revision, node.getID())
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// TextDirection returns the current value of the menu's TextDirection
// property.
func (menu *Menu) TextDirection() TextDirection {
	return menu.props.GetMust(menuInter, "TextDirection").(TextDirection)
}

// Status returns the current value of the menu's Status property.
func (menu *Menu) Status() MenuStatus {
	return menu.props.GetMust(menuInter, "Status").(MenuStatus)
}

// IconThemePath returns the current value of the menu's IconThemePath
// property.
func (menu *Menu) IconThemePath() []string {
	return menu.props.GetMust(menuInter, "IconThemePath").([]string)
}

// ItemMenuTextDirection sets the item's associated menu's
// TextDirection property.
func ItemMenuTextDirection(direction TextDirection) ItemProp {
	return func(item *itemProps) {
		item.menu.props.SetMust(menuInter, "TextDirection", direction)
	}
}

// ItemMenuStatus sets the item's associated menu's Status property.
func ItemMenuStatus(status MenuStatus) ItemProp {
	return func(item *itemProps) {
		item.menu.props.SetMust(menuInter, "Status", status)
	}
}

// ItemMenuIconThemePath sets the item's associated menu's
// IconThemePath property.
func ItemMenuIconThemePath(path []string) ItemProp {
	return func(item *itemProps) {
		item.menu.props.SetMust(menuInter, "IconThemePath", path)
	}
}

// ItemMenuHandler sets the MenuEventHandler that is called when the
// menu receives incoming events. Generally speaking, clients will
// probably want to set handlers on [MenuItem], not on the Menu
// itself.
func ItemMenuHandler(handler MenuEventHandler) ItemProp {
	return func(item *itemProps) {
		defer item.menu.lock()()

		item.menu.handler = handler
	}
}

type menuNode interface {
	lock() func()
	getID() int
	getChildren() []int
	setChildren([]int)
}

func (menu *Menu) lock() func() {
	menu.m.Lock()
	return func() { menu.m.Unlock() }
}

func (menu *Menu) getID() int {
	return 0
}

func (menu *Menu) getChildren() []int {
	return menu.children
}

func (menu *Menu) setChildren(c []int) {
	menu.children = c
}
