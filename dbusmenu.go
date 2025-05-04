package tray

import (
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/godbus/dbus/v5"
)

type MenuEventHandler func(eventID MenuEventID, data any, timestamp uint32) error

func ClickedHandler(handler func(data any, timestamp uint32) error) MenuEventHandler {
	return func(eventID MenuEventID, data any, timestamp uint32) error {
		if eventID == Clicked {
			return handler(data, timestamp)
		}
		return nil
	}
}

type dbusmenu Menu

func (menu *dbusmenu) buildLayout(item *MenuItem, depth int, props []string) menuLayout {
	var id int
	properties := map[string]any{"children-display": "submenu"}
	if item != nil {
		item.m.RLock()
		defer item.m.RUnlock()

		id = item.id
		//properties = mapSlice(item.props, props)
		properties = item.props
		// This is only supposed to send back the properties requested,
		// but for some reason doing so causes things to not update
		// correctly in GNOME. This is quite probably a bug in the
		// StatusNotiferHost implementation and it's not asking for the
		// correct properties based on other signals, or I'm not
		// understanding something about the protocol, but just simply
		// sending everything every time fixes it and that's what other
		// implementations seem to do, so...
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
		child := menu.nodes[id]
		if child != nil {
			children = append(children, menu.buildLayout(child, depth-1, props))
		}
	}

	return children
}

func (menu *dbusmenu) GetLayout(parentID int, recursionDepth int, propertyNames []string) (revision uint32, layout menuLayout, derr *dbus.Error) {
	logger.Info("menu method", "name", "GetLayout", "parentID", parentID, "propertyNames", propertyNames)

	menu.m.RLock()
	defer menu.m.RUnlock()

	layout = menu.buildLayout(nil, recursionDepth, propertyNames)
	return menu.revision, layout, nil
}

func (menu *dbusmenu) GetGroupProperties(ids []int, propertyNames []string) ([]menuProps, *dbus.Error) {
	logger.Info("menu method", "name", "GetGroupProperties", "ids", ids, "propertyNames", propertyNames)

	menu.m.RLock()
	defer menu.m.RUnlock()

	items := maps.Values(menu.nodes)
	if len(ids) != 0 {
		items = func(yield func(*MenuItem) bool) {
			for _, id := range ids {
				item := menu.nodes[id]
				if item != nil && !yield(item) {
					return
				}
			}
		}
	}

	var r []menuProps
	for item := range items {
		item.m.RLock()
		r = append(r, menuProps{
			ID: item.id,
			//Properties: mapSlice(item.props, propertyNames),
			Properties: item.props, // See buildLayout().
		})
		item.m.RUnlock()
	}
	return r, nil
}

func (menu *dbusmenu) GetProperty(id int, name string) (any, *dbus.Error) {
	logger.Info("menu method", "name", "GetProperty", "id", id, "name", name)

	menu.m.RLock()
	defer menu.m.RUnlock()

	item := menu.nodes[id]
	if item == nil {
		return nil, dbus.MakeFailedError(fmt.Errorf("menu item with ID %v not found", id))
	}

	item.m.RLock()
	defer item.m.RUnlock()

	v := item.props[name]
	if v == nil {
		return nil, dbus.MakeFailedError(fmt.Errorf("property %q not found", name))
	}

	return v, nil
}

func (menu *dbusmenu) getHandler(id int) MenuEventHandler {
	menu.m.RLock()
	defer menu.m.RUnlock()

	if id == 0 {
		return menu.handler
	}

	item := menu.nodes[id]
	if item == nil {
		return nil
	}

	item.m.RLock()
	defer item.m.RUnlock()

	return item.handler
}

func (menu *dbusmenu) event(id int, eventID MenuEventID, data dbus.Variant, timestamp uint32) error {
	h := menu.getHandler(id)
	if h == nil {
		return nil
	}

	return h(eventID, data.Value(), timestamp)
}

func (menu *dbusmenu) Event(id int, eventID MenuEventID, data dbus.Variant, timestamp uint32) *dbus.Error {
	logger.Info("menu method", "name", "Event", "id", id, "eventID", eventID, "data", data, "timestamp", timestamp)

	err := menu.event(id, eventID, data, timestamp)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (menu *dbusmenu) EventGroup(events []menuEvent) ([]int, *dbus.Error) {
	logger.Info("menu method", "name", "EventGroup", "events", events)

	ids := make([]int, 0, len(events))
	errs := make([]error, 0, len(events))
	for _, event := range events {
		err := menu.event(event.ID, event.EventID, event.Data, event.Timestamp)
		if err != nil {
			ids = append(ids, event.ID)
			errs = append(errs, err)
		}
	}

	err := errors.Join(errs...)
	if err != nil {
		return ids, dbus.MakeFailedError(err)
	}

	return ids, nil
}

func (menu *dbusmenu) AboutToShow(id int) (bool, *dbus.Error) {
	logger.Info("menu method", "name", "AboutToShow", "id", id)
	return false, nil
}

func (menu *dbusmenu) AboutToShowGroup(ids []int) ([]menuUpdate, []int, *dbus.Error) {
	logger.Info("menu method", "name", "AboutToShowGroup", "ids", ids)
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
	Opened  MenuEventID = "opened"
	Closed  MenuEventID = "closed"
)

func (id MenuEventID) ParseVendor() (vendor, event string, ok bool) {
	e, ok := strings.CutPrefix(string(id), "x-")
	if !ok {
		return "", string(id), false
	}

	vendor, e, ok = strings.Cut(e, "-")
	if !ok {
		return "", string(id), false
	}

	return vendor, e, true
}

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
	Data      dbus.Variant
	Timestamp uint32
}

type menuUpdate struct {
	ID         int
	NeedUpdate bool
}
