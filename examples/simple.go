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

	item.SetID("simple")
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

	select {}
}
