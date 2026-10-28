package tray

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"deedles.dev/tray/internal/set"
	"github.com/godbus/dbus/v5"
)

// MenuEventHandler is a function that is called in response to events
// on both the [Menu] and on [MenuItem]. It is given a MenuEventID
// that identifies the type of event, such as [Clicked], arbitrary
// data that is associated with the event, and a timestamp. The
// timestamp corresponds to an internal time value of the sender and
// is not a specific absolute point in time.
//
// The error returned by the handler is sent as a response to the
// D-Bus call that triggered it, so in a lot of cases it is more
// useful to handle an error locally in some way and then return nil
// from the handler regardless.
//
// For the simple common case of handling specifically [Clicked]
// events, see [ClickedHandler].
type MenuEventHandler func(eventID MenuEventID, data any, timestamp uint32) error

// ClickedHandler is a convenience function that returns a
// MenuEventHandler that calls handler if and only if the event ID is
// [Clicked].
func ClickedHandler(handler func(data any, timestamp uint32) error) MenuEventHandler {
	return func(eventID MenuEventID, data any, timestamp uint32) error {
		if eventID == Clicked {
			return handler(data, timestamp)
		}
		return nil
	}
}

type dbusmenu Menu

// godbus writes a method's reply only after the method returns, which is
// after it has released menu.m. A SetProps in that gap emits a newer
// ItemsPropertiesUpdated that reaches the host first, and the host then
// applies the stale reply over it. So the methods whose replies carry item
// properties write the reply themselves while they still hold menu.m, and
// claimMenuReplies stops godbus from sending a second one.

// flagReplyOwed is a flag bit the D-Bus spec leaves unused. It is only
// ever set in memory, on a call that claimMenuReplies also marked
// FlagNoReplyExpected, so godbus never answers it on success.
const flagReplyOwed dbus.Flags = 0x80

// replyingMethods is every method that takes a dbus.Message. A method
// needs that parameter to address its reply, so the parameter is the
// opt-in, and there is no second list to keep in sync.
var replyingMethods = methodsTaking(reflect.TypeFor[*dbusmenu](), reflect.TypeFor[dbus.Message]())

func methodsTaking(t, param reflect.Type) set.Set[string] {
	s := make(set.Set[string])
	for m := range t.Methods() {
		for in := range m.Type.Ins() {
			if in == param {
				s.Add(m.Name)
				break
			}
		}
	}
	return s
}

// claimMenuReplies is the connection's incoming interceptor. godbus runs
// it on the reader goroutine before dispatching the call, so it must not
// take any menu lock.
func claimMenuReplies(msg *dbus.Message) {
	if msg.Type != dbus.TypeMethodCall || msg.Flags&dbus.FlagNoReplyExpected != 0 {
		return
	}
	path, _ := msg.Headers[dbus.FieldPath].Value().(dbus.ObjectPath)
	if path != menuPath {
		return
	}
	if iface, ok := msg.Headers[dbus.FieldInterface].Value().(string); ok && iface != menuInter {
		return
	}
	member, _ := msg.Headers[dbus.FieldMember].Value().(string)
	if !replyingMethods.Contains(member) {
		return
	}
	msg.Flags |= dbus.FlagNoReplyExpected | flagReplyOwed
}

// reply sends v as the reply to call and returns it, so that the reply
// body is always exactly what the method returns. The caller must hold
// menu.m.
func reply[T any](menu *dbusmenu, call dbus.Message, v T) (T, *dbus.Error) {
	menu.send(call, v)
	return v, nil
}

func reply2[T, U any](menu *dbusmenu, call dbus.Message, v T, u U) (T, U, *dbus.Error) {
	menu.send(call, v, u)
	return v, u, nil
}

// send builds the reply the same way godbus's handleCall does. Errors
// stay with godbus, which sends them regardless of the flags.
func (menu *dbusmenu) send(call dbus.Message, body ...any) {
	if call.Flags&flagReplyOwed == 0 {
		return
	}
	h := map[dbus.HeaderField]dbus.Variant{
		dbus.FieldReplySerial: dbus.MakeVariant(call.Serial()),
		dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(body...)),
	}
	if s, ok := call.Headers[dbus.FieldSender]; ok {
		h[dbus.FieldDestination] = s
	}
	c := menu.item.conn.Send(&dbus.Message{Type: dbus.TypeMethodReply, Headers: h, Body: body}, nil)
	if c.Err != nil {
		logger.Error("write menu reply", "err", c.Err)
	}
}

