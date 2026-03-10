package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"middleware/base"
)

type DefaultLogger struct {
	Structured bool
}

func (d DefaultLogger) Debug(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("DEBUG", msg, kv)
	} else {
		fmt.Printf("[DEBUG] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) Info(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("INFO", msg, kv)
	} else {
		fmt.Printf("[INFO] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) Warn(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("WARN", msg, kv)
	} else {
		fmt.Printf("[WARN] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) Error(msg string, kv ...interface{}) {
	if d.Structured {
		d.logStructured("ERROR", msg, kv)
	} else {
		fmt.Printf("[ERROR] "+msg+"\n", kv...)
	}
}

func (d DefaultLogger) logStructured(level, msg string, kv []interface{}) {
	fields := make(map[string]interface{})
	fields["level"] = level
	fields["msg"] = msg
	fields["ts"] = time.Now().UTC().Format(time.RFC3339)

	for i := 0; i < len(kv)-1; i += 2 {
		if key, ok := kv[i].(string); ok {
			fields[key] = kv[i+1]
		}
	}

	json.NewEncoder(os.Stdout).Encode(fields)
}

type NoopMetrics struct{}

func (n NoopMetrics) RateLimitHit(ip, path string)            {}
func (n NoopMetrics) RequestAllowed(ip, path string)          {}
func (n NoopMetrics) BlockApplied(ip string, d time.Duration) {}
func (n NoopMetrics) BotDetected(ip string, reason string)    {}
func (n NoopMetrics) MemoryPressureTriggered(currentMB int64) {}
func (n NoopMetrics) BackendFailure(error string)             {}
func (n NoopMetrics) CircuitBreakerStateChanged(state string) {}
func (n NoopMetrics) DistributedAttackDetected(count int)     {}

func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Ensure interface compliance
var _ base.Logger = DefaultLogger{}
var _ base.Metrics = NoopMetrics{}
