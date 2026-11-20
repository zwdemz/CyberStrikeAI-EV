package config

import (
	"math"
	"testing"
)

func TestAuditTemperatureValidation(t *testing.T) {
	for _, value := range []float64{-1, 2.1, math.NaN(), math.Inf(1), 0, 0.6, 2} {
		cfg := HitlConfig{AuditModel: OpenAIConfig{Temperature: &value}}
		invalid := math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 2
		if (cfg.ValidateAuditTemperature() != nil) != invalid {
			t.Errorf("value %v", value)
		}
	}
	mainValue := 1.0
	if (HitlConfig{}).AuditModelEffective(OpenAIConfig{Temperature: &mainValue}).Temperature != nil {
		t.Fatal("audit inherited main temperature")
	}
}
