package observability

import (
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	CacheVideoDetail = "video_detail"
	CacheFeedLatest  = "feed_latest"
	CacheFeedHot     = "feed_hot"
)

var (
	registerMetricsOnce sync.Once

	cacheL1HitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_l1_hit_total",
			Help: "Total number of cache L1 hits.",
		},
		[]string{"cache"},
	)
	cacheL1MissTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_l1_miss_total",
			Help: "Total number of cache L1 misses.",
		},
		[]string{"cache"},
	)
	cacheL1EvictTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_l1_evict_total",
			Help: "Total number of cache L1 evictions.",
		},
		[]string{"cache"},
	)
	cacheL2HitTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_l2_hit_total",
			Help: "Total number of cache L2 hits.",
		},
		[]string{"cache"},
	)
	cacheL2MissTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_l2_miss_total",
			Help: "Total number of cache L2 misses.",
		},
		[]string{"cache"},
	)
	cacheSFSharedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_sf_shared_total",
			Help: "Total number of shared singleflight loads.",
		},
		[]string{"cache"},
	)
	cacheInvalidationTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "cache_invalidation_total",
			Help: "Total number of cache invalidations.",
		},
		[]string{"cache", "target", "source"},
	)
	cacheLoadDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "cache_load_duration_seconds",
			Help:    "Latency of cache misses loaded from source of truth.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"cache"},
	)
)

func NewMetricsHandler() http.Handler {
	registerMetrics()
	return promhttp.Handler()
}

func IncCacheL1Hit(cacheName string) {
	registerMetrics()
	cacheL1HitTotal.WithLabelValues(cacheName).Inc()
}

func IncCacheL1Miss(cacheName string) {
	registerMetrics()
	cacheL1MissTotal.WithLabelValues(cacheName).Inc()
}

func IncCacheL1Evict(cacheName string) {
	registerMetrics()
	cacheL1EvictTotal.WithLabelValues(cacheName).Inc()
}

func IncCacheL2Hit(cacheName string) {
	registerMetrics()
	cacheL2HitTotal.WithLabelValues(cacheName).Inc()
}

func IncCacheL2Miss(cacheName string) {
	registerMetrics()
	cacheL2MissTotal.WithLabelValues(cacheName).Inc()
}

func IncCacheSingleflightShared(cacheName string) {
	registerMetrics()
	cacheSFSharedTotal.WithLabelValues(cacheName).Inc()
}

func IncCacheInvalidation(cacheName string, target string, source string) {
	registerMetrics()
	cacheInvalidationTotal.WithLabelValues(cacheName, target, source).Inc()
}

func ObserveCacheLoadSeconds(cacheName string, seconds float64) {
	registerMetrics()
	cacheLoadDurationSeconds.WithLabelValues(cacheName).Observe(seconds)
}

func registerMetrics() {
	registerMetricsOnce.Do(func() {
		prometheus.MustRegister(
			cacheL1HitTotal,
			cacheL1MissTotal,
			cacheL1EvictTotal,
			cacheL2HitTotal,
			cacheL2MissTotal,
			cacheSFSharedTotal,
			cacheInvalidationTotal,
			cacheLoadDurationSeconds,
		)
	})
}
