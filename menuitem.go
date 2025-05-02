package tray

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"slices"
	"sync"
)

type MenuItem struct {
	menu   *Menu
	id     int
	parent int

	m        sync.RWMutex
	props    map[string]any
	children []int
	handler  MenuEventHandler
}

func (menu *Menu) newItem(parent int) *MenuItem {
	menu.id++
	item := MenuItem{
		menu:   menu,
		id:     menu.id,
		parent: parent,
		props:  make(map[string]any),
	}

	menu.layout[item.id] = &item

	return &item
}

func (menu *Menu) AddItem(props ...MenuItemProp) (*MenuItem, error) {
	menu.m.Lock()
	defer menu.m.Unlock()

	item := menu.newItem(0)
	menu.children = append(menu.children, menu.id)

	item.m.Lock()
	defer item.m.Unlock()

	dirty, errs := item.applyProps(props)

	menu.revision++
	errs = append(errs, menu.emitLayoutUpdated())
	errs = append(errs, item.emitPropertiesUpdated(dirty))

	return item, errors.Join(errs...)
}

func (item *MenuItem) AddItem(props ...MenuItemProp) (*MenuItem, error) {
	item.menu.m.Lock()
	defer item.menu.m.Unlock()

	item.m.Lock()
	defer item.m.Unlock()

	child := item.menu.newItem(item.id)
	item.children = append(item.children, child.id)

	child.m.Lock()
	defer child.m.Unlock()

	dirty, errs := child.applyProps(props)
	item.props["children-display"] = "submenu"

	child.menu.revision++
	errs = append(errs, item.emitLayoutUpdated())
	errs = append(errs, item.emitPropertiesUpdated(dirty))

	return child, errors.Join(errs...)
}

func (item *MenuItem) Remove() {
	item.menu.m.Lock()
	defer item.menu.m.Unlock()

	delete(item.menu.layout, item.id)
	if item.parent == 0 {
		item.menu.children = sliceRemove(item.menu.children, item.id)
		item.menu.revision++
		item.menu.emitLayoutUpdated()
		return
	}

	parent := item.menu.layout[item.parent]
	if parent == nil {
		return
	}

	parent.m.Lock()
	defer parent.m.Unlock()

	parent.children = sliceRemove(parent.children, item.id)
	if len(parent.children) == 0 {
		delete(parent.props, "children-display")
	}

	item.menu.revision++
	parent.emitLayoutUpdated()
}

func (item *MenuItem) applyProps(props []MenuItemProp) ([]string, []error) {
	w := menuItemProps{MenuItem: item}
	for _, p := range props {
		p(&w)
	}
	return w.dirty, w.errs
}

func (item *MenuItem) emitPropertiesUpdated(props []string) error {
	type prop struct {
		Name  string
		Value any
	}

	type updatedProps struct {
		ID    int
		Props []prop
	}

	type removedProps struct {
		ID    int
		Props []string
	}

	updated := make([]prop, 0, len(props))
	for _, change := range props {
		updated = append(updated, prop{
			Name:  change,
			Value: item.props[change],
		})
	}

	return item.menu.item.conn.Emit(
		menuPath,
		"com.canonical.dbusmenu.ItemsPropertiesUpdated",
		[]updatedProps{{
			ID:    item.id,
			Props: updated,
		}},
		[]removedProps(nil),
	)
}

func (menu *Menu) emitLayoutUpdated() error {
	return menu.item.conn.Emit(menuPath, "com.canonical.dbusmenu.LayoutUpdated", menu.revision, 0)
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

	return mapLookup(item.props, "shortcut", [][]string(nil))
}

func (item *MenuItem) ToggleType() MenuToggleType {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "toggle-type", MenuToggleType(""))
}

func (item *MenuItem) ToggleState() MenuToggleState {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "toggle-state", MenuToggleState(-1))
}

func (item *MenuItem) VendorProp(vendor, prop string) (any, bool) {
	item.m.RLock()
	defer item.m.RUnlock()

	v, ok := item.props[vendorPropName(vendor, prop)]
	return v, ok
}

func (item *MenuItem) SetProps(props ...MenuItemProp) error {
	item.m.Lock()
	defer item.m.Unlock()

	dirty, errs := item.applyProps(props)
	errs = append(errs, item.emitPropertiesUpdated(dirty))
	return errors.Join(errs...)
}

type MenuType string

const (
	Standard  MenuType = "standard"
	Separator MenuType = "separator"
)

type MenuToggleType string

const (
	NonToggleable MenuToggleType = ""
	Checkmark     MenuToggleType = "checkmark"
	Radio         MenuToggleType = "radio"
)

type MenuToggleState int

const (
	Off MenuToggleState = 0
	On  MenuToggleState = 1
)

func (state MenuToggleState) Indeterminate() bool {
	return state != Off && state != On
}

type MenuEventHandler func(eventID MenuEventID, data any, timestamp uint32) error

type MenuItemProp func(*menuItemProps)

type menuItemProps struct {
	*MenuItem
	dirty []string
	errs  []error
}

func (item *menuItemProps) mark(change string) {
	if !slices.Contains(item.dirty, change) {
		item.dirty = append(item.dirty, change)
	}
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
		item.mark("label")
	}
}

func MenuItemEnabled(enabled bool) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["enabled"] = enabled
		item.mark("enabled")
	}
}

func MenuItemVisible(visible bool) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["visible"] = visible
		item.mark("visible")
	}
}

func MenuItemIconName(name string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["icon-name"] = name
		item.mark("icon-name")
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
		item.mark("icon-data")
	}
}

func MenuItemShortcut(shortcut [][]string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["shortcut"] = shortcut
		item.mark("shortcut")
	}
}

func MenuItemToggleType(t MenuToggleType) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["toggle-type"] = t
		item.mark("toggle-type")
	}
}

func MenuItemToggleState(state MenuToggleState) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["toggle-state"] = state
		item.mark("toggle-state")
	}
}

func MenuItemVendorProp(vendor, prop string, value any) MenuItemProp {
	return func(item *menuItemProps) {
		name := vendorPropName(vendor, prop)
		item.props[name] = value
		item.mark(name)
	}
}

func MenuItemHandler(handler MenuEventHandler) MenuItemProp {
	return func(item *menuItemProps) {
		item.handler = handler
	}
}

func vendorPropName(vendor, prop string) string {
	return fmt.Sprintf("x-%v-%v", vendor, prop)
}

func ClickedHandler(handler func(data any, timestamp uint32) error) MenuEventHandler {
	return func(eventID MenuEventID, data any, timestamp uint32) error {
		if eventID == Clicked {
			return handler(data, timestamp)
		}
		return nil
	}
}
