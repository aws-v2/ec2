package vpcpkg

import (
	"bytes"
	"ec2-api/internal/domain/host"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)


type AgentClient struct {
    httpClient *http.Client
}



func NewAgentClient() *AgentClient {
    return &AgentClient{
        httpClient: &http.Client{
            Timeout: 10 * time.Second,
        },
    }
}


func (a *AgentClient) ReconcileNetwxork(agentIP string, req host.NetworkReconcileRequest) error {

    url := fmt.Sprintf("http://%s:8080/reconcile/network", agentIP)

    body, err := json.Marshal(req)
    if err != nil {
        return err
    }

	fmt.Println("*****s************Reconciling network for host***********", agentIP)

    httpReq, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
    if err != nil {
        return err
    }

    httpReq.Header.Set("Content-Type", "application/json")

    resp, err := a.httpClient.Do(httpReq)
    if err != nil {
        return fmt.Errorf("failed to call agent: %w", err)
    }
	fmt.Println("**************a***Reconciling network for host***********", resp.StatusCode)
    defer resp.Body.Close()

    if resp.StatusCode >= 300 {
        return fmt.Errorf("agent returned non-200: %d", resp.StatusCode)
    }

    return nil
}