package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	namespace = "webrtc"

	roomNameLabel = "room_name"
	endpointLabel = "endpoint"
)

type Metrics struct {
	Reg                          *prometheus.Registry
	WebRTCConnectionCreationTime *prometheus.HistogramVec
	StreamResolution             *prometheus.GaugeVec
	RPS                          *prometheus.CounterVec
	RequestDuration              *prometheus.HistogramVec
}

func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		Reg: reg,
		WebRTCConnectionCreationTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "webrtc_connection_creation_time",
			Buckets:   []float64{0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		}, []string{roomNameLabel}),
		StreamResolution: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "stream_resolution",
		}, []string{roomNameLabel}),
		RPS: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "requests_per_second",
		}, []string{endpointLabel}),
		RequestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "request_duration",
			Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.8, 1, 2},
		}, []string{endpointLabel}),
	}

	reg.MustRegister(m.WebRTCConnectionCreationTime)
	reg.MustRegister(m.StreamResolution)
	reg.MustRegister(m.RPS)
	reg.MustRegister(m.RequestDuration)

	return m
}
