package application

import (
	"context"
	domain "ec2-api/internal/domain/instance"
	interfaces "ec2-api/internal/interfaces"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

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
	hosts, err := w.hostRepo.GetBestHostsByType(5, "lambda")
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}
			log.Printf("checkpoint1")

	var allDomains []string

	for _, host := range hosts {

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

		if len(domains) == 0 {
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
				SessionID: uuid.New().String(),
				ForwardingPort:9087,

			}
			_, err := w.instanceService.CreateInstance(context.Background(), &instanceRequest, w.systemUserId)
			if err != nil {
				log.Printf("failed to provision warmup vm for lambda on host %s: %v", host.IP, err)
			}
		}

		allDomains = append(allDomains, domains...)
	}
			log.Printf("checkpoint2 %v",allDomains)

	return allDomains, nil
}

func GetWarmLambdaVMs(w *WarmupService) ([]string, error) {

	return w.GetWarmLambdaVMs()
}

func (w *WarmupService) GetWarmSageMakerVMs() ([]string, error) {
	hosts, err := w.hostRepo.GetBestHostsByType(5, "sagemaker")
	if err != nil {
		return nil, fmt.Errorf("failed to get hosts: %w", err)
	}

	var allDomains []string

	for _, host := range hosts {
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

		if len(domains) == 0 {
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
				SessionID: uuid.New().String(),
				ForwardingPort:9087,
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
