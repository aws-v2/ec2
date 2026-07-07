//go:build !windows

package libvirt

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	domain "ec2-api/internal/domain/instance"

	libvirt "libvirt.org/go/libvirt"
)

type LibvirtClient struct {
	conn          *libvirt.Connect
	imagesDir     string
	minioEndpoint string
	minioAK       string
	minioSK       string
	natsURL       string
	natsSubject   string
}

func NewLibvirtClient(uri string, imagesDir, minioEndpoint, minioAK, minioSK, natsURL, natsSubject string) (*LibvirtClient, error) {
	conn, err := libvirt.NewConnect(uri)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to libvirt: %w", err)
	}
	return &LibvirtClient{
		conn:          conn,
		imagesDir:     imagesDir,
		minioEndpoint: minioEndpoint,
		minioAK:       minioAK,
		minioSK:       minioSK,
		natsURL:       natsURL,
		natsSubject:   natsSubject,
	}, nil
}
func (c *LibvirtClient) Conn() *libvirt.Connect {
	return c.conn
}
func (l *LibvirtClient) Close() error {
	_, err := l.conn.Close()
	return err
}

func (l *LibvirtClient) GetImagesDir() string {
	return l.imagesDir
}

func runCmdWithProgress(cmd *exec.Cmd) ([]byte, error) {
	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = io.MultiWriter(os.Stderr, &buf)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // don't die on parent SIGINT
	err := cmd.Run()
	return buf.Bytes(), err
}

func (l *LibvirtClient) CreateAndStartVM(
	remoteHostIP string,
	remoteHostUser string,
	remoteHostKey string,

	vmName string,

	diskPath string,
	isoPath string,

	cpu int,
	ram int,

	bridgeName string,
	privateIP string,
	gateway string,
	profile string, // add this
) (int, error) {

	// ---------------------------------------------------
	// Network
	// ---------------------------------------------------

	if bridgeName == "" {
		if err := l.EnsureDefaultNetwork(); err != nil {
			return 0, fmt.Errorf("network setup failed: %w", err)
		}
	}

	// ---------------------------------------------------
	// Build XML
	// ---------------------------------------------------

	xmlConfig := l.buildVMXML(
		vmName,
		diskPath,
		isoPath,
		cpu,
		ram,
		bridgeName,
		profile,
	)

	// ---------------------------------------------------
	// Connect to libvirt
	// ---------------------------------------------------

	conn := l.conn

	if remoteHostIP != "" {

		if remoteHostUser == "" {
			remoteHostUser = "root"
		}

		// Write key to temp file
		formattedKey := strings.TrimSpace(strings.ReplaceAll(remoteHostKey, "\\n", "\n")) + "\n"

		keyFile, err := os.CreateTemp("", "id_rsa_libvirt_*")
		if err != nil {
			return 0, fmt.Errorf("failed to create temp key file: %w", err)
		}
		defer os.Remove(keyFile.Name())

		if _, err := keyFile.Write([]byte(formattedKey)); err != nil {
			keyFile.Close()
			return 0, fmt.Errorf("failed to write key file: %w", err)
		}
		keyFile.Close()

		if err := os.Chmod(keyFile.Name(), 0600); err != nil {
			return 0, fmt.Errorf("failed to chmod key file: %w", err)
		}

		remoteURI := fmt.Sprintf(
			"qemu+ssh://%s@%s/system?keyfile=%s&no_verify=1&sshauth=privkey",
			remoteHostUser,
			remoteHostIP,
			keyFile.Name(), // 👈 was missing
		)

		remoteConn, err := libvirt.NewConnect(remoteURI)
		if err != nil {
			return 0, fmt.Errorf("failed to connect remote libvirt: %w", err)
		}
		defer remoteConn.Close()

		conn = remoteConn
	}
	// ---------------------------------------------------
	// Define VM
	// ---------------------------------------------------

	domain, err := conn.DomainDefineXML(xmlConfig)
	if err != nil {
		return 0, fmt.Errorf(
			"failed to define vm: %w",
			err,
		)
	}

	// ---------------------------------------------------
	// Start VM
	// ---------------------------------------------------

	if err := domain.Create(); err != nil {
		_ = domain.Undefine()

		return 0, fmt.Errorf(
			"failed to start vm: %w",
			err,
		)
	}

	// ---------------------------------------------------
	// VM ID
	// ---------------------------------------------------

	id, err := domain.GetID()
	if err != nil {
		return 0, fmt.Errorf(
			"failed to get vm id: %w",
			err,
		)
	}

	log.Printf(
		"[Libvirt] VM %s started on host %s with IP %s",
		vmName,
		remoteHostIP,
		privateIP,
	)

	return int(id), nil
}

