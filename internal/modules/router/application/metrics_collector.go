package application

import (
	"strings"
	"sync/atomic"

	"hyperstrate/server/internal/shared/metrics"
)

// Collect implements metrics.Collector. It emits Prometheus-format counters
// and gauges for every router seen since process start.
func (m *pipelineMetrics) Collect(buf *strings.Builder) {
	m.routers.Range(func(key, val any) bool {
		routerID := key.(string)
		s := val.(*routerStats)

		s.counts.Range(func(k, v any) bool {
			metrics.WriteCounter(buf,
				"router_requests_total",
				"Total requests processed by the router pipeline, by router and status.",
				map[string]string{"router_id": routerID, "status": k.(string)},
				v.(*atomic.Int64).Load(),
			)
			return true
		})

		s.latency.Lock()
		avg := float64(0)
		total := s.totalReq
		if total > 0 {
			avg = s.totalMs / float64(total)
		}
		s.latency.Unlock()

		metrics.WriteGauge(buf,
			"router_avg_latency_ms",
			"Average request latency in milliseconds for the router pipeline.",
			map[string]string{"router_id": routerID},
			avg,
		)
		metrics.WriteCounter(buf,
			"router_total_requests",
			"Total number of requests handled by this router.",
			map[string]string{"router_id": routerID},
			total,
		)
		return true
	})
}
