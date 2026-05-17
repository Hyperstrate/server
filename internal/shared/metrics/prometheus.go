// Package metrics provides a Prometheus-compatible text exposition endpoint.
// It uses the standard Prometheus text format (no external library required):
//
//	# HELP metric_name Description of metric.
//	# TYPE metric_name counter
//	metric_name{label="value"} 42
//
// Prometheus can scrape GET /metrics and parse this format natively.
package metrics

import (
	"fmt"
	"net/http"
	"strings"
)

// Collector is implemented by any module that can emit Prometheus-format metrics.
type Collector interface {
	// Collect appends metric lines in Prometheus text exposition format to buf.
	Collect(buf *strings.Builder)
}

// Handler returns a Gin-compatible http.Handler that aggregates all registered
// collectors and writes them in Prometheus text exposition format.
func Handler(collectors ...Collector) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var buf strings.Builder
		for _, c := range collectors {
			c.Collect(&buf)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, buf.String())
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// WriteCounter writes a counter metric block.
//
//	# HELP name help
//	# TYPE name counter
//	name{...} value
func WriteCounter(buf *strings.Builder, name, help string, labels map[string]string, value int64) {
	buf.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
	buf.WriteString(fmt.Sprintf("# TYPE %s counter\n", name))
	buf.WriteString(fmt.Sprintf("%s%s %d\n", name, formatLabels(labels), value))
}

// WriteGauge writes a gauge metric block.
func WriteGauge(buf *strings.Builder, name, help string, labels map[string]string, value float64) {
	buf.WriteString(fmt.Sprintf("# HELP %s %s\n", name, help))
	buf.WriteString(fmt.Sprintf("# TYPE %s gauge\n", name))
	buf.WriteString(fmt.Sprintf("%s%s %g\n", name, formatLabels(labels), value))
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf(`%s=%q`, k, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}
