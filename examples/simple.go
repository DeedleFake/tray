package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	_ "image/png"

	"deedles.dev/tray"
)

//go:embed icon.png
var icon []byte

func main() {
	item, err := tray.New()
	if err != nil {
		panic(err)
	}
	defer item.Close()

	img, _, err := image.Decode(bytes.NewReader(icon))
	if err != nil {
		panic(err)
	}

	item.SetID("dev.deedles.tray.examples.simple")
	item.SetTitle("Simple Example")
	item.SetIconPixmap(img)
	item.SetToolTip("", nil, "Simple Example", "A simple example of a tray icon.")
	item.SetHandler(tray.ActivateHandler(func(x, y int) error {
		fmt.Println("Activated.")
		return nil
	}))

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
