package tray

import (
	"bytes"
	"image"
	"image/png"
	"sync"
)

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
