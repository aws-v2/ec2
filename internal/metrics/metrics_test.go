package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestOperationMetricsAreRegisteredAndRecorded(t *testing.T) {
	const profile = "metrics-test-profile"
	RecordInstanceCreate(profile, "success", 12*time.Second)
	RecordAMIBuild("failed", 45*time.Second)
	SetAMIBuildProgress("metrics-test-ami", 42)
	RecordAgentCallError("metrics-test-action", "metrics-test-host")
	defer ClearAMIBuildProgress("metrics-test-ami")

	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	familyByName := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		familyByName[family.GetName()] = family
	}

	instanceCreate := familyByName["ec2_instance_create_total"]
	if instanceCreate == nil || !hasLabels(instanceCreate, map[string]string{
		"profile": profile,
		"result":  "success",
	}) {
		t.Fatal("instance create counter missing expected labels")
	}

	amiBuild := familyByName["ec2_ami_build_total"]
	if amiBuild == nil || !hasLabels(amiBuild, map[string]string{"result": "failed"}) {
		t.Fatal("AMI build counter missing expected result")
	}

	progress := familyByName["ec2_ami_build_progress_percent"]
	if progress == nil || !hasLabels(progress, map[string]string{"ami_id": "metrics-test-ami"}) {
		t.Fatal("AMI build progress gauge missing expected AMI")
	}

	agentErrors := familyByName["ec2_agent_call_errors_total"]
	if agentErrors == nil || !hasLabels(agentErrors, map[string]string{
		"action": "metrics-test-action",
		"host":   "metrics-test-host",
	}) {
		t.Fatal("agent error counter missing expected labels")
	}

	if familyByName["ec2_instance_create_duration_seconds"] == nil {
		t.Fatal("instance create duration histogram is not registered")
	}
	if familyByName["ec2_ami_build_duration_seconds"] == nil {
		t.Fatal("AMI build duration histogram is not registered")
	}
}

func hasLabels(family *dto.MetricFamily, want map[string]string) bool {
	for _, metric := range family.GetMetric() {
		matches := true
		for key, value := range want {
			found := false
			for _, label := range metric.GetLabel() {
				if label.GetName() == key && label.GetValue() == value {
					found = true
					break
				}
			}
			if !found {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
