package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFeederDogInterval(t *testing.T) {
	// Default interval
	if got := feederDogInterval(nil); got != defaultFeederDogInterval {
		t.Errorf("expected default interval %v, got %v", defaultFeederDogInterval, got)
	}

	// Custom interval
	config := &DaemonPatrolConfig{
		Patrols: &PatrolsConfig{
			FeederDog: &FeederDogConfig{
				Enabled:     true,
				IntervalStr: "2m",
			},
		},
	}
	if got := feederDogInterval(config); got != 2*time.Minute {
		t.Errorf("expected 2m interval, got %v", got)
	}

	// Invalid interval falls back to default
	config.Patrols.FeederDog.IntervalStr = "invalid"
	if got := feederDogInterval(config); got != defaultFeederDogInterval {
		t.Errorf("expected default interval for invalid config, got %v", got)
	}
}

func TestIsPatrolEnabled_FeederDog(t *testing.T) {
	// Nil config: disabled (opt-in patrol)
	if IsPatrolEnabled(nil, "feeder_dog") {
		t.Error("expected feeder_dog to be disabled with nil config")
	}

	// Empty patrols: disabled
	config := &DaemonPatrolConfig{
		Patrols: &PatrolsConfig{},
	}
	if IsPatrolEnabled(config, "feeder_dog") {
		t.Error("expected feeder_dog to be disabled by default")
	}

	// Explicitly enabled
	config.Patrols.FeederDog = &FeederDogConfig{Enabled: true}
	if !IsPatrolEnabled(config, "feeder_dog") {
		t.Error("expected feeder_dog to be enabled when configured")
	}

	// Explicitly disabled
	config.Patrols.FeederDog = &FeederDogConfig{Enabled: false}
	if IsPatrolEnabled(config, "feeder_dog") {
		t.Error("expected feeder_dog to be disabled when explicitly disabled")
	}
}

func TestFeederDogConfigJSON(t *testing.T) {
	jsonData := `{"enabled": true, "interval": "3m"}`

	var config FeederDogConfig
	if err := json.Unmarshal([]byte(jsonData), &config); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if !config.Enabled {
		t.Error("expected enabled=true")
	}
	if config.IntervalStr != "3m" {
		t.Errorf("expected interval=3m, got %s", config.IntervalStr)
	}
}

func TestFeederDogDefaultConstants(t *testing.T) {
	if defaultFeederDogInterval != 5*time.Minute {
		t.Errorf("expected default interval 5m, got %v", defaultFeederDogInterval)
	}
	if feederDogTimeout != 2*time.Minute {
		t.Errorf("expected timeout 2m, got %v", feederDogTimeout)
	}
}
