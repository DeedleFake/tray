package tray

import (
	"bytes"
	"errors"
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

func (menu *Menu) AddItem(props ...MenuItemProp) (*MenuItem, error) {
	menu.m.Lock()
	defer menu.m.Unlock()

	item := menu.newItem()
	menu.children = append(menu.children, menu.id)

	item.m.Lock()
	defer item.m.Unlock()

	menu.revision++
	errs := item.applyProps(props)
	errs = append(errs, menu.item.conn.Emit(menuPath, "com.canonical.dbusmenu.LayoutUpdated", menu.revision, 0))

	return item, errors.Join(errs...)
}

func (item *MenuItem) AddItem(props ...MenuItemProp) *MenuItem {
	item.menu.m.Lock()
	defer item.menu.m.Unlock()

	item.m.Lock()
	defer item.m.Unlock()

	child := item.menu.newItem()
	item.children = append(item.children, child.id)

	child.m.Lock()
	defer child.m.Unlock()

	child.menu.revision++
	errs := child.applyProps(props)
	errs = append(errs, item.emitLayoutUpdated())

	return child
}

func (item *MenuItem) applyProps(props []MenuItemProp) []error {
	w := menuItemProps{MenuItem: item}
	for _, p := range props {
		p(&w)
	}
	return w.errs
}

func (item *MenuItem) emitLayoutUpdated() error {
	return item.menu.item.conn.Emit(menuPath, "com.canonical.dbusmenu.LayoutUpdated", item.menu.revision, item.id)
}

func (item *MenuItem) Type() MenuType {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "type", MenuType("standard"))
}

func (item *MenuItem) Label() string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "label", "")
}

func (item *MenuItem) Enabled() bool {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "enabled", true)
}

func (item *MenuItem) Visible() bool {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "visible", true)
}

func (item *MenuItem) IconName() string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "icon-name", "")
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

func (item *MenuItem) Shortcut() [][]string {
	item.m.RLock()
	defer item.m.RUnlock()

	v, _ := item.props["shortcut"].([][]string)
	return v
}

func (item *MenuItem) SetProps(props ...MenuItemProp) error {
	item.m.Lock()
	defer item.m.Unlock()

	errs := item.applyProps(props)

	item.menu.m.Lock()
	defer item.menu.m.Unlock()

	item.menu.revision++
	errs = append(errs, item.emitLayoutUpdated())

	return errors.Join(errs...)
}

type MenuType string

const (
	Standard  MenuType = "standard"
	Separator MenuType = "separator"
)

type MenuEventHandler func(eventID MenuEventID, data any, timestamp uint32) error

type MenuItemProp func(*menuItemProps)

type menuItemProps struct {
	*MenuItem
	errs []error
}

func (item *menuItemProps) catch(err error) {
	item.errs = append(item.errs, err)
}

func MenuItemType(t MenuType) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["type"] = t
	}
}

func MenuItemLabel(label string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["label"] = label
	}
}

func MenuItemEnabled(enabled bool) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["enabled"] = enabled
	}
}

func MenuItemVisible(visible bool) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["visible"] = visible
	}
}

func MenuItemIconName(name string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["icon-name"] = name
	}
}

func MenuItemIconData(img image.Image) MenuItemProp {
	return func(item *menuItemProps) {
		var buf bytes.Buffer
		err := png.Encode(&buf, img)
		if err != nil {
			item.catch(err)
			return
		}

		item.props["icon-data"] = buf.Bytes()
	}
}

func MenuItemShortcut(shortcut [][]string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["shortcut"] = shortcut
	}
}

func MenuItemHandler(handler MenuEventHandler) MenuItemProp {
	return func(item *menuItemProps) {
		item.handler = handler
	}
}
