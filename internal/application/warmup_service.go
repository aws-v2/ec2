package application

import (
	"bytes"
	"context"
	domain "ec2-api/internal/domain/instance"
	interfaces "ec2-api/internal/interfaces"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
)

type WarmupService struct {
	repo            interfaces.InstanceRepository
	hostRepo        interfaces.HostRepository
	instanceService InstanceService
	systemUserId    string
}

func NewWarmupService(
	repo interfaces.InstanceRepository,
	hostRepo interfaces.HostRepository,
	instanceService InstanceService,
	systemUserId string,
) *WarmupService {
	return &WarmupService{
		repo:            repo,
		hostRepo:        hostRepo,
		instanceService: instanceService,
		systemUserId:    systemUserId,
	}
}

func (w *WarmupService) GetWarmLambdaVMs() ([]string, error) {
	fmt.Println("=========Warming lambda vms =============")

	hosts, err := w.hostRepo.GetBestHostsByType(5, "lambda")
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}
	log.Printf("checkpoint1")

	var allDomains []string

	fmt.Printf("sagemeker hosts found: %v", len(hosts))

	for _, host := range hosts {
		fmt.Printf("calling sagemeker hosts found: %v", host.ID)

		url := fmt.Sprintf("http://%s:%d/warm-vms", host.IP, 9030)

		resp, err := http.Get(url)
		if err != nil {
			log.Printf("failed to reach host %s: %v", host.IP, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("bad status from host %s: %s", host.IP, resp.Status)
			resp.Body.Close()
			continue
		}

		var domains []string
		if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
			log.Printf("failed to decode domains from host %s: %v", host.IP, err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()

		if len(domains) == 0 || !specificDomains(domains, "lambda") { //orthe doainsthat arereturnedthere is nodomwain that starts with "lambda"
			resourceId := uuid.New().String()
			instanceRequest := domain.CreateInstanceRequest{
				Profile:    "lambda",
				Name:       fmt.Sprintf("warmup-lambda-%s", resourceId),
				ResourceID: resourceId,
				Image:      "ubuntu-22.04",
				Specs: domain.VMSpecs{
					CPU:     1,
					RAM:     1024,
					Storage: 10,
				},
				SessionID:      uuid.New().String(),
				ForwardingPort: 9087,
			}
			_, err := w.instanceService.CreateInstance(context.Background(), &instanceRequest, w.systemUserId)
			if err != nil {
				log.Printf("failed to provision warmup vm for lambda on host %s: %v", host.IP, err)
			}
		}

		allDomains = append(allDomains, domains...)
	}
	log.Printf("checkpoint2 %v", allDomains)

	return allDomains, nil
}

func specificDomains(allDomains []string, domainType string) bool {
	for _, domain := range allDomains {
		if strings.Contains(domain, domainType) {
			return true
		}

	}
	return false
}

func GetWarmLambdaVMs(w *WarmupService) ([]string, error) {

	return w.GetWarmLambdaVMs()
}

func (w *WarmupService) GetWarmSageMakerVMs() ([]string, error) {
	fmt.Println("=========Warming SGM vms =============")

	hosts, err := w.hostRepo.GetBestHostsByType(5, "sagemaker")
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}

	var allDomains []string

	fmt.Printf("sagemeker hosts found: %v", len(hosts))

	for _, host := range hosts {
		fmt.Printf("calling sagemeker hosts found: %v", host.ID)
		url := fmt.Sprintf("http://%s:%d/warm-vms", host.IP, 9030)

		resp, err := http.Get(url)
		if err != nil {
			log.Printf("failed to reach host %s: %v", host.IP, err)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("bad status from host %s: %s", host.IP, resp.Status)
			resp.Body.Close()
			continue
		}

		var domains []string
		if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
			log.Printf("failed to decode domains from host %s: %v", host.IP, err)
			resp.Body.Close()
			continue
		}
		resp.Body.Close()
		log.Printf(" %s: %v", host.IP, err)

		if len(domains) == 0 || !specificDomains(domains, "sagemaker") {
			resourceId := uuid.New().String()
			instanceRequest := domain.CreateInstanceRequest{
				Profile:    "sagemaker",
				Name:       fmt.Sprintf("warmup-sagemaker-%s", resourceId),
				ResourceID: resourceId,
				Image:      "ubuntu-22.04",
				Specs: domain.VMSpecs{
					CPU:     1,
					RAM:     1024,
					Storage: 10,
				},
				SessionID:      uuid.New().String(),
				ForwardingPort: 9087,
			}
			_, err := w.instanceService.CreateInstance(context.Background(), &instanceRequest, w.systemUserId)
			if err != nil {
				log.Printf("failed to provision warmup vm for sagemaker on host %s: %v", host.IP, err)
			}
		}

		allDomains = append(allDomains, domains...)
	}

	return allDomains, nil
}

func GetWarmSageMakerVMs(w *WarmupService) ([]string, error) {

	return w.GetWarmSageMakerVMs()
}

func (w *WarmupService) PurgeWarmLambdaVMs() {
	ctx := context.Background()
	vms, err := w.repo.FindUnreachableVms(ctx)
	if err != nil {
		fmt.Printf("failed to get hosts: %v\n", err)
		return
	}

	var toRemove []*domain.Instance

	for _, vm := range vms {
		if !pingHost(vm.IP) {
			toRemove = append(toRemove, vm)
		}
	}

	if len(toRemove) == 0 {
		fmt.Println("PurgeWarmLambdaVMs: no unreachable VMs found")
		return
	}

	for _, vm := range toRemove {
		host, err := w.hostRepo.GetByID(vm.HostID)
		if err != nil {
			fmt.Printf("failed to find host %s for vm %s: %v\n", vm.HostID, vm.VMName, err)
			continue
		}

		if err := sendDestroyCommand(host.IP, vm.ID); err != nil {
			fmt.Printf("failed to destroy vm %s on host %s: %v\n", vm.VMName, host.IP, err)
			continue
		}

		fmt.Printf("Marking VM for removal: %s (%s)\n", vm.VMName, vm.IP)

		if err := w.repo.Delete(vm.ID); err != nil {
			fmt.Printf("failed to remove vm %s from db: %v\n", vm.VMName, err)
			continue
		}

		fmt.Printf("VM removed from db: %s\n", vm.VMName)
	}

}

// sendDestroyCommand posts a destroy action to the agent running on hostIP:9030
// and returns an error unless the agent responds with 202 Accepted.
func sendDestroyCommand(hostIP string, vmID string) error {
	url := fmt.Sprintf("http://%s:9030/vm/power", hostIP)

	body, err := json.Marshal(VMPowerRequest{
		VMID:   vmID,
		Action: "destroy",
	})
	if err != nil {
		return fmt.Errorf("failed to marshal request body: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to reach agent at %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("agent returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

// VMPowerRequest mirrors the agent's expected request body.
type VMPowerRequest struct {
	VMID   string `json:"vm_id"`
	Action string `json:"action"`
}

// pingHost sends a single ICMP ping with a 1-second timeout and reports
// whether the host responded. Uses the system `ping` binary since raw
// ICMP sockets require elevated privileges.
func pingHost(ip string) bool {
	if ip == "" {
		return false
	}

	// -c 1: send 1 packet, -W 1: wait max 1 second for a reply (Linux)
	cmd := exec.CommandContext(context.Background(), "ping", "-c", "1", "-W", "1", ip)
	err := cmd.Run()
	return err == nil
}
