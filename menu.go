package tray

import (
	"sync"

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

	m        sync.RWMutex
	id       int
	layout   map[int]*MenuItem
	children []int
	revision uint32
	handler  MenuEventHandler
}

func (item *Item) createMenu() error {
	item.menu = &Menu{
		item:   item,
		layout: make(map[int]*MenuItem),
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

func (menu *Menu) SetHandler(handler MenuEventHandler) {
	menu.m.Lock()
	defer menu.m.Unlock()

	menu.handler = handler
}

type dbusmenu Menu

func (menu *dbusmenu) buildLayout(item *MenuItem, depth int, props []string) menuLayout {
	var id int
	properties := map[string]any{"children-display": "submenu"}
	if item != nil {
		item.m.RLock()
		defer item.m.RUnlock()

		id = item.id
		properties = mapSlice(item.props, props)
	}

	return menuLayout{
		ID:         id,
		Properties: properties,
		Children:   menu.buildChildren(item, depth, props),
	}
}

func (menu *dbusmenu) buildChildren(parent *MenuItem, depth int, props []string) []any {
	if depth == 0 { // -1 is infinite, so check is exactly to 0.
		return nil
	}

	ids := menu.children
	if parent != nil {
		ids = parent.children
	}

	children := make([]any, 0, len(ids))
	for _, id := range ids {
		child := menu.layout[id]
		if child != nil {
			children = append(children, menu.buildLayout(child, depth-1, props))
		}
	}

	return children
}

func (menu *dbusmenu) GetLayout(parentID int, recursionDepth int, propertyNames []string) (revision uint32, layout menuLayout, derr *dbus.Error) {
	log("menu method", "name", "GetLayout", "parentID", parentID, "propertyNames", propertyNames)

	menu.m.RLock()
	defer menu.m.RUnlock()

	layout = menu.buildLayout(nil, recursionDepth, propertyNames)
	return menu.revision, layout, nil
}

func (menu *dbusmenu) GetGroupProperties(ids []int, propertyNames []string) ([]menuProps, *dbus.Error) {
	log("menu method", "name", "GetGroupProperties", "ids", ids, "propertyNames", propertyNames)

	menu.m.RLock()
	defer menu.m.RUnlock()

	r := make([]menuProps, len(ids))
	for i, id := range ids {
		item := menu.layout[id]
		if item == nil {
			continue
		}

		item.m.RLock()
		r[i] = menuProps{
			ID:         id,
			Properties: mapSlice(item.props, propertyNames),
		}
		item.m.RUnlock()
	}
	return r, nil
}

func (menu *dbusmenu) GetProperty(id int, name string) (any, *dbus.Error) {
	log("menu method", "name", "GetProperty", "id", id, "name", name)

	menu.m.RLock()
	defer menu.m.RUnlock()

	item := menu.layout[id]
	if item == nil {
		return nil, nil
	}

	item.m.RLock()
	defer item.m.RUnlock()

	return item.props[name], nil
}

func (menu *dbusmenu) getHandler(id int) MenuEventHandler {
	menu.m.RLock()
	defer menu.m.RUnlock()

	if id == 0 {
		return menu.handler
	}

	item := menu.layout[id]
	if item == nil {
		return nil
	}

	item.m.RLock()
	defer item.m.RUnlock()

	return item.handler
}

func (menu *dbusmenu) Event(id int, eventID MenuEventID, data dbus.Variant, timestamp uint32) *dbus.Error {
	log("menu method", "name", "Event", "id", id, "eventID", eventID, "data", data, "timestamp", timestamp)

	h := menu.getHandler(id)
	if h == nil {
		return nil
	}

	err := h(eventID, data.Value(), timestamp)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (menu *dbusmenu) EventGroup(events []menuEvent) ([]int, *dbus.Error) {
	log("menu method", "name", "EventGroup", "events", events)
	return nil, nil
}

func (menu *dbusmenu) AboutToShow(id int) (bool, *dbus.Error) {
	log("menu method", "name", "AboutToShow", "id", id)
	// TODO: Return true only if changes have happened.
	return true, nil
}

func (menu *dbusmenu) AboutToShowGroup(ids []int) ([]menuUpdate, []int, *dbus.Error) {
	log("menu method", "name", "AboutToShowGroup", "ids", ids)
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
	Clicked MenuEventID = "clicked"
	Hovered MenuEventID = "hovered"
	Closed  MenuEventID = "closed"
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
	Timestamp uint32
}

type menuUpdate struct {
	ID         int
	NeedUpdate bool
}
