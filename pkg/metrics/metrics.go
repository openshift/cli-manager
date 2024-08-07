package metrics

import (
	"sync"

	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

var registerControllerMetrics sync.Once

var (
	GitAPIRequestCounts = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Name:           "cli_manager_git_api_requests_total",
			Help:           "Total counts of Git API requests",
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"name"},
	)
)

func init() {
	registerControllerMetrics.Do(func() {
		legacyregistry.MustRegister(GitAPIRequestCounts)
	})
}
