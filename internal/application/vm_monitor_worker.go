package application

import (
	"context"
	"log"
	"time"

	domain "ec2-api/internal/domain/host"
)

type VMMonitorWorker struct {
	metricsRepo domain.MetricsRepository
	hostService *HostService
	ticker      *time.Ticker
	stopChan    chan struct{}
}

func NewVMMonitorWorker(metricsRepo domain.MetricsRepository, hostService *HostService) *VMMonitorWorker {
	return &VMMonitorWorker{
		metricsRepo: metricsRepo,
		hostService: hostService,
		stopChan:    make(chan struct{}),
	}
}

func (w *VMMonitorWorker) Start(interval time.Duration) {
	w.ticker = time.NewTicker(interval)
	log.Printf("[VMMonitorWorker] Starting worker with interval %v", interval)

	// Run initial check immediately
	go w.checkVMs()

	go func() {
		for {
			select {
			case <-w.ticker.C:
				w.checkVMs()
			case <-w.stopChan:
				w.ticker.Stop()
				return
			}
		}
	}()
}

func (w *VMMonitorWorker) Stop() {
	close(w.stopChan)
}

func (w *VMMonitorWorker) checkVMs() {
	log.Printf("[VMMonitorWorker] Checking VMs for inactivity matching policy...")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	// 30 minutes for sleep, 1 hour for terminate
	targets, err := w.metricsRepo.GetVMsRequiringAction(ctx, 1*time.Minute, 1*time.Hour)
	if err != nil {
		log.Printf("[VMMonitorWorker] Error getting VMs requiring action: %v", err)
		return
	}

	for _, target := range targets {
		log.Printf("[VMMonitorWorker] VM %s on host %s requires action: %s", target.VMID, target.HostIP, target.Action)
		
		err := w.hostService.SendPowerAction(ctx, target.HostIP, target.VMID, target.Action)
		if err != nil {
			log.Printf("[VMMonitorWorker] Error sending action %s to VM %s: %v", target.Action, target.VMID, err)
			continue
		}
		
		log.Printf("[VMMonitorWorker] Successfully sent %s signal to VM %s", target.Action, target.VMID)
	}
}