func (menu *dbusmenu) buildLayout(item *MenuItem, depth int, props []string) menuLayout {
	if item == nil {
		return menuLayout{
			ID:         0,
			Properties: map[string]any{"children-display": "submenu"},
			Children:   menu.buildChildren(item, depth, props),
		}
	}

	item.m.RLock()
	defer item.m.RUnlock()

	return menuLayout{
		ID:         item.id,
		Properties: mapSlice(item.props, nil), // Workaround for possible bug in the GNOME extension.
		Children:   menu.buildChildren(item, depth, props),
	}
}

func (menu *dbusmenu) buildChildren(parent *MenuItem, depth int, props []string) []any {
	if depth == 0 { // -1 is infinite, so check is exactly to 0.
		return nil
	}

	var parentID int
	ids := menu.children
	if parent != nil {
		parentID = parent.id
		ids = parent.children
	}

	menu.dirty.Remove(parentID)

	children := make([]any, 0, len(ids))
	for _, id := range ids {
		child := menu.nodes[id]
		if child != nil {
			children = append(children, menu.buildLayout(child, depth-1, props))
		}
	}

	return children
}

func (menu *dbusmenu) GetLayout(call dbus.Message, parentID int, recursionDepth int, propertyNames []string) (uint32, menuLayout, *dbus.Error) {
	logger.Info("menu method", "name", "GetLayout", "parentID", parentID, "propertyNames", propertyNames)

	// TODO: Find a way to do this without locking it.
	menu.m.Lock()
	defer menu.m.Unlock()

	var parent *MenuItem
	if parentID != 0 {
		parent = menu.nodes[parentID]
		if parent == nil {
			return menu.revision, menuLayout{}, dbus.MakeFailedError(fmt.Errorf("unknown node with ID %v", parentID))
		}
	}

	return reply2(menu, call, menu.revision, menu.buildLayout(parent, recursionDepth, propertyNames))
}

func (menu *dbusmenu) GetGroupProperties(call dbus.Message, ids []int, propertyNames []string) ([]menuProps, *dbus.Error) {
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
			ID:         item.id,
			Properties: mapSlice(item.props, nil), // Workaround for possible bug in the GNOME extension.
		})
		item.m.RUnlock()
	}
	return reply(menu, call, r)
}

func (menu *dbusmenu) GetProperty(call dbus.Message, id int, name string) (any, *dbus.Error) {
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

	return reply(menu, call, v)
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

	menu.m.RLock()
	defer menu.m.RUnlock()

	return menu.dirty.Contains(id), nil
}

func (menu *dbusmenu) AboutToShowGroup(ids []int) (updates []menuUpdate, invalid []int, derr *dbus.Error) {
	logger.Info("menu method", "name", "AboutToShowGroup", "ids", ids)

	menu.m.RLock()
	defer menu.m.RUnlock()

	for _, id := range ids {
		item := menu.nodes[id]
		if item == nil {
			invalid = append(invalid, id)
			continue
		}

		updates = append(updates, menuUpdate{
			ID:         id,
			NeedUpdate: menu.dirty.Contains(id),
		})
	}

	return updates, invalid, nil
}

// TextDirection represents the possible configurations a menu's text
// direction.
type TextDirection string

const (
	LeftToRight TextDirection = "ltr"
	RightToLeft TextDirection = "rtl"
)

// MenuStatus is the possible statuses of a menu.
type MenuStatus string

const (
	Normal MenuStatus = "normal"
	Notice MenuStatus = "notice"
)

// MenuEventID is an identifier for the type of event that is being
// sent to a menu's event handler. The values defined in this package
// are not the only possible values. In particular, the spec defines a
// format for vendor-specific custom values. These can be parsed with
// the [ParseVendor] method.
type MenuEventID string

const (
	Clicked MenuEventID = "clicked"
	Hovered MenuEventID = "hovered"
	Opened  MenuEventID = "opened"
	Closed  MenuEventID = "closed"
)

// ParseVendor parses a vendor-specific custom event ID. If id
// contains such a value, it returns the vendor and event names as
// parsed from it. If it does not contain such a value, the returned
// bool will be false.
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
