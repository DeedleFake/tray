package main

import (
	"bytes"
	_ "embed"
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

	item.SetTitle("Simple Example")
	item.SetIconPixmap(img)
	item.SetToolTip("", nil, "Simple Example", "A simple example of a tray icon.")

	select {}
}