// createCloudInitISO builds a cloud-init ISO with three files:
//   - user-data  : SSH keys, user setup
//   - meta-data  : instance ID and hostname
//   - network-config : static IP configuration (v2 format)
//
// The network-config file is what tells cloud-init to configure the NIC with
// the pre-allocated IP from the network service instead of using DHCP.

func (l *LibvirtClient) CreateCloudInitISO(vmName, combinedKeys, privateIP, gateway, instanceToken, profile string, params map[string]string) (string, func(), error) {
	userDataPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-user-data", vmName))
	metaDataPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-meta-data", vmName))
	networkCfgPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s-network-config", vmName))
	absDir, _ := filepath.Abs(l.imagesDir)
	isoPath := filepath.Join(absDir, fmt.Sprintf("%s-cloudinit.iso", vmName))

	// Derive the instance ID from vmName (vm-i-abc12345 → i-abc12345)
	instanceID := strings.TrimPrefix(vmName, "vm-")

	cleanup := func() {
		os.Remove(userDataPath)
		os.Remove(metaDataPath)
		os.Remove(networkCfgPath)
	}

	// ── user-data ─────────────────────────────────────────────────────────────
	keysYaml := ""
	for _, key := range strings.Split(combinedKeys, "\n") {
		key = strings.TrimSpace(key)
		// Drop blank lines and any fragment that isn't a real key
		if key == "" || (!strings.HasPrefix(key, "ssh-") && !strings.HasPrefix(key, "ecdsa-")) {
			continue
		}
		keysYaml += fmt.Sprintf("      - %s\n", key)
	}

	if keysYaml == "" {
		fmt.Printf("[Libvirt] [WARN] No valid SSH keys found for VM %s\n", vmName)
	}

	if keysYaml == "" {
		fmt.Printf("[Libvirt] [WARN] No SSH keys provided for VM %s\n", vmName)
	}

	writeFiles := `  - path: /opt/metrics-agent/config
    permissions: '0600'
    content: |
      INSTANCE_ID="__INSTANCE_ID__"
      IAM_TOKEN="__IAM_TOKEN__"
      METRICS_ENDPOINT="http://192.168.1.7:8099/api/v1/metrics-server/ec2/ingest"

  - path: /opt/metrics-agent/report.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      source /opt/metrics-agent/config
      while true; do
        CPU=$(top -bn2 -d 1 | grep "Cpu(s)" | tail -n 1 | awk '{print 100 - $8}')
        if [ -z "$CPU" ]; then CPU="0.0"; fi
        MEM_TOTAL=$(free -m | awk '/Mem:/{print $2}')
        MEM_USED=$(free -m | awk '/Mem:/{print $2}')
        MEM_PCT=$(awk -v used="$MEM_USED" -v total="$MEM_TOTAL" 'BEGIN{printf "%.1f", (used/total)*100}')
        curl -s -X POST "${METRICS_ENDPOINT}" \
          -H "Content-Type: application/json" \
          -H "Authorization: Bearer ${IAM_TOKEN}" \
          -d "{
            \"instance_id\": \"${INSTANCE_ID}\",
            \"cpu_percent\": ${CPU},
            \"mem_total_mb\": ${MEM_TOTAL},
            \"mem_used_mb\": ${MEM_USED},
            \"mem_percent\": ${MEM_PCT},
            \"disk_total_gb\": 0,
            \"disk_used_gb\": 0
          }" 2>/dev/null
        sleep 5
      done

  - path: /etc/systemd/system/metrics-agent.service
    content: |
      [Unit]
      Description=Instance Metrics Agent
      After=network-online.target
      [Service]
      Type=simple
      ExecStart=/opt/metrics-agent/report.sh
      Restart=always
      RestartSec=5
      [Install]
      WantedBy=multi-user.target`

	runCmd := `  - systemctl daemon-reload
  - systemctl enable metrics-agent
  - systemctl start metrics-agent`

	// ── Select Profile Template ──────────────────────────────────────────────
	userName :="ubuntu"
	profileContent := ""
	switch profile {
	case "gamelift":
		userName="gls"
		profileContent = `
  - path: /opt/game/start.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      set -e

      GAME_DIR="/var/lib/libvirt/game"
      ZIP=$(find /var/lib/libvirt -maxdepth 1 -name "*game*" -name "*.zip" -o \
                  -maxdepth 1 -name "*game*" ! -type d | head -1)

      if [ -z "$ZIP" ]; then
        echo "[game] no game zip found in /var/lib/libvirt" >&2
        exit 1
      fi

      echo "[game] extracting $ZIP -> $GAME_DIR"
      mkdir -p "$GAME_DIR"
      unzip -o "$ZIP" -d "$GAME_DIR"

      BIN=$(find "$GAME_DIR" -name "*.x86_64" | head -1)

      if [ -z "$BIN" ]; then
        echo "[game] no .x86_64 binary found after extraction" >&2
        exit 1
      fi

      echo "[game] found binary: $BIN"
      chmod +x "$BIN"

      cd "$(dirname "$BIN")"
      exec "./$( basename "$BIN")" --headless --env-port 8080

  - path: /etc/systemd/system/game-server.service
    content: |
      [Unit]
      Description=Godot Game Server
      After=network-online.target
      Wants=network-online.target

      [Service]
      Type=simple
      Environment="BACKEND_URL={{BACKEND_URL}}"
      ExecStart=/opt/game/start.sh
      StandardOutput=journal
      StandardError=journal
      SyslogIdentifier=game-server
      Restart=on-failure
      RestartSec=5

      [Install]
      WantedBy=multi-user.target
`
		runCmd += "\n  - apt-get update"
		runCmd += "\n  - apt-get install -y libfontconfig1 unzip"
		runCmd += "\n  - chmod +x /opt/game/start.sh"
		runCmd += "\n  - systemctl daemon-reload"
		runCmd += "\n  - systemctl enable game-server"
		runCmd += "\n  - systemctl start game-server"

	case "ai-worker":
		// Download andinstall the agentorget atemplete with the isntealledagent
		// or pickatempletewith teh agent areadyintalled
		userName="sgm"
		profileContent = fmt.Sprintf(`
  - path: /opt/ml/config/minio
    content: |
      MINIO_ENDPOINT="%s"
      MINIO_ACCESS_KEY="%s"
      MINIO_SECRET_KEY="%s"

  - path: /opt/ml/config/nats
    content: |
      NATS_URL="%s"
      NATS_SUBJECT="%s"

  - path: /opt/ml/start.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      set -e
      source /opt/ml/config/minio
      source /opt/ml/config/nats
      
      # Setup directories
      mkdir -p /opt/ml/input/data /opt/ml/output /opt/ml/model /opt/ml/code
      
      # Configure mc (MinIO Client)
      mc alias set local "http://${MINIO_ENDPOINT}" "${MINIO_ACCESS_KEY}" "${MINIO_SECRET_KEY}"
      
      # 1. Download Script
      echo "[SageMaker] Downloading script from {{STORAGE_ARN}}..."
      ARN="{{STORAGE_ARN}}"
      PATH_PART=${ARN#arn:aws:s3:::}
      BUCKET=${PATH_PART%%%%/*}
      KEY=${PATH_PART#*/}
      
      mc cp "local/${BUCKET}/${KEY}" /opt/ml/code/script.zip
      unzip -o /opt/ml/code/script.zip -d /opt/ml/code/
      
      # 2. Download Input Data
      INPUT_URI="{{INPUT_DATA_URI}}"
      if [ ! -z "$INPUT_URI" ] && [ "$INPUT_URI" != "<nil>" ]; then
        echo "[SageMaker] Downloading input data from ${INPUT_URI}..."
        IPATH=${INPUT_URI#arn:aws:s3:::}
        IBUCKET=${IPATH%%%%/*}
        IKEY=${IPATH#*/}
        mc cp -r "local/${IBUCKET}/${IKEY}" /opt/ml/input/data/
      fi
      
      # 3. Export Hyperparameters and Env Vars
      echo "[SageMaker] Setting environment variables..."
      export HYPERPARAMETERS='{{HYPERPARAMETERS}}'
      export BACKEND_URL="{{BACKEND_URL}}"
      export SM_CHANNEL_TRAIN=/opt/ml/input/data
      export SM_MODEL_DIR=/opt/ml/model
      export SM_OUTPUT_DATA_DIR=/opt/ml/output
      
      # 4. Execute Script
      cd /opt/ml/code
      STATUS="COMPLETED"
      if [ -z "{{HEADLESS_BIN}}" ] || [ "{{HEADLESS_BIN}}" == "<nil>" ]; then
        echo "[SageMaker] No HEADLESS_BIN specified. Looking for train.py..."
        if [ -f "train.py" ]; then
          python3 train.py || STATUS="FAILED"
        else
          echo "[SageMaker] ERROR: No executable specified and no train.py found."
          STATUS="FAILED"
        fi
      else
        echo "[SageMaker] Running {{HEADLESS_BIN}}..."
        chmod +x "{{HEADLESS_BIN}}"
        ./"{{HEADLESS_BIN}}" || STATUS="FAILED"
      fi
      
      # 5. Upload Output
      OUTPUT_URI="{{OUTPUT_DATA_URI}}"
      if [ ! -z "$OUTPUT_URI" ] && [ "$OUTPUT_URI" != "<nil>" ]; then
        echo "[SageMaker] Uploading output to ${OUTPUT_URI}..."
        OPATH=${OUTPUT_URI#arn:aws:s3:::}
        OBUCKET=${OPATH%%%%/*}
        OKEY=${OPATH#*/}
        mc cp -r /opt/ml/output/. "local/${OBUCKET}/${OKEY}/"
      fi

      # 6. Notify Completion via NATS
      echo "[SageMaker] Job ${STATUS}. Notifying Orchestrator..."
      JOB_ID="{{JOB_ID}}"
      INSTANCE_ID="__INSTANCE_ID__"
      PAYLOAD="{\"job_id\": ${JOB_ID:-0}, \"instance_id\": \"${INSTANCE_ID}\", \"status\": \"${STATUS}\"}"
      nats -s "${NATS_URL}" pub "${NATS_SUBJECT}" "${PAYLOAD}"
`, l.minioEndpoint, l.minioAK, l.minioSK, l.natsURL, l.natsSubject)

		runCmd += "\n  - apt-get update && apt-get install -y unzip python3-pip"
		runCmd += "\n  - curl https://dl.min.io/client/mc/release/linux-amd64/mc -o /usr/local/bin/mc && chmod +x /usr/local/bin/mc"
		runCmd += "\n  - curl -s https://raw.githubusercontent.com/nats-io/natscli/main/install.sh | sh"
		runCmd += "\n  - systemctl enable ai-worker && systemctl start ai-worker"
	case "rds":
		userName ="rds"
	case "lambda":
		userName ="lambda"
	default:
		// Vanilla profile has no extra files or commands
		fmt.Printf("[Libvirt] Using Vanilla profile for VM %s\n", vmName)
	}

	userData := fmt.Sprintf(`#cloud-config
ssh_pwauth: true
users:
  - name: %s
    plain_text_passwd: "ubuntu!!"
    sudo: ['ALL=(ALL) NOPASSWD:ALL']
    shell: /bin/bash
    lock_passwd: false
    ssh-authorized-keys:
%s
write_files:
%s
%s

runcmd:
%s
`, userName,keysYaml, writeFiles, profileContent, runCmd )

	// ── Replace basic placeholders ───────────────────────────────────────────
	userData = strings.ReplaceAll(userData, "__INSTANCE_ID__", instanceID)
	userData = strings.ReplaceAll(userData, "__IAM_TOKEN__", instanceToken)
	userData = strings.ReplaceAll(userData, "__GATEWAY_IP__", gateway)

	// ── Replace Dynamic Parameters ───────────────────────────────────────────
	for key, value := range params {
		placeholder := fmt.Sprintf("{{%s}}", strings.ToUpper(key))
		userData = strings.ReplaceAll(userData, placeholder, value)
	}

	if err := os.WriteFile(userDataPath, []byte(userData), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write user-data: %w", err)
	}

	// ── meta-data ─────────────────────────────────────────────────────────────
	metaData := fmt.Sprintf(`instance-id: %s
local-hostname: %s
`, vmName, vmName)

	if err := os.WriteFile(metaDataPath, []byte(metaData), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write meta-data: %w", err)
	}

	// ── network-config ────────────────────────────────────────────────────────
	// Interface name is ens3 — this is the KVM/QEMU default interface name.
	// Previously this was enp1s0 which does not exist in this VM configuration,
	// causing cloud-init to skip network setup entirely and leaving the VM with no IP.
	var networkConfig string
	if privateIP != "" && gateway != "" {
		networkConfig = fmt.Sprintf(`version: 2
ethernets:
  ens3:
    dhcp4: false
    addresses:
      - %s/24
    gateway4: %s
    nameservers:
      addresses:
        - 8.8.8.8
        - 8.8.4.4
`, privateIP, gateway)
	} else {
		networkConfig = `version: 2
ethernets:
  ens3:
    dhcp4: true
`
	}

	if err := os.WriteFile(networkCfgPath, []byte(networkConfig), 0600); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write network-config: %w", err)
	}

	cmd := exec.Command("genisoimage",
		"-output", isoPath,
		"-volid", "cidata",
		"-joliet", "-rock",
		"-graft-points",
		"user-data="+userDataPath,
		"meta-data="+metaDataPath,
		"network-config="+networkCfgPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("genisoimage failed: %w\nOutput: %s", err, output)
	}

	fmt.Printf("✓ Cloud-init ISO created: %s (static IP: %s)\n", isoPath, privateIP)

	return isoPath, cleanup, nil
}
func (l *LibvirtClient) getConnection(remoteHostIP, remoteHostUser, remoteHostKey string) (*libvirt.Connect, func(), error) {
	log.Printf("[Libvirt][getConnection] called — remoteHostIP=%q remoteHostUser=%q hasKey=%v",
		remoteHostIP, remoteHostUser, remoteHostKey != "")

	if remoteHostIP == "" {
		log.Printf("[Libvirt][getConnection] no remote IP provided — using local connection")
		return l.conn, func() {}, nil
	}

	if remoteHostUser == "" {
		remoteHostUser = "x6617274696"
		log.Printf("[Libvirt][getConnection] no remote user provided — falling back to default user")
	}

	remoteURI := fmt.Sprintf("qemu+ssh://%s@%s/system?no_verify=1", remoteHostUser, remoteHostIP)
	log.Printf("[Libvirt][getConnection] base URI built: %s", remoteURI)

	var cleanupFuncs []func()

	if remoteHostKey != "" {
		log.Printf("[Libvirt][getConnection] SSH key provided — writing temp key file")

		formattedKey := strings.ReplaceAll(remoteHostKey, "\\n", "\n")
		if !strings.HasSuffix(formattedKey, "\n") {
			formattedKey += "\n"
		}

		f, err := os.CreateTemp("", "id_rsa_*")
		if err != nil {
			log.Printf("[Libvirt][getConnection] [ERROR] failed to create temp key file: %v", err)
			return nil, nil, fmt.Errorf("failed to create temp key file for connection: %w", err)
		}
		log.Printf("[Libvirt][getConnection] temp key file created: %s", f.Name())

		if _, err := f.Write([]byte(formattedKey)); err != nil {
			log.Printf("[Libvirt][getConnection] [ERROR] failed to write temp key file %s: %v", f.Name(), err)
			os.Remove(f.Name())
			return nil, nil, fmt.Errorf("failed to write temp key file for connection: %w", err)
		}
		f.Close()

		if err := os.Chmod(f.Name(), 0600); err != nil {
			log.Printf("[Libvirt][getConnection] [ERROR] failed to chmod temp key file %s: %v", f.Name(), err)
			os.Remove(f.Name())
			return nil, nil, fmt.Errorf("failed to chmod temp key file for connection: %w", err)
		}
		log.Printf("[Libvirt][getConnection] temp key file chmod 0600 OK: %s", f.Name())

		remoteURI += fmt.Sprintf("&keyfile=%s", f.Name())
		log.Printf("[Libvirt][getConnection] final URI with keyfile: %s", remoteURI)

		cleanupFuncs = append(cleanupFuncs, func() {
			log.Printf("[Libvirt][getConnection] cleanup — removing temp key file: %s", f.Name())
			os.Remove(f.Name())
		})
	} else {
		log.Printf("[Libvirt][getConnection] no SSH key provided — connecting without keyfile")
	}

	log.Printf("[Libvirt][getConnection] dialing remote libvirt at %s ...", remoteURI)
	remoteConn, err := libvirt.NewConnect(remoteURI)
	if err != nil {
		log.Printf("[Libvirt][getConnection] [ERROR] failed to connect to remote libvirt at %s: %v", remoteURI, err)
		for _, cf := range cleanupFuncs {
			cf()
		}
		return nil, nil, fmt.Errorf("failed to connect to remote libvirt: %w", err)
	}

	log.Printf("[Libvirt][getConnection] [OK] connected to remote libvirt at %s", remoteURI)

	return remoteConn, func() {
		log.Printf("[Libvirt][getConnection] closing remote libvirt connection to %s", remoteHostIP)
		remoteConn.Close()
		for _, cf := range cleanupFuncs {
			cf()
		}
	}, nil
}

