package tray

import "github.com/godbus/dbus/v5"

// Handler specifies behavior for incoming events for a
// StatusNotifierItem. In most cases, Activate is the only method of
// interest, as that's the one that's called when an icon is clicked
// or double-clicked. For that common case, see [ActivateHandler].
//
// Errors returned by the methods of a Handler are sent over D-Bus as
// a response to the call that resulted in the Handler being invoked
// in the first place. In a lot of cases, this won't do much besides
// possibly showing up in a log somewhere, so it's probably best in
// those cases to handle it locally some other way, if appropriate,
// and then just return nil regardless.
type Handler interface {
	ContextMenu(x, y int) error
	Activate(x, y int) error
	SecondaryActivate(x, y int) error
	Scroll(delta int, orientation Orientation) error
}

// ActiveHandler is a convenience type for the common case of only
// needing to implement the Activate method of [Handler]. It calls
// itself when Activate is called and does nothing for all other
// methods.
type ActivateHandler func(x, y int) error

func (h ActivateHandler) ContextMenu(x, y int) error                      { return nil }
func (h ActivateHandler) Activate(x, y int) error                         { return h(x, y) }
func (h ActivateHandler) SecondaryActivate(x, y int) error                { return nil }
func (h ActivateHandler) Scroll(delta int, orientation Orientation) error { return nil }

type statusNotifierItem Item

func (item *statusNotifierItem) Handler() Handler {
	return (*Item)(item).Handler()
}

func (item *statusNotifierItem) ContextMenu(x, y int) *dbus.Error {
	logger.Info("item method", "name", "ContextMenu", "x", x, "y", y)

	handler := item.Handler()
	if handler == nil {
		return nil
	}
	err := handler.ContextMenu(x, y)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (item *statusNotifierItem) Activate(x, y int) *dbus.Error {
	logger.Info("item method", "name", "Activate", "x", x, "y", y)

	handler := item.Handler()
	if handler == nil {
		return nil
	}
	err := handler.Activate(x, y)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (item *statusNotifierItem) SecondaryActivate(x, y int) *dbus.Error {
	logger.Info("item method", "name", "SecondaryActivate", "x", x, "y", y)

	handler := item.Handler()
	if handler == nil {
		return nil
	}
	err := handler.SecondaryActivate(x, y)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (item *statusNotifierItem) Scroll(delta int, orientation Orientation) *dbus.Error {
	logger.Info("item method", "name", "Scroll", "delta", delta, "orientation", orientation)

	handler := item.Handler()
	if handler == nil {
		return nil
	}
	err := handler.Scroll(delta, orientation)
	if err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}
