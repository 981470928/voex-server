package main

import (
	"log"
	"os"
	"voex-server/internal/updater"
)

func main() {
	if err := updater.Run(); err != nil {
		log.Print("updater stopped: ", err)
		os.Exit(1)
	}
}
