
func (a *Agent) reportMetrics() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	controlPlane := a.cfg.ControlPlaneURL

	for range ticker.C {
		// In a real scenario, we'd use 'shirou/gopsutil' or similar.
		// For now, we'll provide simulated but realistic metrics reporting.
		ip, _ := getHostIP()
		
		metrics := map[string]interface{}{
			"host_id":    a.cfg.NodeID,
			"hostname":   a.cfg.NodeID,
			"ip":         ip,
			"cpu_total":  8,
			"cpu_used":   2,
			"ram_total":  16384,
			"ram_free":   12288,
			"disk_total": 500,
			"disk_free":  450,
		}

		jsonData, err := json.Marshal(metrics)
		if err != nil {
			log.Printf("[agent] failed to marshal metrics: %v", err)
			continue
		}

		url := fmt.Sprintf("%s/api/v1/compute/hosts/heartbeat", controlPlane)
		
		req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
		if err != nil {
			log.Printf("[agent] failed to create request: %v", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Api-Key", a.cfg.APIKey)

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("[agent] failed to post metrics to %s: %v", url, err)
			continue
		}
		resp.Body.Close()
		log.Printf("[agent] reported metrics to %s status=%d", url, resp.StatusCode)
	}
}


