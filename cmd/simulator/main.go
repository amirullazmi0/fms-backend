// simulator: fake FMC150 devices for development and demos (see pkg/simulator).
// The backend can also run them itself: fms-administrator → "Jalankan simulasi".
//
//	go run ./cmd/simulator [devices=2] [host=127.0.0.1] [port=DEVICE_PORT] [intervalSeconds=3]
//
// devices = all units: 1 EX per 5, 1 LV and 1 SUPPORT per 10, the rest DT (simulator.Mix).
// SIM_LOOP=1: unassigned trucks loop around central Jakarta instead of parking.
// SIM_BLANKSPOT=1: signal only near the loading site, data arrives late in bulk.
// SIM_QUEUE=1: trucks queue before a busy loading / dumping spot.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/pkg/database"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/simulator"

	_ "github.com/joho/godotenv/autoload"
)

func main() {
	args := os.Args[1:]
	o := simulator.Options{Devices: 2, Loop: os.Getenv("SIM_LOOP") == "1",
		BlankSpot: os.Getenv("SIM_BLANKSPOT") == "1", Queue: os.Getenv("SIM_QUEUE") == "1"}
	if len(args) > 0 {
		if n, err := strconv.Atoi(args[0]); err == nil && n > 0 {
			o.Devices = n
		}
	}
	o.Excavators, o.LightVehicles, o.Supports = simulator.Mix(o.Devices)
	cfg, cfgErr := config.LoadConfig()
	if cfgErr == nil {
		o.Port, o.DataDir, o.OverpassURL = cfg.Device.Port, cfg.App.DataDir, cfg.Routing.OverpassURL
	}
	if len(args) > 1 {
		o.Host = args[1]
	}
	if len(args) > 2 {
		o.Port = args[2]
	}
	if len(args) > 3 {
		if n, err := strconv.Atoi(args[3]); err == nil && n > 0 {
			o.Interval = n
		}
	}
	_ = logger.InitLogger(logger.LoggerConfig{Format: "console"})

	if cfgErr == nil {
		if db, err := database.NewDatabase(&cfg.Database); err == nil {
			o.DB = db.DB
		} else {
			fmt.Printf("Database tidak terjangkau (%v): rute dari dashboard tidak dibaca\n", err)
		}
	} else {
		fmt.Println("Config tidak terbaca: rute dari dashboard tidak dibaca")
	}
	_ = simulator.Run(context.Background(), o)
}
