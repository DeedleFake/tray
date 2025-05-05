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

// MenuItem is a single item in the menu of the tray item. The item
// can be a regular single item or can be a sub-menu containing more
// items, recursively.
//
// MenuItems are created by calling the AddChild method on either the
// [Menu] or on another MenuItem.
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

// AddChild creates a new MenuItem with the given properties and
// appends it as the last child of the root of the menu hierarchy.
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

// AddChild creates a new MenuItem with the given properties and
// append it as the last child of the MenuItem that it is called on.
// If the receiver MenuItem has no children before this, it will
// automatically be converted into a sub-menu MenuItem.
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

// Remove removes item from the menu hierarchy completely. If its
// parent is another MenuItem and item is its only child, the parent
// is converted from a sub-menu item back into a regular one.
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

// MoveBefore makes item the previous sibling of sibling. If
// necessary, this method will transfer item from its current parent
// to sibling's parent.
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

// RequestActivation sends a request to the environment that item be
// activated. What exactly this means is dependent on the environment
// and situation.
func (item *MenuItem) RequestActivation(timestamp uint32) error {
	return item.menu.item.conn.Emit(
		menuPath,
		"com.canonical.dbusmenu.ItemActivationRequested",
		item.id,
		timestamp,
	)
}

// Type returns the current value of the item's "type" property,
func (item *MenuItem) Type() MenuType {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "type", MenuType("standard"))
}

// Label returns the current value of the item's "label" property.
// This is the text that should be displayed by the environment for
// the item in the menu.
func (item *MenuItem) Label() string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "label", "")
}

// Enabled returns the current value of the item's "enabled" property.
func (item *MenuItem) Enabled() bool {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "enabled", true)
}

// Visible returns the current value of the item's "visible" property.
func (item *MenuItem) Visible() bool {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "visible", true)
}

// IconName returns the current value of the item's "icon-name"
// property. This is one of two properties that determine what icon
// should be displayed in the menu by the environment, the other being
// "icon-data".
func (item *MenuItem) IconName() string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "icon-name", "")
}

// IconData returns the current value of the item's "icon-data"
// property. This is one of two properties that determine what icon
// should be displayed in the menu by the environment, the other being
// "icon-name".
func (item *MenuItem) IconData() (image.Image, error) {
	item.m.RLock()
	defer item.m.RUnlock()

	data, ok := item.props["icon-data"].([]byte)
	if !ok {
		return nil, nil
	}
	return png.Decode(bytes.NewReader(data))
}

// Shortcut returns the current value of the item's "shortcut"
// property. This is used by the environment to determine keyboard
// shortcuts and is of the form
//
//	[][]string{
//		[]string{"Control", "Alt", "e"},
//	}
func (item *MenuItem) Shortcut() [][]string {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "shortcut", [][]string(nil))
}

// ToggleType returns the current value of the item's "toggle-type"
// property.
func (item *MenuItem) ToggleType() MenuToggleType {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "toggle-type", MenuToggleType(""))
}

// ToggleState returns the current value of the item's "toggle-state"
// property.
func (item *MenuItem) ToggleState() MenuToggleState {
	item.m.RLock()
	defer item.m.RUnlock()

	return mapLookup(item.props, "toggle-state", MenuToggleState(-1))
}

// VendorProp returns the current value of the vendor-specific custom
// property with the given vendor and property name. It returns false
// as its second return if no such property exists.
func (item *MenuItem) VendorProp(vendor, prop string) (any, bool) {
	item.m.RLock()
	defer item.m.RUnlock()

	v, ok := item.props[vendorPropName(vendor, prop)]
	return v, ok
}

// SetProps sets all of the given properties on the item.
func (item *MenuItem) SetProps(props ...MenuItemProp) error {
	defer item.lock()()

	dirty, errs := item.applyProps(props)
	errs = append(errs, item.emitPropertiesUpdated(dirty))

	defer item.menu.lock()()
	errs = append(errs, item.menu.updateLayout(item))

	return errors.Join(errs...)
}

// MenuType is the possible values of the type of a menu item.
type MenuType string

const (
	Standard  MenuType = "standard"
	Separator MenuType = "separator"
)

// MenuToggleType is the types of togglability that a menu item can
// have.
type MenuToggleType string

const (
	NonToggleable MenuToggleType = ""
	Checkmark     MenuToggleType = "checkmark"
	Radio         MenuToggleType = "radio"
)

// MenuToggleState is the two main states that a togglable menu item can be in. All values other than [On] and [Off] are considered to be indeterminate.
type MenuToggleState int

const (
	Off MenuToggleState = 0
	On  MenuToggleState = 1
)

// Indeterminate returns true if state represents an indeterminate state other than [On] and [Off].
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

// MenuItemType sets a MenuItem's "type" property. See
// [MenuItem.Type].
func MenuItemType(t MenuType) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["type"] = t
	}
}

// MenuItemLabel sets a MenuItem's "label" property. See
// [MenuItem.Label].
func MenuItemLabel(label string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["label"] = label
		item.mark("label")
	}
}

// MenuItemEnabled sets a MenuItem's "enabled" property. See
// [MenuItem.Enabled].
func MenuItemEnabled(enabled bool) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["enabled"] = enabled
		item.mark("enabled")
	}
}

// MenuItemVisible sets a MenuItem's "visible" property. See
// [MenuItem.Visible].
func MenuItemVisible(visible bool) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["visible"] = visible
		item.mark("visible")
	}
}

// MenuItemIconName sets a MenuItem's "icon-name" property. See
// [MenuItem.IconName].
func MenuItemIconName(name string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["icon-name"] = name
		item.mark("icon-name")
	}
}

// MenuItemIconData sets a MenuItem's "icon-data" property. See
// [MenuItem.IconData].
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

// MenuItemShortcut sets a MenuItem's "shortcut" property. See
// [MenuItem.Shortcut].
func MenuItemShortcut(shortcut [][]string) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["shortcut"] = shortcut
		item.mark("shortcut")
	}
}

// MenuItemToggleType sets a MenuItem's "toggle-type" property. See
// [MenuItem.ToggleType].
func MenuItemToggleType(t MenuToggleType) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["toggle-type"] = t
		item.mark("toggle-type")
	}
}

// MenuItemToggleState sets a MenuItem's "toggle-state" property. See
// [MenuItem.ToggleState].
func MenuItemToggleState(state MenuToggleState) MenuItemProp {
	return func(item *menuItemProps) {
		item.props["toggle-state"] = state
		item.mark("toggle-state")
	}
}

// MenuItemVendorProp sets a vendor-specific custom property with the
// given vendor and property name. See [MenuItem.VendorProp].
func MenuItemVendorProp(vendor, prop string, value any) MenuItemProp {
	return func(item *menuItemProps) {
		name := vendorPropName(vendor, prop)
		item.props[name] = value
		item.mark(name)
	}
}

// MenuItemHandler sets the event handler for a MenuItem.
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
