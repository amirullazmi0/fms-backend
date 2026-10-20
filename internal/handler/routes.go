package handler

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/amirullazmi0/fms-backend/config"
	"github.com/amirullazmi0/fms-backend/internal/middleware"
	"github.com/amirullazmi0/fms-backend/internal/model"
	"github.com/amirullazmi0/fms-backend/internal/repository"
	"github.com/amirullazmi0/fms-backend/internal/scheduler"
	"github.com/amirullazmi0/fms-backend/internal/tracker"
	"github.com/amirullazmi0/fms-backend/internal/usecase"
	"github.com/amirullazmi0/fms-backend/pkg/geocode"
	"github.com/amirullazmi0/fms-backend/pkg/logger"
	"github.com/amirullazmi0/fms-backend/pkg/routing"
	"github.com/amirullazmi0/fms-backend/pkg/sse"
	"github.com/amirullazmi0/fms-backend/pkg/tunnel"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/requestid"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// Router wires repositories -> services -> usecases -> handlers, starts the background
// workers (device TCP listener, shift scheduler, geocoder, tunnel watcher) on ctx, and
// returns the HTTP router.
func Router(ctx context.Context, cfg *config.Config, db *sql.DB) (*gin.Engine, error) {
	// Repositories
	userRepo := repository.NewUserRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	deviceRepo := repository.NewDeviceRepository(db)
	eventRepo := repository.NewEventRepository(db)
	siteRepo := repository.NewSiteRepository(db)
	driverRepo := repository.NewDriverRepository(db)
	shiftRepo := repository.NewShiftRepository(db)
	tripRepo := repository.NewTripRepository(db)
	featureRepo := repository.NewFeatureRepository(db)
	visitRepo := repository.NewVisitRepository(db)
	roadRepo := repository.NewRoadRepository(db)
	productionRepo := repository.NewProductionRepository(db)
	mdtRepo := repository.NewMdtRepository(db)

	// Services
	hub := sse.NewHub()
	liveTracker := tracker.New(cfg, deviceRepo, eventRepo, hub)
	if err := liveTracker.Load(ctx); err != nil {
		return nil, fmt.Errorf("load tracker state: %w", err)
	}
	geocoder := geocode.New(cfg.Geocode.Enabled, eventRepo, func(format string, args ...any) {
		logger.Warn(fmt.Sprintf(format, args...))
	})
	if err := geocoder.Load(ctx); err != nil {
		return nil, fmt.Errorf("load place cache: %w", err)
	}
	router := routing.New(cfg.Routing.Enabled, cfg.Routing.OSRMURL, cfg.Routing.TruckFactor, routing.NewRoadGraph(cfg.Routing.OverpassURL, cfg.App.DataDir).WithStore(roadRepo))
	shiftScheduler := scheduler.New(shiftRepo, siteRepo, tripRepo, deviceRepo, driverRepo, liveTracker).WithVisits(visitRepo).WithMdt(mdtRepo)

	// Usecases & handlers
	userUsecase := usecase.NewUserUsecase(userRepo, roleRepo, &cfg.JWT)
	userHandler := NewUserHandler(userUsecase)

	roleUsecase := usecase.NewRoleUsecase(roleRepo)
	roleHandler := NewRoleHandler(roleUsecase)

	deviceUsecase := usecase.NewDeviceUsecase(deviceRepo, eventRepo, siteRepo, liveTracker, geocoder, &cfg.Report)
	deviceHandler := NewDeviceHandler(deviceUsecase)

	siteUsecase := usecase.NewSiteUsecase(siteRepo, deviceRepo, router, liveTracker)
	siteHandler := NewSiteHandler(siteUsecase)

	shiftUsecase := usecase.NewShiftUsecase(shiftRepo, driverRepo, tripRepo, shiftScheduler, liveTracker, &cfg.Report).WithVisits(visitRepo)
	shiftHandler := NewShiftHandler(shiftUsecase)

	productionUsecase := usecase.NewProductionUsecase(productionRepo, tripRepo, liveTracker, &cfg.Report)
	productionHandler := NewProductionHandler(productionUsecase)

	mdtUsecase := usecase.NewMdtUsecase(mdtRepo, tripRepo, liveTracker, cfg.Report.TimeZone)
	mdtHandler := NewMdtHandler(mdtUsecase)

	streamHandler := NewStreamHandler(hub)

	featureUsecase := usecase.NewFeatureUsecase(ctx, featureRepo, db, cfg, liveTracker, roadRepo)
	roadHandler := NewRoadHandler(roadRepo)
	featureHandler := NewFeatureHandler(featureUsecase)
	go featureUsecase.ResumeSimulation(ctx)

	// Background workers
	go func() {
		if err := liveTracker.Serve(ctx); err != nil {
			logger.Fatal("Device TCP listener failed", zap.Error(err))
		}
	}()
	switch {
	case cfg.Device.TunnelHost != "":
		// Fixed public address (e.g. the VPS domain + DEVICE_PORT): shown in the dashboard for the SMS setup.
		liveTracker.SetTunnel(&tunnel.Info{Host: cfg.Device.TunnelHost, Port: cfg.Device.TunnelPort, Provider: "manual"})
	case !cfg.Device.Tunnel:
		logger.Info("Tunnel off (TUNNEL=false): devices connect straight to DEVICE_PORT")
	default:
		devicePort, _ := strconv.Atoi(cfg.Device.Port)
		tunnels := tunnel.New(tunnel.Options{
			Mode:       cfg.Device.TunnelMode,
			Authtoken:  cfg.Device.NgrokAuthtoken,
			BoreHost:   cfg.Device.BoreHost,
			DevicePort: devicePort,
			DataDir:    cfg.App.DataDir,
			Serve:      liveTracker.ServeListener,
			OnChange:   liveTracker.SetTunnel,
			Logf: func(level, format string, args ...any) {
				liveTracker.Log("conn", "", fmt.Sprintf(format, args...), level)
			},
		})
		go tunnels.Run(ctx)
	}
	go geocoder.Run(ctx)
	go shiftScheduler.Run(ctx)

	if !cfg.App.Debug {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(middleware.Recovery())
	r.Use(requestid.New())
	r.Use(middleware.Logger())
	r.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.CORS.AllowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "timestamp": time.Now()})
	})

	auth := middleware.JWTAuth(&cfg.JWT)
	can := middleware.RequirePermission
	// fms-administrator endpoints: role must be is_administrator, plus the permission.
	administrator := middleware.RequireAdministrator()

	api := r.Group("/api")
	{
		authGroup := api.Group("/auth")
		{
			authGroup.POST("/login", middleware.RateLimit(middleware.RateLimitConfig{KeyPrefix: "auth:login", Limit: 20, Window: time.Minute}), userHandler.Login)
			authGroup.POST("/refresh", middleware.RateLimit(middleware.RateLimitConfig{KeyPrefix: "auth:refresh", Limit: 30, Window: time.Minute}), userHandler.RefreshToken)
			authGroup.POST("/logout", middleware.JWTAuthOptional(&cfg.JWT), userHandler.Logout)
		}

		users := api.Group("/users")
		users.Use(auth)
		{
			users.GET("/profile", userHandler.GetProfile)
			users.GET("", administrator, can(model.PermissionUserReadAll), userHandler.GetUsers)
			users.POST("", administrator, can(model.PermissionUserCreate), userHandler.CreateUser)
			users.PUT("/:id", administrator, can(model.PermissionUserUpdate), userHandler.UpdateUser)
			users.PUT("/:id/role", administrator, can(model.PermissionUserUpdateRole), userHandler.UpdateUserRole)
			users.DELETE("/:id", administrator, can(model.PermissionUserDelete), userHandler.DeleteUser)
		}

		api.GET("/permissions", auth, administrator, can(model.PermissionRoleRead), roleHandler.GetAllPermissions)

		roles := api.Group("/roles")
		roles.Use(auth, administrator)
		{
			roles.GET("", can(model.PermissionRoleRead), roleHandler.GetAllRoles)
			roles.GET("/:id", can(model.PermissionRoleRead), roleHandler.GetRoleByID)
			roles.POST("", can(model.PermissionRoleCreate), roleHandler.CreateRole)
			roles.PUT("/:id", can(model.PermissionRoleUpdate), roleHandler.UpdateRole)
			roles.DELETE("/:id", can(model.PermissionRoleDelete), roleHandler.DeleteRole)
		}

		// Live updates: any logged-in user (the page itself is gated by its permissions).
		api.GET("/stream", auth, streamHandler.Stream)

		devices := api.Group("/devices")
		devices.Use(auth)
		{
			devices.GET("", can(model.PermissionDeviceRead), deviceHandler.GetDevices)
			devices.GET("/:imei", can(model.PermissionDeviceRead), deviceHandler.GetDevice)
			devices.PATCH("/:imei", can(model.PermissionDeviceUpdate), deviceHandler.UpdateDevice)
			devices.DELETE("/:imei", can(model.PermissionDeviceDelete), deviceHandler.DeleteDevice)
			devices.GET("/:imei/day", can(model.PermissionDeviceRead), deviceHandler.Day)
			devices.GET("/:imei/ritase", can(model.PermissionDeviceRead, model.PermissionReportRead), deviceHandler.Ritase)
			devices.GET("/:imei/records", can(model.PermissionDeviceRead), deviceHandler.Records)
			devices.GET("/:imei/raw", can(model.PermissionDeviceRead), deviceHandler.RawRecords)
			devices.POST("/:imei/command", can(model.PermissionDeviceCommand), deviceHandler.Command)
			devices.GET("/:imei/mdt", can(model.PermissionMdtManage), mdtHandler.Tablets)
			devices.POST("/:imei/mdt", can(model.PermissionMdtManage), mdtHandler.PairCode)
		}
		api.DELETE("/mdt-tablets/:id", auth, can(model.PermissionMdtManage), mdtHandler.RevokeTablet)

		// Status unit & delay code (dasar PA/UA). Semua user login bisa baca daftar delay code.
		api.GET("/delay-codes", auth, mdtHandler.DelayCodes)
		api.PUT("/delay-codes", auth, can(model.PermissionDelayCodeManage), mdtHandler.UpsertDelayCode)
		unitStatus := api.Group("/unit-status")
		unitStatus.Use(auth)
		{
			unitStatus.GET("", can(model.PermissionDeviceRead, model.PermissionReportRead), mdtHandler.StatusLogs)
			unitStatus.GET("/current", can(model.PermissionDeviceRead, model.PermissionReportRead), mdtHandler.CurrentStatuses)
			unitStatus.POST("", can(model.PermissionUnitStatusWrite), mdtHandler.SetStatus)
		}

		// Aplikasi MDT di kabin (fms-mdt): pairing pakai kode, sisanya pakai token tablet.
		mdt := api.Group("/mdt")
		{
			mdt.POST("/pair", middleware.RateLimit(middleware.RateLimitConfig{KeyPrefix: "mdt:pair", Limit: 10, Window: time.Minute}), mdtHandler.Pair)
			tablet := mdt.Group("")
			tablet.Use(middleware.MdtAuth(mdtUsecase.TabletByToken))
			tablet.POST("/login", middleware.RateLimit(middleware.RateLimitConfig{KeyPrefix: "mdt:login", Limit: 20, Window: time.Minute}), mdtHandler.Login)
			tablet.GET("/bootstrap", mdtHandler.Bootstrap)
			tablet.GET("/position", mdtHandler.Position)
			tablet.POST("/status", mdtHandler.SubmitStatuses)
		}

		monitor := api.Group("")
		monitor.Use(auth)
		{
			monitor.GET("/events", can(model.PermissionDeviceRead), deviceHandler.Events)
			monitor.GET("/status", can(model.PermissionDeviceRead), deviceHandler.Status)
			monitor.GET("/io-map", can(model.PermissionDeviceRead), deviceHandler.IoMap)
			monitor.GET("/place", can(model.PermissionDeviceRead), deviceHandler.Place)
			monitor.GET("/commands", can(model.PermissionLogRead, model.PermissionDeviceCommand), deviceHandler.Commands)
			monitor.GET("/logs", can(model.PermissionLogRead), deviceHandler.Logs)
			monitor.GET("/road", can(model.PermissionRouteRead), siteHandler.Road)
		}

		sites := api.Group("/sites")
		sites.Use(auth)
		{
			sites.GET("", can(model.PermissionSiteRead), siteHandler.GetSites)
			sites.POST("", can(model.PermissionSiteCreate), siteHandler.CreateSite)
			sites.PUT("/:id", can(model.PermissionSiteUpdate), siteHandler.UpdateSite)
			sites.PUT("/:id/zones", can(model.PermissionSiteUpdate), siteHandler.UpdateZones)
			sites.DELETE("/:id", can(model.PermissionSiteDelete), siteHandler.DeleteSite)
		}

		routes := api.Group("/routes")
		routes.Use(auth)
		{
			routes.GET("", can(model.PermissionRouteRead), siteHandler.GetRoutes)
			routes.GET("/:id/history", can(model.PermissionRouteRead), siteHandler.RouteHistory)
			routes.POST("", can(model.PermissionRouteCreate), siteHandler.CreateRoute)
			routes.PUT("/:id", can(model.PermissionRouteUpdate), siteHandler.UpdateRoute)
			routes.DELETE("/:id", can(model.PermissionRouteDelete), siteHandler.DeleteRoute)
		}

		drivers := api.Group("/drivers")
		drivers.Use(auth)
		{
			drivers.GET("", can(model.PermissionDriverRead), shiftHandler.GetDrivers)
			drivers.GET("/last", can(model.PermissionDriverRead), shiftHandler.LastAssignments)
			drivers.POST("", can(model.PermissionDriverCreate), shiftHandler.CreateDriver)
			drivers.PUT("/:id", can(model.PermissionDriverUpdate), shiftHandler.UpdateDriver)
			drivers.DELETE("/:id", can(model.PermissionDriverDelete), shiftHandler.DeleteDriver)
			drivers.PUT("/:id/pin", can(model.PermissionMdtManage), mdtHandler.SetPin)
		}

		templates := api.Group("/shift-templates")
		templates.Use(auth)
		{
			templates.GET("", can(model.PermissionShiftTemplateRead), shiftHandler.GetTemplates)
			templates.POST("", can(model.PermissionShiftTemplateCreate), shiftHandler.CreateTemplate)
			templates.PUT("/:id", can(model.PermissionShiftTemplateUpdate), shiftHandler.UpdateTemplate)
			templates.DELETE("/:id", can(model.PermissionShiftTemplateDelete), shiftHandler.DeleteTemplate)
		}

		shifts := api.Group("/shifts")
		shifts.Use(auth)
		{
			shifts.GET("", can(model.PermissionShiftRead), shiftHandler.GetShifts)
			shifts.GET("/:id", can(model.PermissionShiftRead), shiftHandler.GetShift)
			shifts.POST("", can(model.PermissionShiftCreate), shiftHandler.CreateShift)
			shifts.PATCH("/:id", can(model.PermissionShiftUpdate), shiftHandler.UpdateShift)
			shifts.DELETE("/:id", can(model.PermissionShiftDelete), shiftHandler.DeleteShift)
			shifts.POST("/:id/start", can(model.PermissionShiftOperate), shiftHandler.StartShift)
			shifts.POST("/:id/close", can(model.PermissionShiftOperate), shiftHandler.CloseShift)
			shifts.POST("/:id/reopen", can(model.PermissionShiftOperate), shiftHandler.ReopenShift)
			shifts.POST("/:id/assignments", can(model.PermissionAssignmentManage), shiftHandler.AddAssignment)
			shifts.POST("/:id/copy-previous", can(model.PermissionAssignmentManage), shiftHandler.CopyPrevious)
		}

		assignments := api.Group("/assignments")
		assignments.Use(auth, can(model.PermissionAssignmentManage))
		{
			assignments.PUT("/:id", shiftHandler.UpdateAssignment)
			assignments.DELETE("/:id", shiftHandler.EndAssignment)
		}

		// Fitur (fms-administrator → Fitur). Every signed-in app reads the switches.
		features := api.Group("/features")
		features.Use(auth)
		{
			features.GET("", featureHandler.GetFeatures)
			features.PUT("/:key", administrator, can(model.PermissionFeatureUpdate), featureHandler.UpdateFeature)
		}

		// Stored road data (fms-administrator → Data jalan).
		roadData := api.Group("/road-data")
		roadData.Use(auth, administrator)
		{
			roadData.GET("/paths", can(model.PermissionRoadRead), roadHandler.GetPaths)
			roadData.GET("/paths/:id", can(model.PermissionRoadRead), roadHandler.GetPath)
			roadData.DELETE("/paths", can(model.PermissionRoadDelete), roadHandler.DeleteAllPaths)
			roadData.DELETE("/paths/:id", can(model.PermissionRoadDelete), roadHandler.DeletePath)
			roadData.GET("/areas", can(model.PermissionRoadRead), roadHandler.GetAreas)
			roadData.DELETE("/areas", can(model.PermissionRoadDelete), roadHandler.DeleteAllAreas)
			roadData.DELETE("/areas/:id", can(model.PermissionRoadDelete), roadHandler.DeleteArea)
		}

		// Truck simulator run by the backend (feature "simulation").
		simulation := api.Group("/simulation")
		simulation.Use(auth, administrator, can(model.PermissionSimulationRun))
		{
			simulation.GET("", featureHandler.GetSimulation)
			simulation.POST("/start", featureHandler.StartSimulation)
			simulation.POST("/stop", featureHandler.StopSimulation)
		}

		reports := api.Group("/reports")
		reports.Use(auth, can(model.PermissionReportRead))
		{
			reports.GET("", shiftHandler.Report)
			reports.GET("/trips", shiftHandler.ReportTrips)
			reports.GET("/visits", shiftHandler.ReportVisits)
			reports.GET("/visits/list", shiftHandler.ReportVisitList)
			reports.GET("/hourly", productionHandler.Hourly)
			reports.GET("/freshness", productionHandler.Freshness)
			reports.GET("/stages", productionHandler.Stages)
		}

		// Target produksi per rute: semua yang bisa baca laporan boleh lihat, ubah butuh target.manage.
		targets := api.Group("/production-targets")
		targets.Use(auth)
		{
			targets.GET("", can(model.PermissionReportRead, model.PermissionTargetManage), productionHandler.GetTargets)
			targets.POST("", can(model.PermissionTargetManage), productionHandler.CreateTarget)
			targets.PUT("/:id", can(model.PermissionTargetManage), productionHandler.UpdateTarget)
			targets.DELETE("/:id", can(model.PermissionTargetManage), productionHandler.DeleteTarget)
		}
	}

	return r, nil
}