func (l *LibvirtClient) RestartVM(remoteHostIP, remoteHostUser, remoteHostKey, vmName string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	// Lookup the domain by name
	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to find VM %s: %w", vmName, err)
	}
	defer domain.Free()

	// Check if the VM is active (running)
	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check VM state: %w", err)
	}

	if !active {
		return fmt.Errorf("VM is not running")
	}

	// Shutdown the VM gracefully
	if err := domain.Shutdown(); err != nil {
		return fmt.Errorf("failed to shut down VM %s: %w", vmName, err)
	}

	// Wait for the VM to shut down (polling for the active status to become false)
	for {
		active, err := domain.IsActive()
		if err != nil {
			return fmt.Errorf("failed to check VM state: %w", err)
		}

		if !active {
			// VM has shut down
			break
		}

		// Sleep for a short period before checking again
		time.Sleep(1 * time.Second)
	}

	// Start the VM again
	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to restart VM %s: %w", vmName, err)
	}

	return nil
}

func (l *LibvirtClient) StopVM(remoteHostIP, remoteHostUser, remoteHostKey, vmName string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return err
	}
	defer domain.Free()
	return domain.Shutdown()
}

func (l *LibvirtClient) StartVM(remoteHostIP, remoteHostUser, remoteHostKey, vmName string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	// Use name instead of ID
	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to find VM %s: %w", vmName, err)
	}
	defer domain.Free()

	// Check if already running
	active, err := domain.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check VM state: %w", err)
	}

	if active {
		return fmt.Errorf("VM is already running")
	}

	// Start the VM
	if err := domain.Create(); err != nil {
		return fmt.Errorf("failed to start VM: %w", err)
	}

	return nil
}

