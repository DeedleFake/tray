package tray

import (
	"fmt"
	"os"
	"slices"
	"sync"
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

type lazy[T any] struct {
	m  sync.RWMutex
	v  T
	ok bool
}

func (lazy *lazy[T]) Get(create func() (T, error)) (T, error) {
	lazy.m.RLock()
	if lazy.ok {
		v := lazy.v
		lazy.m.RUnlock()
		return v, nil
	}
	lazy.m.RUnlock()

	lazy.m.Lock()
	defer lazy.m.Unlock()

	if lazy.ok {
		return lazy.v, nil
	}

	v, err := create()
	if err != nil {
		return v, err
	}

	lazy.v = v
	lazy.ok = true

	return v, nil
}

func (lazy *lazy[T]) Clear() {
	lazy.m.Lock()
	defer lazy.m.Unlock()

	var zero T
	lazy.v = zero
	lazy.ok = false
}
