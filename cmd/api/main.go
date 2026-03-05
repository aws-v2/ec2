package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	httpd "net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"os/exec"
	"path/filepath"
	"strings"
	"github.com/Qarani-m/ec2-api/internal/application"
	"github.com/Qarani-m/ec2-api/internal/config"
	"github.com/Qarani-m/ec2-api/internal/domain"
	"github.com/Qarani-m/ec2-api/internal/libvirt"
	"github.com/Qarani-m/ec2-api/internal/repository/postgres"
	transport "github.com/Qarani-m/ec2-api/internal/transport/http"
	"github.com/Qarani-m/ec2-api/pkg/database"
	"github.com/Qarani-m/ec2-api/pkg/messaging"
	"github.com/gin-gonic/gin"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	postgresConn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.DB.User, cfg.DB.Password, cfg.DB.Host, cfg.DB.Port, cfg.DB.Database, cfg.DB.SSLMode)
	
	libvirtURI := cfg.Libvirt.URI
	imagesDir := cfg.Libvirt.ImagesDir
	natsSubject := getEnv("NATS_LIFECYCLE_SUBJECT", "dev.compute.v1.instance.lifecycle")
	// libvirtURI := getEnv("LIBVIRT_URI", "qemu:///session")

	// 1. Initialize Infrastructure Layer
	log.Println("Initializing PostgreSQL repository...")
	db, err := database.NewPostgresDB(postgresConn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	log.Println("Running database migrations...")
	if err := database.Migrate(db, postgres.Schema); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}
	log.Println("Database migration completed successfully")

	log.Println("Initializing Libvirt client...")
	libvirtClient, err := libvirt.NewLibvirtClient(libvirtURI, imagesDir)
	if err != nil {
		log.Printf("Warning: Failed to connect to libvirt: %v", err)
		log.Println("Continuing without libvirt support...")
		libvirtClient = nil
	} else {
		defer libvirtClient.Close()
		log.Println("Libvirt connected successfully")
	}

	// 1.5 Initialize Messaging Layer
	log.Println("Initializing NATS publisher...")
	natsPublisher, err := messaging.NewNATSPublisher(cfg.NATS.URL, cfg.NATS.User, cfg.NATS.Password, natsSubject)
	if err != nil {
		log.Printf("Warning: Failed to connect to NATS at %s: %v", cfg.NATS.URL, err)
		log.Println("Continuing without NATS publishing support...")
		natsPublisher = nil
	} else {
		defer natsPublisher.Close()
		log.Println("NATS publisher initialized successfully")
	}

	log.Println(postgresConn)

	// // 2. Initialize Repository Layer
	var instanceRepo domain.InstanceRepository
	var volumeRepo domain.VolumeRepository
	var snapshotRepo domain.SnapshotRepository
	var sshKeyRepo domain.SSHKeyRepository
	var ipRepo domain.IPRepository
	var sgRepo domain.SecurityGroupRepository
	var templateRepo domain.TemplateRepository
	var fleetRepo domain.FleetRepository

	instanceRepo = postgres.NewInstanceRepository(db)
	volumeRepo = postgres.NewVolumeRepository(db)
	snapshotRepo = postgres.NewSnapshotRepository(db)
	sshKeyRepo = postgres.NewSSHKeyRepository(db)
	ipRepo = postgres.NewIPRepository(db)
	sgRepo = postgres.NewSecurityGroupRepository(db)
	templateRepo = postgres.NewTemplateRepository(db)
	fleetRepo = postgres.NewFleetRepository(db)

	// Initialize System Key Service
	systemKeyService := application.NewSystemKeyService(imagesDir) // Store keys near images
	if err := systemKeyService.EnsureKeys(); err != nil {
		log.Printf("Warning: Failed to ensure system keys: %v", err)
	}
	systemPubKey, _ := systemKeyService.GetPublicKeyString()

	// 3. Initialize Application Layer (Services)
	log.Println("Initializing services...")
	networkingService := application.NewNetworkingService(ipRepo, sgRepo, instanceRepo, libvirtClient)
	if err := networkingService.SeedDefaultSecurityGroup(); err != nil {
		log.Printf("Warning: Failed to seed default security group: %v", err)
	}

	keysDir := getEnv("KEYS_DIR", filepath.Join(imagesDir, "keys"))
	if err := os.MkdirAll(keysDir, 0755); err != nil {
		log.Printf("Warning: Failed to create keys directory: %v", err)
	}

	instanceService := application.NewInstanceService(instanceRepo, networkingService, libvirtClient, systemPubKey, imagesDir, natsPublisher)
	volumeService := application.NewVolumeService(volumeRepo, instanceRepo, libvirtClient)
	snapshotService := application.NewSnapshotService(snapshotRepo, instanceRepo, volumeRepo, libvirtClient)
	sshKeyService := application.NewSSHKeyService(sshKeyRepo, systemKeyService, keysDir)
	templateService := application.NewTemplateService(templateRepo, instanceRepo, libvirtClient)
	terminalService := application.NewTerminalService(instanceService, systemKeyService)
	fleetService := application.NewFleetService(fleetRepo, instanceRepo)


	// 4. Initialize Transport Layer (HTTP Handlers)
	log.Println("Initializing HTTP handlers...")
	instanceHandler := transport.NewInstanceHandler(instanceService)
	volumeHandler := transport.NewVolumeHandler(volumeService, snapshotService)
	snapshotHandler := transport.NewSnapshotHandler(snapshotService)
	sshKeyHandler := transport.NewSSHKeyHandler(sshKeyService)
	networkingHandler := transport.NewNetworkingHandler(networkingService)
	templateHandler := transport.NewTemplateHandler(templateService)
	terminalHandler := transport.NewTerminalHandler(terminalService)
	fleetHandler := transport.NewFleetHandler(fleetService)

	// Docs handler — docs are stored in <workdir>/docs/compute
	workDir, _ := os.Getwd()
	computeDocsDir := filepath.Join(workDir, "docs", "compute")
	docsHandler := transport.NewDocsHandler(computeDocsDir)

	// 5. Setup Router
	router := gin.Default()
	router.SetTrustedProxies(nil)

	// Health check for Eureka
	router.GET("/health", func(c *gin.Context) {
		c.JSON(httpd.StatusOK, gin.H{"status": "UP"})
	})

	transport.SetupRoutes(router, instanceHandler, volumeHandler, snapshotHandler, sshKeyHandler, networkingHandler, templateHandler, terminalHandler, fleetHandler, docsHandler)

	// 6. Eureka Registration
	eurekaConfig := getEurekaConfig()
	if err := registerWithEureka(eurekaConfig); err != nil {
		log.Printf("⚠️  Eureka registration failed: %v", err)
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
		log.Printf("🚀 Server starting on port %s...", cfg.Server.Port)
		if err := srv.ListenAndServe(); err != nil && err != httpd.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Deregister from Eureka
	if err := deregisterFromEureka(eurekaConfig); err != nil {
		log.Printf("⚠️  Eureka deregistration failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exiting")
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
		ServerURL:          getEnv("EUREKA_SERVER_URL", "http://localhost:8761/eureka"),
		// ServerURL:         getEnv("EUREKA_SERVER_URL", fmt.Sprintf("http://%s:8761/eureka", windowsHostIP)),
		AppName:           getEnv("EUREKA_APP_NAME", "ec2-service"),
		HostName:          getEnv("EUREKA_HOSTNAME", wslIP),  // Use WSL IP
		IPAddr:            getEnv("EUREKA_IP_ADDR", wslIP),   // Use WSL IP
		Port:              getEnvInt("SERVER_PORT", 8085),
		VipAddress:        getEnv("EUREKA_VIP_ADDRESS", "ec2-service"),
		InstanceID:        getEnv("EUREKA_INSTANCE_ID", "ec2-service:8085"),
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
	log.Println("⚠️  Could not determine WSL IP, falling back to 127.0.0.1")
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

	log.Printf("✅ Successfully registered with Eureka server at %s", url)
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
			log.Printf("❌ Failed to create heartbeat request: %v", err)
			continue
		}

		resp, err := client.Do(req)
		if err != nil {
			log.Printf("❌ Failed to send heartbeat to Eureka: %v", err)
			continue
		}

		if resp.StatusCode != httpd.StatusOK && resp.StatusCode != httpd.StatusNoContent {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("⚠️  Heartbeat failed with status %d: %s", resp.StatusCode, string(body))
		} else {
			log.Printf("💓 Heartbeat sent successfully to Eureka")
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

	log.Printf("✅ Successfully deregistered from Eureka server")
	return nil
}
