// Package tray is an implementation of StatusNotifierItem.
//
// Package tray provides a pure Go implementation of the
// StatusNotifierItem D-Bus interface. This can be used to create
// system tray icons and menus in most Linux desktop environments.
package tray

import (
	"encoding/binary"
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
		logger.Warn(
			"dbus call failed",
			"destination", call.Destination,
			"path", call.Path,
			"method", call.Method,
			"args", call.Args,
			"err", call.Err,
		)
	}
	return call
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

type formatARGB32 struct{}

var argb32 formatARGB32

func (formatARGB32) String() string { return "ARGB8888" }

func (formatARGB32) Size() int { return 4 }

func (formatARGB32) Read(data []byte) (r, g, b, a uint32) {
	n := binary.BigEndian.Uint32(data)
	a = (n >> 24 * 0xFFFF / 0xFF)
	r = (n >> 16 & 0xFF) * a / 0xFF
	g = (n >> 8 & 0xFF) * a / 0xFF
	b = (n & 0xFF) * a / 0xFF
	return
}

func (formatARGB32) Write(buf []byte, r, g, b, a uint32) {
	if a == 0 {
		copy(buf, []byte{0, 0, 0, 0})
		return
	}

	r = (r * 0xFF / a) << 16
	g = (g * 0xFF / a) << 8
	b = b * 0xFF / a
	a = (a * 0xFF / 0xFFFF) << 24
	binary.BigEndian.PutUint32(buf, r|g|b|a)
}
