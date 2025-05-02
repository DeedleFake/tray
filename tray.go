package tray

import (
	"fmt"
	"os"
	"slices"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

var id uint64

func getName(space string) string {
	id := atomic.AddUint64(&id, 1)
	return fmt.Sprintf("org.%v.StatusNotifierItem-%v-%v", space, os.Getpid(), id)
}

func getSpace(conn *dbus.Conn) (string, error) {
	var freedesktopOwned bool
	err := conn.BusObject().Call("NameHasOwner", 0, "org.freedesktop.StatusNotifierWatcher").Store(&freedesktopOwned)
	if err != nil {
		return "", err
	}

	if freedesktopOwned {
		return "freedesktop", nil
	}
	return "kde", nil
}

func endianSwap(data []byte) {
	if len(data)%4 != 0 {
		panic(fmt.Errorf("len(data) %% 4 != 0, len(data) == %v", len(data)))
	}

	for i := 0; i < len(data); i += 4 {
		slices.Reverse(data[i : i+4])
	}
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