type ErrorDomain int
type ErrorLevel int
type ErrorNumber int

type Error struct {
	Code    ErrorNumber `json:"code"`
	Domain  ErrorDomain
	Message string
	Level   ErrorLevel
}

func (l *LibvirtClient) DeleteVM(remoteHostIP, remoteHostUser, remoteHostKey, vmName string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {

		return fmt.Errorf("Domain not found")
	}

	// Force stop if running
	domain.Destroy()

	return domain.Undefine()
}

func (l *LibvirtClient) GetPublicIP(vmID int) string {
	// Return NAT port mapping for SSH access
	return fmt.Sprintf("localhost:%d", 2200+vmID)
}
func (l *LibvirtClient) buildVMXML(name, diskPath, isoPath string, cpu, ram int, bridgeName, profile string) string {
	cpuXML := ""
	if profile == "gamelift" || profile == "ai-worker" {
		cpuXML = "<cpu mode='host-passthrough' check='none'/>"
	}
	networkXML := ""
	if bridgeName != "" {
		networkXML = fmt.Sprintf(`
    <interface type='bridge'>
      <source bridge='%s'/>
      <model type='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x03' function='0x0'/>
    </interface>`, bridgeName)
	} else {
		networkXML = `
    <interface type='network'>
      <source network='default'/>
      <model type='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x03' function='0x0'/>
    </interface>`
	}

	return fmt.Sprintf(`
<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os>
    <type arch='x86_64'>hvm</type>
    <boot dev='hd'/>
  </os>
  %s
  <features>
    <acpi/>
    <apic/>
    <pae/>
  </features>
  <clock offset='utc'>
    <timer name='rtc' tickpolicy='catchup'/>
    <timer name='pit' tickpolicy='delay'/>
    <timer name='hpet' present='no'/>
  </clock>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <!-- Main disk -->
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' cache='writeback'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x04' function='0x0'/>
    </disk>
    <!-- Cloud-init ISO -->
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='%s'/>
      <target dev='hdc' bus='ide'/>
      <readonly/>
      <address type='drive' controller='0' bus='1' target='0' unit='0'/>
    </disk>
    <controller type='ide' index='0'>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x01' function='0x1'/>
    </controller>
    <!-- Network with DHCP -->
    %s
    <!-- Serial console -->
    <serial type='pty'>
      <target type='isa-serial' port='0'>
        <model name='isa-serial'/>
      </target>
    </serial>
    <console type='pty'>
      <target type='serial' port='0'/>
    </console>
    <!-- Graphics -->
    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'>
      <listen type='address' address='127.0.0.1'/>
    </graphics>
    <video>
      <model type='vga' vram='16384' heads='1' primary='yes'/>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x02' function='0x0'/>
    </video>
    <memballoon model='virtio'>
      <address type='pci' domain='0x0000' bus='0x00' slot='0x05' function='0x0'/>
    </memballoon>
  </devices>
</domain>
`, name, ram, cpu, cpuXML, diskPath, isoPath, networkXML)
}

