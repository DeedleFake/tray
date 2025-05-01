package main

import "deedles.dev/tray"

func main() {
	item, err := tray.New()
	if err != nil {
		panic(err)
	}
	defer item.Close()

	err = item.Register()
	if err != nil {
		panic(err)
	}

	select {}
}
