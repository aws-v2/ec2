package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	httpd "net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"os/exec"
	"path/filepath"
	"strings"

	config "ec2-api/config"
	transport "ec2-api/internal/api/http/handlers"
	routes "ec2-api/internal/api/http/router"
	application "ec2-api/internal/application"
	database "ec2-api/internal/infra/database"
	libvirt "ec2-api/internal/infra/libvirt"
	messaging "ec2-api/internal/infra/messaging"
	repository "ec2-api/internal/infra/repository"
	storage "ec2-api/internal/infra/storage"
	interfaces "ec2-api/internal/interfaces"
	"ec2-api/internal/vpcpkg"
	pkg "ec2-api/pkg"


	"log/slog"

	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize structured logging
	var handler slog.Handler
	if cfg.Profile == "prod" {
		handler = slog.NewJSONHandler(os.Stdout, nil)
	} else {
		handler = slog.NewTextHandler(os.Stdout, nil)
	}
	logger := slog.New(handler.WithAttrs([]slog.Attr{
		slog.String("profile", cfg.Profile),
		slog.String("service", "ec2-service"),
	}))
	slog.SetDefault(logger)

	slog.Info("Starting EC2 service", "profile", cfg.Profile)

	postgresConn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.DB.User, cfg.DB.Password, cfg.DB.Host, cfg.DB.Port, cfg.DB.Database, cfg.DB.SSLMode)

	libvirtURI := cfg.Libvirt.URI
	imagesDir := cfg.Libvirt.ImagesDir

	// 1. Perform TCP Reachability Checks
	slog.Info("Performing pre-startup reachability checks...")

	// NATS Check
	if err := pkg.CheckReachabilityURL("NATS", cfg.NATS.URL, 5, 2*time.Second); err != nil {
		slog.Error("FATAL: NATS unreachable", "url", cfg.NATS.URL, "error", err)
		os.Exit(1)
	}

	// Database Check
	if err := pkg.CheckReachability("PostgreSQL", cfg.DB.Host, cfg.DB.Port, 5, 2*time.Second); err != nil {
		slog.Error("FATAL: Database unreachable", "host", cfg.DB.Host, "port", cfg.DB.Port, "error", err)
		os.Exit(1)
	}

	// 2. Initialize Messaging Layer (Priority)
	slog.Info("Initializing NATS publisher...")
	natsPublisher, err := messaging.NewNATSPublisher(cfg.NATS.URL, cfg.NATS.User, cfg.NATS.Password, cfg.NATS.SubjectPrefix)
	if err != nil {
		slog.Warn("Failed to connect to NATS", "url", cfg.NATS.URL, "error", err)
		natsPublisher = nil
	} else {
		defer natsPublisher.Close()
		slog.Info("NATS publisher initialized successfully")
	}

	// 3. Initialize Infrastructure Layer
	slog.Info("Initializing PostgreSQL repository...")
	db, err := database.NewPostgresDB(postgresConn)
	if err != nil {
		slog.Error("Failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	slog.Info("Running database migrations...", "migrations_dir", cfg.MigrationsDir)
	if err := database.MigrateDir(db, cfg.MigrationsDir); err != nil {
		slog.Error("Failed to migrate database", "error", err)
		os.Exit(1)
	}
	slog.Info("Database migration completed successfully")

	slog.Info("Initializing Libvirt client...")
	natsSubject := messaging.BuildSubject(cfg.Profile, "instance", "lifecycle")
	libvirtClient, err := libvirt.NewLibvirtClient(libvirtURI, imagesDir, cfg.MinIO.Endpoint, cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, cfg.NATS.URL, natsSubject)
	
	
	
	if err != nil {
		slog.Warn("Failed to connect to libvirt", "error", err)
		libvirtClient = nil
	} else {
		defer libvirtClient.Close()
		slog.Info("Libvirt connected successfully")
	}

	// 1.6 Initialize Storage Layer (MinIO)
	slog.Info("Initializing MinIO adapter...")
	minioAdapter, err := storage.NewMinIOAdapter(cfg.MinIO.Endpoint, cfg.MinIO.AccessKey, cfg.MinIO.SecretKey, cfg.MinIO.UseSSL)
	if err != nil {
		slog.Warn("Failed to initialize MinIO adapter", "error", err)
		minioAdapter = nil
	} else {
		slog.Info("MinIO adapter initialized successfully")
	}

	// // 2. Initialize Repository Layer
	var instanceRepo interfaces.InstanceRepository
	var volumeRepo interfaces.VolumeRepository
	var snapshotRepo interfaces.SnapshotRepository
	var sshKeyRepo interfaces.SSHKeyRepository
	var ipRepo interfaces.IPRepository
	var sgRepo interfaces.SecurityGroupRepository
	var templateRepo interfaces.TemplateRepository
	var fleetRepo interfaces.FleetRepository
	var hostRepo interfaces.HostRepository

	instanceRepo = repository.NewInstanceRepository(db,cfg)
	volumeRepo = repository.NewVolumeRepository(db)
	snapshotRepo = repository.NewSnapshotRepository(db)
	sshKeyRepo = repository.NewSSHKeyRepository(db)
	ipRepo = repository.NewIPRepository(db)
	sgRepo = repository.NewSecurityGroupRepository(db)
	templateRepo = repository.NewTemplateRepository(db)
	fleetRepo = repository.NewFleetRepository(db)
	hostRepo = repository.NewHostRepository(db.DB)

	// Initialize System Key Service
	systemKeyService := application.NewSystemKeyService(imagesDir) // Store keys near images
	if err := systemKeyService.EnsureKeys(); err != nil {
		slog.Warn("Failed to ensure system keys", "error", err)
	}
	systemPubKey, _ := systemKeyService.GetPublicKeyString()

	// 3. Initialize Application Layer (Services)
	slog.Info("Initializing services...")
	networkingService := application.NewNetworkingService(ipRepo, sgRepo, instanceRepo, libvirtClient, natsPublisher)
	if err := networkingService.SeedDefaultSecurityGroup(); err != nil {
		slog.Warn("Failed to seed default security group", "error", err)
	}

	keysDir := getEnv("KEYS_DIR", filepath.Join(imagesDir, "keys"))
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		slog.Warn("Failed to create keys directory", "error", err)
	}
	vpcProvisioner := vpcpkg.NewVPCProvisioner(libvirtClient.Conn())
	vpcRepo := repository.NewVPCRepository(db)
	vpcService := vpcpkg.NewVpcService(vpcRepo,vpcProvisioner)

	hostService := application.NewHostService(hostRepo)

	instanceService := application.NewInstanceService(instanceRepo, networkingService, libvirtClient, systemPubKey, imagesDir, natsPublisher, minioAdapter,vpcService, hostService)
	volumeService := application.NewVolumeService(volumeRepo, instanceRepo, libvirtClient)
	snapshotService := application.NewSnapshotService(snapshotRepo, instanceRepo, volumeRepo, libvirtClient)
	sshKeyService := application.NewSSHKeyService(sshKeyRepo, systemKeyService, keysDir)
	templateService := application.NewTemplateService(templateRepo, instanceRepo, libvirtClient)
	terminalService := application.NewTerminalService(instanceRepo)
	fleetService := application.NewFleetService(fleetRepo, instanceRepo)
	docsService := application.NewDocsService("docs")

	// 3.5 Initialize NATS Subscriber for Scaling Enforcement
	if cfg.NATS.URL != "" {
		natsSubscriber, err := messaging.NewNATSSubscriber(cfg.NATS.URL, cfg.NATS.User, cfg.NATS.Password, cfg.NATS.SubjectPrefix, instanceService)
		if err != nil {
			slog.Warn("Failed to initialize NATS subscriber", "error", err)
		} else {
			if err := natsSubscriber.Start(); err != nil {
				slog.Warn("Failed to start NATS subscriber", "error", err)
			} else {
				defer natsSubscriber.Close()
			}
		}
	}

	// 4. Initialize Transport Layer (HTTP Handlers)
	slog.Info("Initializing HTTP handlers...")
	instanceHandler := transport.NewInstanceHandler(instanceService)
	volumeHandler := transport.NewVolumeHandler(volumeService, snapshotService)
	snapshotHandler := transport.NewSnapshotHandler(snapshotService)
	sshKeyHandler := transport.NewSSHKeyHandler(sshKeyService)
	networkingHandler := transport.NewNetworkingHandler(networkingService)
	templateHandler := transport.NewTemplateHandler(templateService)
	terminalHandler := transport.NewTerminalHandler(terminalService)
	fleetHandler := transport.NewFleetHandler(fleetService)
	docsHandler := transport.NewDocsHandler(docsService)
	hostHandler := transport.NewHostHandler(hostService)

	// 5. Setup Router
	router := gin.Default()
	router.SetTrustedProxies(nil)

	// Health check for Eureka
	router.GET("/health", func(c *gin.Context) {
		c.JSON(httpd.StatusOK, gin.H{"status": "UP"})
	})

	routes.SetupRoutes(router, instanceHandler, volumeHandler, snapshotHandler, sshKeyHandler, networkingHandler, templateHandler, terminalHandler, fleetHandler, docsHandler, hostHandler)

	// 6. Eureka Registration
	eurekaConfig := getEurekaConfig()
	if err := registerWithEureka(eurekaConfig); err != nil {
		slog.Warn("Eureka registration failed", "error", err)
	} else {
		go sendHeartbeat(eurekaConfig)
	}

	// 7. Start Server with Graceful Shutdown
	addr := cfg.Server.Port
	if !strings.HasPrefix(addr, ":") {
		addr = ":" + addr
	}

	srv := &httpd.Server{
		Addr:    addr,
		Handler: router,
	}

	// Initializing the server in a goroutine so that it won't block the graceful shutdown handling below
	go func() {
		slog.Info("Server starting", "port", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != httpd.ErrServerClosed {
			slog.Error("Failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("Shutting down server...")

	// Deregister from Eureka
	if err := deregisterFromEureka(eurekaConfig); err != nil {
		slog.Warn("Eureka deregistration failed", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exiting")
}

func getEnv(key, defaultValue string) string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value
}

func getEnvInt(key string, defaultValue int) int {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue
	}
	return i
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return defaultValue
	}
	return d
}

// EurekaConfig holds Eureka registration configuration
type EurekaConfig struct {
	ServerURL         string
	AppName           string
	HostName          string
	IPAddr            string
	Port              int
	VipAddress        string
	InstanceID        string
	HeartbeatInterval time.Duration
}

// getEurekaConfig reads Eureka configuration from environment variables
func getEurekaConfig() *EurekaConfig {
	// Windows host IP for Eureka server
	// windowsHostIP := getEnv("WINDOWS_HOST_IP", "192.168.1.2")

	// Get WSL's IP address that Windows can reach
	wslIP := getWSLIPAddress()

	return &EurekaConfig{
		ServerURL: getEnv("EUREKA_SERVER_URL", "http://localhost:8761/eureka"),
		// ServerURL:         getEnv("EUREKA_SERVER_URL", fmt.Sprintf("http://%s:8761/eureka", windowsHostIP)),
		AppName:           getEnv("EUREKA_APP_NAME", "ec2-service"),
		HostName:          getEnv("EUREKA_HOSTNAME", wslIP), // Use WSL IP
		IPAddr:            getEnv("EUREKA_IP_ADDR", wslIP),  // Use WSL IP
		Port:              getEnvInt("SERVER_PORT", 8088),
		VipAddress:        getEnv("EUREKA_VIP_ADDRESS", "ec2-service"),
		InstanceID:        getEnv("EUREKA_INSTANCE_ID", "ec2-service:8088"),
		HeartbeatInterval: getEnvDuration("EUREKA_HEARTBEAT_INTERVAL", 30*time.Second),
	}
}

// getWSLIPAddress retrieves the WSL IP address
func getWSLIPAddress() string {
	// Try environment variable first
	if ip := os.Getenv("WSL_IP_ADDR"); ip != "" {
		return ip
	}

	// Try to get WSL IP programmatically
	cmd := exec.Command("hostname", "-I")
	output, err := cmd.Output()
	if err == nil {
		ips := strings.Fields(string(output))
		if len(ips) > 0 {
			return ips[0]
		}
	}

	// Fallback to localhost (won't work from Windows but better than nothing)
	slog.Warn("Could not determine WSL IP, falling back to 127.0.0.1")
	return "127.0.0.1"
}

// registerWithEureka registers the service instance with Eureka server
func registerWithEureka(config *EurekaConfig) error {
	instance := map[string]interface{}{
		"instance": map[string]interface{}{
			"instanceId": config.InstanceID,
			"hostName":   config.HostName,
			"app":        config.AppName,
			"ipAddr":     config.IPAddr,
			"vipAddress": config.VipAddress,
			"status":     "UP",
			"port": map[string]interface{}{
				"$":        config.Port,
				"@enabled": "true",
			},
			"dataCenterInfo": map[string]interface{}{
				"@class": "com.netflix.appinfo.InstanceInfo$DefaultDataCenterInfo",
				"name":   "MyOwn",
			},
			"healthCheckUrl": fmt.Sprintf("http://%s:%d/health", config.HostName, config.Port),
			"statusPageUrl":  fmt.Sprintf("http://%s:%d/health", config.HostName, config.Port),
			"homePageUrl":    fmt.Sprintf("http://%s:%d/", config.HostName, config.Port),
		},
	}

	jsonData, err := json.Marshal(instance)
	if err != nil {
		return fmt.Errorf("failed to marshal Eureka registration data: %w", err)
	}

	url := fmt.Sprintf("%s/apps/%s", config.ServerURL, config.AppName)
	req, err := httpd.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create registration request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	client := &httpd.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to register with Eureka: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != httpd.StatusNoContent && resp.StatusCode != httpd.StatusOK {
		return fmt.Errorf("eureka registration failed with status %d: %s", resp.StatusCode, string(body))
	}

	slog.Info("Successfully registered with Eureka server", "url", url)
	return nil
}

// sendHeartbeat sends periodic heartbeats to Eureka server
func sendHeartbeat(config *EurekaConfig) {
	ticker := time.NewTicker(config.HeartbeatInterval)
	defer ticker.Stop()

	url := fmt.Sprintf("%s/apps/%s/%s", config.ServerURL, config.AppName, config.InstanceID)
	client := &httpd.Client{Timeout: 5 * time.Second}

	for range ticker.C {
		req, err := httpd.NewRequest("PUT", url, nil)
		if err != nil {
			slog.Error("Failed to create heartbeat request", "error", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Error("Failed to send heartbeat to Eureka", "error", err)
			continue
		}

		if resp.StatusCode != httpd.StatusOK && resp.StatusCode != httpd.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			slog.Warn("Heartbeat failed", "status", resp.StatusCode, "body", string(body))
		} else {
			slog.Debug("Heartbeat sent successfully to Eureka")
		}

		resp.Body.Close()
	}
}

// deregisterFromEureka removes the service instance from Eureka
func deregisterFromEureka(config *EurekaConfig) error {
	url := fmt.Sprintf("%s/apps/%s/%s", config.ServerURL, config.AppName, config.InstanceID)
	req, err := httpd.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create deregistration request: %w", err)
	}

	client := &httpd.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to deregister from Eureka: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != httpd.StatusOK && resp.StatusCode != httpd.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deregistration failed with status %d: %s", resp.StatusCode, string(body))
	}

	slog.Info("Successfully deregistered from Eureka server")
	return nil
}