// EnsureDefaultNetwork ensures the default network is active and has DHCP
func (l *LibvirtClient) EnsureDefaultNetwork() error {
	network, err := l.conn.LookupNetworkByName("default")
	if err != nil {
		return fmt.Errorf("default network not found: %w. Run: virsh net-start default", err)
	}
	defer network.Free()

	active, err := network.IsActive()
	if err != nil {
		return fmt.Errorf("failed to check network status: %w", err)
	}

	if !active {
		if err := network.Create(); err != nil {
			// Check for specific bridge conflict error
			if strings.Contains(err.Error(), "Network is already in use by interface") {
				return fmt.Errorf("failed to start default network: %w. This is likely a bridge conflict. Try running: 'sudo ip link set virbr0 down && sudo ip link delete virbr0' then restart the service", err)
			}
			return fmt.Errorf("failed to start default network: %w", err)
		}
	}

	// Check autostart
	autostart, err := network.GetAutostart()
	if err == nil && !autostart {
		network.SetAutostart(true)
	}

	return nil
}

// waitForVMIP waits for the VM to get an IP address
func (l *LibvirtClient) waitForVMIP(domain *libvirt.Domain, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		interfaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
		if err == nil {
			for _, iface := range interfaces {
				for _, addr := range iface.Addrs {
					if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 {
						return addr.Addr, nil
					}
				}
			}
		}

		time.Sleep(2 * time.Second)
	}

	return "", fmt.Errorf("timeout waiting for VM IP address")
}

