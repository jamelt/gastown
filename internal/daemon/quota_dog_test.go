package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestQuotaDogInterval(t *testing.T) {
	// Default interval
	if got := quotaDogInterval(nil); got != defaultQuotaDogInterval {
		t.Errorf("expected default interval %v, got %v", defaultQuotaDogInterval, got)
	}

	// Custom interval
	config := &DaemonPatrolConfig{
		Patrols: &PatrolsConfig{
			QuotaDog: &QuotaDogConfig{
				Enabled:     true,
				IntervalStr: "2m",
			},
		},
	}
	if got := quotaDogInterval(config); got != 2*time.Minute {
		t.Errorf("expected 2m interval, got %v", got)
	}

	// Invalid interval falls back to default
	config.Patrols.QuotaDog.IntervalStr = "invalid"
	if got := quotaDogInterval(config); got != defaultQuotaDogInterval {
		t.Errorf("expected default interval for invalid config, got %v", got)
	}
}

func TestIsPatrolEnabled_QuotaDog(t *testing.T) {
	// Nil config: disabled (opt-in patrol)
	if IsPatrolEnabled(nil, "quota_dog") {
		t.Error("expected quota_dog to be disabled with nil config")
	}

	// Empty patrols: disabled
	config := &DaemonPatrolConfig{
		Patrols: &PatrolsConfig{},
	}
	if IsPatrolEnabled(config, "quota_dog") {
		t.Error("expected quota_dog to be disabled by default")
	}

	// Explicitly enabled
	config.Patrols.QuotaDog = &QuotaDogConfig{Enabled: true}
	if !IsPatrolEnabled(config, "quota_dog") {
		t.Error("expected quota_dog to be enabled when configured")
	}

	// Explicitly disabled
	config.Patrols.QuotaDog = &QuotaDogConfig{Enabled: false}
	if IsPatrolEnabled(config, "quota_dog") {
		t.Error("expected quota_dog to be disabled when explicitly disabled")
	}
}

func TestQuotaDogConfigJSON(t *testing.T) {
	jsonData := `{"enabled": true, "interval": "3m"}`

	var config QuotaDogConfig
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

func TestQuotaDogDefaultConstants(t *testing.T) {
	if defaultQuotaDogInterval != 5*time.Minute {
		t.Errorf("expected default interval 5m, got %v", defaultQuotaDogInterval)
	}
	if quotaDogTimeout != 2*time.Minute {
		t.Errorf("expected timeout 2m, got %v", quotaDogTimeout)
	}
}

func TestQuotaDogRunsAccountRotationBeforeProviderFailover(t *testing.T) {
	actions := quotaDogActions()
	if len(actions) != 2 || actions[0] != "rotate" || actions[1] != "failover" {
		t.Fatalf("quota dog actions = %v, want [rotate failover]", actions)
	}
}

func TestQuotaDogFailureEscalation(t *testing.T) {
	d := &Daemon{}

	if got := d.getQuotaDogFailures("rotate"); got != 0 {
		t.Fatalf("expected 0 failures before any recorded, got %d", got)
	}

	for i := 1; i < quotaDogFailureEscalationThreshold; i++ {
		d.recordQuotaDogFailure("rotate")
		if got := d.getQuotaDogFailures("rotate"); got != i {
			t.Fatalf("expected %d consecutive failures, got %d", i, got)
		}
	}

	// One more failure should reach the escalation threshold.
	d.recordQuotaDogFailure("rotate")
	if got := d.getQuotaDogFailures("rotate"); got != quotaDogFailureEscalationThreshold {
		t.Fatalf("expected %d consecutive failures, got %d", quotaDogFailureEscalationThreshold, got)
	}

	// A different action tracks independently.
	if got := d.getQuotaDogFailures("failover"); got != 0 {
		t.Fatalf("expected failover to be unaffected by rotate failures, got %d", got)
	}

	// Success resets the counter.
	d.resetQuotaDogFailures("rotate")
	if got := d.getQuotaDogFailures("rotate"); got != 0 {
		t.Fatalf("expected 0 failures after reset, got %d", got)
	}
}
