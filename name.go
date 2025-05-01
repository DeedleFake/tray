package tray

import (
	"fmt"
	"os"
	"sync/atomic"

	"github.com/godbus/dbus/v5"
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
