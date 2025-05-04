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

	menu.nodes[item.id] = &item

	return &item
}

func (menu *Menu) AddChild(props ...MenuItemProp) (*MenuItem, error) {
	defer menu.lock()()

	child := menu.newItem(0)
	defer child.lock()()

	dirty, errs := child.applyProps(props)
	errs = append(errs, menu.updateLayout(menu))
	errs = append(errs, child.emitPropertiesUpdated(dirty))

	menu.setChildren(append(menu.children, child.id))

	return child, errors.Join(errs...)
}

func (item *MenuItem) AddChild(props ...MenuItemProp) (*MenuItem, error) {
	defer item.menu.lock()()
	defer item.lock()()

	child := item.menu.newItem(item.id)
	defer child.lock()()

	dirty, errs := child.applyProps(props)
	errs = append(errs, item.menu.updateLayout(item))
	errs = append(errs, item.emitPropertiesUpdated(dirty))

	item.setChildren(append(item.children, child.id))

	return child, errors.Join(errs...)
}

func (item *MenuItem) Remove() error {
	parent := item.getParent()
	if parent == nil {
		return nil
	}
	defer parent.lock()()

	parent.setChildren(sliceRemove(parent.getChildren(), item.id))

	if parent != item.menu {
		defer item.menu.lock()()
	}

	delete(item.menu.nodes, item.id)

	return item.menu.updateLayout(parent)
}

func (item *MenuItem) MoveBefore(sibling *MenuItem) error {
	dst := sibling.getParent()
	src := item.getParent()
	if dst == nil || src == nil {
		// TODO: Allow moving children who have previously been removed?
		return nil
	}

	defer dst.lock()()
	if dst != src {
		defer src.lock()()
	}

	dc := dst.getChildren()
	i := slices.Index(dc, sibling.id)
	if i < 0 {
		i = len(dc)
	}

	src.setChildren(sliceRemove(src.getChildren(), item.id))
	dst.setChildren(slices.Insert(dc, i, item.id))
	item.parent = dst.getID()

	if dst != item.menu && src != item.menu {
		defer item.menu.lock()()
	}

	updates := []menuNode{dst, src}
	if dst == src {
		updates = updates[:1]
	}

	return item.menu.updateLayout(updates...)
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

func (item *MenuItem) RequestActivation(timestamp uint32) error {
	return item.menu.item.conn.Emit(
		menuPath,
		"com.canonical.dbusmenu.ItemActivationRequested",
		item.id,
		timestamp,
	)
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
	defer item.lock()()

	dirty, errs := item.applyProps(props)
	errs = append(errs, item.emitPropertiesUpdated(dirty))

	defer item.menu.lock()()
	errs = append(errs, item.menu.updateLayout(item))

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

func (item *MenuItem) getParent() menuNode {
	if item.parent == 0 {
		return item.menu
	}

	item.menu.m.RLock()
	defer item.menu.m.RUnlock()

	p, ok := item.menu.nodes[item.parent]
	if !ok {
		return nil
	}
	return p
}

func (item *MenuItem) lock() func() {
	item.m.Lock()
	return func() { item.m.Unlock() }
}

func (item *MenuItem) getID() int {
	return item.id
}

func (item *MenuItem) getChildren() []int {
	return item.children
}

func (item *MenuItem) setChildren(c []int) {
	item.children = c
	if len(item.children) == 0 {
		item.props["children-display"] = ""
		return
	}
	item.props["children-display"] = "submenu"
}
