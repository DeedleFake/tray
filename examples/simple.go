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

	item.Menu().AddItem(
		tray.MenuItemLabel("First"),
		tray.MenuItemHandler(func(event tray.MenuEventID, data any, timestamp uint32) error {
			if event == tray.Clicked {
				fmt.Println("First clicked.")
			}
			return nil
		}),
	)

	item.Menu().AddItem(
		tray.MenuItemType(tray.Separator),
	)

	item.Menu().AddItem(
		tray.MenuItemLabel("Second"),
		tray.MenuItemHandler(func(event tray.MenuEventID, data any, timestamp uint32) error {
			if event == tray.Clicked {
				fmt.Println("Second clicked.")
			}
			return nil
		}),
	)

	select {}
}
