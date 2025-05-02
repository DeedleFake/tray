package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"

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

	menu := item.Menu()

	mitem := menu.AddItem()
	mitem.SetLabel("First")
	mitem.SetHandler(func(event tray.MenuEventID, data any, timestamp uint32) error {
		if event == tray.Clicked {
			fmt.Println("First clicked.")
		}
		return nil
	})

	mitem = menu.AddItem()
	mitem.SetType(tray.Separator)

	mitem = menu.AddItem()
	mitem.SetLabel("Second")
	mitem.SetHandler(func(event tray.MenuEventID, data any, timestamp uint32) error {
		if event == tray.Clicked {
			fmt.Println("Second clicked.")
		}
		return nil
	})

	select {}
}
