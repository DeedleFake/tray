package tray

import (
	"bytes"
	"image"
	"image/png"
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

type MenuItem struct {
	menu *Menu
	id   int

	m        sync.RWMutex
	props    map[string]any
	children []int
	handler  MenuEventHandler
}

func (menu *Menu) newItem() *MenuItem {
	menu.id++
	item := MenuItem{
		menu:  menu,
		id:    menu.id,
		props: make(map[string]any),
	}

	menu.layout[item.id] = &item
	menu.revision++

	return &item
}

func (menu *Menu) AddItem() *MenuItem {
	menu.m.Lock()
	defer menu.m.Unlock()

	item := menu.newItem()
	menu.children = append(menu.children, menu.id)

	menu.item.conn.Emit(menuPath, "com.canonical.dbusmenu.LayoutUpdated", menu.revision, 0)

	return item
}

func (item *MenuItem) AddItem() *MenuItem {
	item.menu.m.Lock()
	defer item.menu.m.Unlock()

	item.m.Lock()
	defer item.m.Unlock()

	child := item.menu.newItem()
	item.children = append(item.children, child.id)
	item.emitLayoutUpdated()

	return child
}

func (item *MenuItem) emitLayoutUpdated() error {
	return item.menu.item.conn.Emit(menuPath, "com.canonical.dbusmenu.LayoutUpdated", item.menu.revision, item.id)
}

func (item *MenuItem) Type() MenuItemType {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "type", MenuItemType("standard"))
}

func (item *MenuItem) SetType(t MenuItemType) {
	item.m.Lock()
	defer item.m.Unlock()

	item.props["type"] = t
	item.emitLayoutUpdated()
}

func (item *MenuItem) Label() string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "label", "")
}

func (item *MenuItem) SetLabel(label string) {
	item.m.Lock()
	defer item.m.Unlock()

	item.props["label"] = label
	item.emitLayoutUpdated()
}

func (item *MenuItem) Enabled() bool {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "enabled", true)
}

func (item *MenuItem) SetEnabled(enabled bool) {
	item.m.Lock()
	defer item.m.Unlock()

	item.props["enabled"] = enabled
	item.emitLayoutUpdated()
}

func (item *MenuItem) Visible() bool {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "visible", true)
}

func (item *MenuItem) SetVisible(visible bool) {
	item.m.Lock()
	defer item.m.Unlock()

	item.props["visible"] = visible
	item.emitLayoutUpdated()
}

func (item *MenuItem) IconName() string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "icon-name", "")
}

func (item *MenuItem) SetIconName(name string) {
	item.m.Lock()
	defer item.m.Unlock()

	item.props["icon-name"] = name
	item.emitLayoutUpdated()
}

func (item *MenuItem) IconData() (image.Image, error) {
	item.m.RLock()
	defer item.m.RUnlock()

	data, ok := item.props["icon-data"].([]byte)
	if !ok {
		return nil, nil
	}
	return png.Decode(bytes.NewReader(data))
}

func (item *MenuItem) SetIconData(img image.Image) error {
	item.m.Lock()
	defer item.m.Unlock()

	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	if err != nil {
		return err
	}

	item.props["icon-data"] = buf.Bytes()
	item.emitLayoutUpdated()
	return nil
}

func (item *MenuItem) Shortcut() [][]string {
	item.m.RLock()
	defer item.m.RUnlock()

	v, _ := item.props["shortcut"].([][]string)
	return v
}

func (item *MenuItem) SetShortcut(shortcut [][]string) {
	item.m.Lock()
	defer item.m.Unlock()

	item.props["shortcut"] = shortcut
	item.emitLayoutUpdated()
}

func (item *MenuItem) SetHandler(handler MenuEventHandler) {
	item.m.Lock()
	defer item.m.Unlock()

	item.handler = handler
}

type MenuItemType string

const (
	Standard  MenuItemType = "standard"
	Separator MenuItemType = "separator"
)

type MenuEventHandler func(eventID MenuEventID, data any, timestamp uint32) error
