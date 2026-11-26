package config

import "testing"

func TestServiceTargetOverridePreservesOtherDependencies(t *testing.T) {
	t.Setenv("CONFIG_PATH", "config.yaml")
	t.Setenv("SERVICE_TARGETS", "scooter:envoy.scooterflow.svc.cluster.local:10001")
	cfg, err := Read()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Targets["scooter"] != "envoy.scooterflow.svc.cluster.local:10001" || len(cfg.Targets) != 4 {
		t.Fatalf("override lost dependencies: %v", cfg.Targets)
	}
	for service, address := range map[string]string{
		"profile": "dns:///profile-headless:9090",
		"rental":  "rental:9090",
		"pricing": "pricing:9090",
	} {
		if cfg.Targets[service] != address {
			t.Fatalf("unrelated target changed: %s", service)
		}
	}
}
