// create-user makes or resets a dashboard account (replaces `yarn user:create`):
//
//	go run ./cmd/create-user <email> <password> [name] [nama role, default ADMIN]
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/database/seeder"
	"github.com/amirullazmi0/fms-backend/pkg/logger"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Pakai: go run ./cmd/create-user <email> <password> [nama] [nama role, default ADMIN]")
		os.Exit(1)
	}
	name, role := "", "ADMIN"
	if len(os.Args) > 3 {
		name = os.Args[3]
	}
	if len(os.Args) > 4 {
		role = os.Args[4]
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Println("Config:", err)
		os.Exit(1)
	}
	_ = logger.InitLogger(logger.LoggerConfig{Format: "console"})
	db, err := database.NewDatabase(&cfg.Database)
	if err != nil {
		fmt.Println("Database:", err)
		os.Exit(1)
	}
	defer db.Close()

	created, err := seeder.UpsertUser(context.Background(), db.DB, os.Args[1], os.Args[2], name, role, true)
	if err != nil {
		fmt.Println("Gagal:", err)
		os.Exit(1)
	}
	// ADMIN accounts are administrators (pass every permission check).
	if strings.EqualFold(role, "ADMIN") {
		if err := seeder.SetAdministrator(context.Background(), db.DB, os.Args[1], true); err != nil {
			fmt.Println("Gagal set administrator:", err)
			os.Exit(1)
		}
	}
	if created {
		fmt.Printf("User %s dibuat (%s)\n", os.Args[1], role)
	} else {
		fmt.Printf("User %s diperbarui (%s)\n", os.Args[1], role)
	}
}
