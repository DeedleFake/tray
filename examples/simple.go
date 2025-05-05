package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"
	"sync"
	"time"

	"deedles.dev/tray"
)

var (
	//go:embed icon.png
	iconData []byte

	icon image.Image
)

func init() {
	img, _, err := image.Decode(bytes.NewReader(iconData))
	if err != nil {
		panic(err)
	}
	icon = img
}

func onTrayActivate(x, y int) error {
	fmt.Println("Activated.")
	return nil
}

func main() {
	done := make(chan struct{})

	item, err := tray.New(
		tray.ItemID("dev.deedles.tray.examples.simple"),
		tray.ItemTitle("Simple Example"),
		tray.ItemIconPixmap(icon),
		tray.ItemToolTip("", nil, "Simple Example", "A simple example of a tray icon."),
		tray.ItemIsMenu(true),
		tray.ItemHandler(tray.ActivateHandler(onTrayActivate)),
	)
	if err != nil {
		panic(err)
	}
	defer item.Close()

	group, _ := item.Menu().AddChild(
		tray.MenuItemLabel("Edit"),
	)

	var m sync.Mutex
	var p, add, remove, quit *tray.MenuItem
	props := []tray.MenuItemProp{
		tray.MenuItemLabel("Print"),
		tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
			fmt.Println("Print clicked.")
			return nil
		})),
	}

	add, _ = group.AddChild(
		tray.MenuItemLabel("Add"),
		tray.MenuItemEnabled(false),
		tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
			m.Lock()
			defer m.Unlock()

			p, _ = item.Menu().AddChild(props...)
			p.MoveBefore(quit)
			add.SetProps(tray.MenuItemEnabled(false))
			remove.SetProps(tray.MenuItemEnabled(true))
			return nil
		})),
	)

	remove, _ = group.AddChild(
		tray.MenuItemLabel("Remove"),
		tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
			m.Lock()
			defer m.Unlock()

			p.Remove()
			add.SetProps(tray.MenuItemEnabled(true))
			remove.SetProps(tray.MenuItemEnabled(false))
			return nil
		})),
	)

	item.Menu().AddChild(tray.MenuItemType(tray.Separator))

	m.Lock()

	p, _ = item.Menu().AddChild(props...)

	quit, _ = item.Menu().AddChild(
		tray.MenuItemLabel("Quit"),
		tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
			close(done)
			return nil
		})),
	)

	m.Unlock()

	time.AfterFunc(5*time.Second, func() {
		m.Lock()
		defer m.Unlock()

		quit.SetProps(tray.MenuItemLabel("Exit"))
	})

	<-done
}
