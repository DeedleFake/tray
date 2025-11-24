// Package tray is an implementation of StatusNotifierItem.
//
// Package tray provides a pure Go implementation of the
// StatusNotifierItem D-Bus interface. This can be used to create
// system tray icons and menus in most Linux desktop environments.
package tray

import (
	"errors"
	"log/slog"
	"maps"
	"os"
	"slices"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

var logger = slog.With("TRAY_DEBUG", 1)

func init() {
	if os.Getenv("TRAY_DEBUG") != "1" {
		logger = slog.New(slog.DiscardHandler)
	}
}

func dbusCall(obj dbus.BusObject, method string, flags dbus.Flags, args ...any) *dbus.Call {
	call := obj.Call(method, flags, args...)
	if call.Err != nil {
		errName := dbusErrorName(call.Err)
		logger.Warn(
			"dbus call failed",
			"destination", call.Destination,
			"path", call.Path,
			"method", call.Method,
			"args", call.Args,
			"errName", errName,
			"err", call.Err,
		)
	}
	return call
}

func dbusErrorName(err error) string {
	var dbusError dbus.Error
	if errors.As(err, &dbusError) {
		return dbusError.Name
	}
	return "<not applicable>"
}

func makeProp[T any](v T) *prop.Prop {
	return &prop.Prop{
		Value: v,
		Emit:  prop.EmitTrue,
	}
}

func makeConstProp[T any](v T) *prop.Prop {
	p := makeProp(v)
	p.Emit = prop.EmitConst
	return p
}

func mapSlice[K comparable, V any, M ~map[K]V](m M, s []K) M {
	if len(s) == 0 {
		return maps.Clone(m)
	}

	m2 := make(M, len(s))
	for _, k := range s {
		v, ok := m[k]
		if ok {
			m2[k] = v
		}
	}
	return m2
}

func mapLookup[K comparable, V any](m map[K]any, k K, d V) V {
	v, ok := m[k].(V)
	if !ok {
		return d
	}
	return v
}

func sliceRemove[S ~[]T, T comparable](s S, v T) S {
	i := slices.Index(s, v)
	if i < 0 {
		return s
	}
	return slices.Delete(s, i, i+1)
}