func (l *LibvirtClient) getVMIP(domain *libvirt.Domain) (string, error) {
	ifaces, err := domain.ListAllInterfaceAddresses(libvirt.DOMAIN_INTERFACE_ADDRESSES_SRC_LEASE)
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		for _, addr := range iface.Addrs {
			if addr.Type == libvirt.IP_ADDR_TYPE_IPV4 && !strings.HasPrefix(addr.Addr, "127.") {
				return strings.Split(addr.Addr, "/")[0], nil
			}
		}
	}
	return "", fmt.Errorf("no IP found")
}

func (l *LibvirtClient) CreateSnapshot(remoteHostIP, remoteHostUser, remoteHostKey, vmName, snapshotName, description string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	xml := fmt.Sprintf(`
<domainsnapshot>
  <name>%s</name>
  <description>%s</description>
</domainsnapshot>`, snapshotName, description)

	_, err = domain.CreateSnapshotXML(xml, 0)
	if err != nil {
		return fmt.Errorf("failed to create snapshot for %s: %w", vmName, err)
	}

	return nil
}

func (l *LibvirtClient) DeleteSnapshot(remoteHostIP, remoteHostUser, remoteHostKey, vmName, snapshotName string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	snapshot, err := domain.SnapshotLookupByName(snapshotName, 0)
	if err != nil {
		return fmt.Errorf("failed to lookup snapshot %s for domain %s: %w", snapshotName, vmName, err)
	}
	defer snapshot.Free()

	if err := snapshot.Delete(0); err != nil {
		return fmt.Errorf("failed to delete snapshot %s for domain %s: %w", snapshotName, vmName, err)
	}

	return nil
}

