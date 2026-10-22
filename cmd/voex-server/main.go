package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"voex-server/internal/app"
	"voex-server/internal/config"
	"voex-server/internal/database"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	command := "serve"
	if len(args) > 1 {
		return fmt.Errorf("usage: voex-server [serve|migrate|schema]")
	}
	if len(args) == 1 {
		command = args[0]
	}
	switch command {
	case "serve":
		return app.Run()
	case "migrate":
		if err := database.Migrate(config.Read().Database); err != nil {
			return fmt.Errorf("schema migration failed: %w", err)
		}
		log.Printf("schema migrated to %s", database.LatestVersion)
		return nil
	case "schema":
		status, err := database.Inspect(config.Read().Database)
		if encodeErr := json.NewEncoder(os.Stdout).Encode(status); encodeErr != nil {
			return encodeErr
		}
		return err
	default:
		return fmt.Errorf("unknown command %q; usage: voex-server [serve|migrate|schema]", command)
	}
}
