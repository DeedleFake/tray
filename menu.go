package tray

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	menuPath  dbus.ObjectPath = "/StatusNotifierMenu"
	menuInter string          = "com.canonical.dbusmenu"
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

type dbusmenu Menu

func (menu *dbusmenu) GetLayout(parentID int, recursionDepth int, propertyNames []string) (revision uint32, layout menuLayout, derr *dbus.Error) {
	return 0, menuLayout{}, nil
}

func (menu *dbusmenu) GetGroupProperties(ids []int, propertyNames []string) ([]menuProps, *dbus.Error) {
	return nil, nil
}

func (menu *dbusmenu) GetProperty(id int, name string) (any, *dbus.Error) {
	return nil, nil
}

func (menu *dbusmenu) Event(id int, eventID MenuEventID, data any, timestamp uint) *dbus.Error {
	return nil
}

func (menu *dbusmenu) EventGroup(events []menuEvent) ([]int, *dbus.Error) {
	return nil, nil
}

func (menu *dbusmenu) AboutToShow(id int) (bool, *dbus.Error) {
	return false, nil
}

func (menu *dbusmenu) AboutToShowGroup(ids []int) ([]menuUpdate, []int, *dbus.Error) {
	return nil, nil, nil
}

type TextDirection string

const (
	LeftToRight TextDirection = "ltr"
	RightToLeft TextDirection = "rtl"
)

type MenuStatus string

const (
	Normal MenuStatus = "normal"
	Notice MenuStatus = "notice"
)

type MenuEventID string

const (
	Clicked  MenuEventID = "clicked"
	Hovereed MenuEventID = "hovered"
)

type menuLayout struct {
	ID         int
	Properties map[string]any
	Children   []any
}

type menuProps struct {
	ID         int
	Properties map[string]any
}

type menuEvent struct {
	ID        int
	EventID   MenuEventID
	Data      any
	Timestamp uint
}

type menuUpdate struct {
	ID         int
	NeedUpdate bool
}