func (l *LibvirtClient) RestoreSnapshot(remoteHostIP, remoteHostUser, remoteHostKey, vmName, snapshotName string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	domain, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("failed to lookup domain %s: %w", vmName, err)
	}
	defer domain.Free()

	snapshot, err := domain.SnapshotLookupByName(snapshotName, 0)
	if err != nil {
		return fmt.Errorf("failed to lookup snapshot %s for domain %s: %w", snapshotName, vmName, err)
	}
	defer snapshot.Free()

	if err := snapshot.RevertToSnapshot(0); err != nil {
		return fmt.Errorf("failed to revert domain %s to snapshot %s: %w", vmName, snapshotName, err)
	}

	return nil
}
func (l *LibvirtClient) runRemoteSSH(remoteHostIP, remoteHostUser, remoteHostKey, command string) error {
	if remoteHostIP == "" {
		// Run locally
		cmd := exec.Command("bash", "-c", command)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("local command failed: %w, output: %s", err, string(output))
		}
		return nil
	}

	if remoteHostUser == "" {
		remoteHostUser = "root"
	}

	formattedKey := strings.ReplaceAll(remoteHostKey, "\\n", "\n")
	if !strings.HasSuffix(formattedKey, "\n") {
		formattedKey += "\n"
	}

	f, err := os.CreateTemp("", "id_rsa_ssh_*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())

	if _, err := f.Write([]byte(formattedKey)); err != nil {
		return err
	}
	f.Close()
	os.Chmod(f.Name(), 0600)

	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		"-i", f.Name(),
		fmt.Sprintf("%s@%s", remoteHostUser, remoteHostIP),
		command,
	}

	cmd := exec.Command("ssh", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remote command failed: %w, output: %s", err, string(output))
	}

	return nil
}
func (l *LibvirtClient) InjectAssets(
	remoteHostIP, remoteHostUser, remoteHostKey, diskPath string,
	assets []domain.AssetConfigs,
) error {

	if len(assets) == 0 {
		return nil
	}

	log.Printf("[Libvirt] Injecting %d assets into disk %s on %s", len(assets), diskPath, remoteHostIP)
	log.Printf("[Libvirt] Injection diskPath %s", diskPath)

	// Build copy commands for each asset
	var copyCommands strings.Builder
	injected := 0

	for _, asset := range assets {
		if asset.Path == "" {
			continue
		}

		src := asset.Path
		dst := filepath.Dir(asset.Path)

		log.Printf("[Libvirt] Injecting asset src=%s dst=%s url=%s sha256=%s **ssetPath=%s",
			src, dst, asset.URL, asset.SHA256, asset.Path,
		)

		copyCommands.WriteString(fmt.Sprintf("mkdir -p \"$MOUNT/%s\"\n", dst))
		copyCommands.WriteString(fmt.Sprintf("cp -r \"%s\" \"$MOUNT/%s/\"\n", src, dst))
		injected++
	}

	if injected == 0 {
		log.Printf("[Libvirt] No valid assets to inject, skipping")
		return nil
	}
	script := fmt.Sprintf(`
set -e

modprobe nbd max_part=8 2>/dev/null || true

NBD="/dev/nbd0"
MOUNT="/mnt/_serwin_inject_$$"
FLAT="/tmp/_serwin_flat_$$.raw"   # RAW — no qcow2 metadata to confuse nbd reads

cleanup() {
    umount "$MOUNT" 2>/dev/null || true
    kpartx -dv "$NBD" 2>/dev/null || true
    qemu-nbd --disconnect "$NBD" 2>/dev/null || true
    rmdir "$MOUNT" 2>/dev/null || true
    rm -f "$FLAT"
}
trap cleanup EXIT

echo "[InjectAssets] Flattening qcow2 -> raw..."
qemu-img convert -f qcow2 -O raw "%s" "$FLAT"
echo "[InjectAssets] Flatten complete: $(du -sh $FLAT | cut -f1)"

# Ensure nbd slot is clean before use
qemu-nbd --disconnect "$NBD" 2>/dev/null || true
sleep 1

mkdir -p "$MOUNT"
qemu-nbd --connect="$NBD" --format=raw "$FLAT"
sleep 3

# Force kernel partition re-read
blockdev --rereadpt "$NBD" 2>/dev/null || true
kpartx -av "$NBD" 2>/dev/null || true
sleep 2

echo "[InjectAssets] Partition layout:"
lsblk "$NBD"

MOUNTED=0
for PART in "/dev/mapper/nbd0p2" "/dev/mapper/nbd0p1" "/dev/mapper/nbd0p3"; do
    if [ -b "$PART" ]; then
        echo "[InjectAssets] Trying $PART..."
        if mount "$PART" "$MOUNT" 2>/dev/null; then
            echo "[InjectAssets] Mounted $PART"
            MOUNTED=1
            break
        fi
    fi
done

if [ "$MOUNTED" -eq 0 ]; then
    echo "[InjectAssets] Failed to mount. fdisk output:"
    fdisk -l "$NBD" 2>/dev/null || true
    exit 1
fi

%s

umount "$MOUNT"
MOUNTED=0

echo "[InjectAssets] Writing back to qcow2..."
kpartx -dv "$NBD" 2>/dev/null || true
qemu-nbd --disconnect "$NBD" 2>/dev/null || true
sleep 1

# Convert modified raw back to qcow2 (replaces original overlay)
qemu-img convert -f raw -O qcow2 "$FLAT" "%s"

echo "[InjectAssets] Injection complete"
`, diskPath, copyCommands.String(), diskPath)

	return l.runRemoteSSH(remoteHostIP, remoteHostUser, remoteHostKey, script)
}
