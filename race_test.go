package tray

import (
	"encoding/binary"
	"image"
	"io"
	"net"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// newTestItem returns an Item with exported properties whose D-Bus
// connection writes into a pipe that is drained and discarded.
func newTestItem(t *testing.T) *Item {
	t.Helper()

	local, remote := net.Pipe()
	go io.Copy(io.Discard, remote)

	conn, err := dbus.NewConn(local)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Close()
		remote.Close()
	})

	item := Item{conn: conn}
	err = item.exportProps()
	if err != nil {
		t.Fatal(err)
	}
	return &item
}

// testSetConcurrentWithGet sets a property repeatedly while another
// goroutine encodes the value returned by Get, the way godbus does
// when replying to a client.
func testSetConcurrentWithGet(t *testing.T, name string, set func(image.Image) ItemProp) {
	t.Helper()

	item := newTestItem(t)
	imgs := []image.Image{
		image.NewRGBA(image.Rect(0, 0, 16, 16)),
		image.NewRGBA(image.Rect(0, 0, 16, 16)),
	}
	item.SetProps(set(imgs[0]))

	const n = 1000
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range n {
			item.SetProps(set(imgs[i%2]))
		}
	})
	wg.Go(func() {
		for range n {
			v, err := item.props.Get(itemInter, name)
			if err != nil {
				t.Error(err)
				return
			}
			msg := dbus.Message{
				Type: dbus.TypeSignal,
				Headers: map[dbus.HeaderField]dbus.Variant{
					dbus.FieldPath:      dbus.MakeVariant(itemPath),
					dbus.FieldInterface: dbus.MakeVariant(itemInter),
					dbus.FieldMember:    dbus.MakeVariant("Test"),
					dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(v)),
				},
				Body: []any{v},
			}
			encErr := msg.EncodeTo(io.Discard, binary.LittleEndian)
			if encErr != nil {
				t.Error(encErr)
				return
			}
		}
	})
	wg.Wait()
}

func TestIconPixmapConcurrentWithGet(t *testing.T) {
	testSetConcurrentWithGet(t, "IconPixmap", func(img image.Image) ItemProp { return ItemIconPixmap(img) })
}

func TestOverlayIconPixmapConcurrentWithGet(t *testing.T) {
	testSetConcurrentWithGet(t, "OverlayIconPixmap", func(img image.Image) ItemProp { return ItemOverlayIconPixmap(img) })
}

func TestAttentionIconPixmapConcurrentWithGet(t *testing.T) {
	testSetConcurrentWithGet(t, "AttentionIconPixmap", func(img image.Image) ItemProp { return ItemAttentionIconPixmap(img) })
}

func TestToolTipConcurrentWithGet(t *testing.T) {
	testSetConcurrentWithGet(t, "ToolTip", func(img image.Image) ItemProp { return ItemToolTip("", []image.Image{img}, "", "") })
}

func TestMenuIconThemePathConcurrentWithGet(t *testing.T) {
	item := newTestItem(t)
	menu := newTestMenu(t)
	menu.item = item
	item.menu = menu
	props, err := prop.Export(item.conn, menuPath, menuPropsMap)
	if err != nil {
		t.Fatal(err)
	}
	menu.props = props

	paths := [][]string{{"/a", "/b"}, {"/c", "/d"}}
	item.SetProps(ItemMenuIconThemePath(paths[0]))

	const n = 1000
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range n {
			item.SetProps(ItemMenuIconThemePath(paths[i%2]))
		}
	})
	wg.Go(func() {
		for range n {
			v, err := menu.props.Get(menuInter, "IconThemePath")
			if err != nil {
				t.Error(err)
				return
			}
			for _, s := range v.Value().([]string) {
				_ = len(s)
				_ = s + ""
			}
		}
	})
	wg.Wait()
}
