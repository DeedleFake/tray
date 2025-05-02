package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"
	"sync"

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
	item, err := tray.New(
		tray.ItemID("dev.deedles.tray.examples.simple"),
		tray.ItemTitle("Simple Example"),
		tray.ItemIconPixmap(icon),
		tray.ItemToolTip("", nil, "Simple Example", "A simple example of a tray icon."),
		tray.ItemHandler(tray.ActivateHandler(onTrayActivate)),
	)
	if err != nil {
		panic(err)
	}
	defer item.Close()

	group, _ := item.Menu().AddItem(
		tray.MenuItemLabel("Edit"),
	)

	var m sync.Mutex
	var p, add, remove *tray.MenuItem
	props := []tray.MenuItemProp{
		tray.MenuItemLabel("Print"),
		tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
			fmt.Println("Print clicked.")
			return nil
		})),
	}

	add, _ = group.AddItem(
		tray.MenuItemLabel("Add"),
		tray.MenuItemEnabled(false),
		tray.MenuItemHandler(tray.ClickedHandler(func(data any, timestamp uint32) error {
			m.Lock()
			defer m.Unlock()

			p, _ = item.Menu().AddItem(props...)
			add.SetProps(tray.MenuItemEnabled(false))
			remove.SetProps(tray.MenuItemEnabled(true))
			return nil
		})),
	)

	remove, _ = group.AddItem(
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

	item.Menu().AddItem(tray.MenuItemType(tray.Separator))

	m.Lock()
	p, _ = item.Menu().AddItem(props...)
	m.Unlock()

	select {}
}
