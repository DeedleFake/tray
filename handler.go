package tray

import "github.com/godbus/dbus/v5"

type Handler interface {
	ContextMenu(x, y int) error
	Activate(x, y int) error
	SecondaryActivate(x, y int) error
	Scroll(delta int, orientation Orientation) error
}

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
