package agent

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

func startMetricsServer(addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		log.Info().Str("addr", addr).Msg("metrics server listening")
		if err := http.ListenAndServe(addr, mux); err != nil {
			log.Error().Err(err).Msg("metrics server failed")
		}
	}()
}
