package server

import (
	"net/http"
	"time"

	lynxhttp "github.com/lynx-go/lynx/server/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/torchwooddev/torchwood/internal/pkg/config"
)

type MetricsServer struct {
	*lynxhttp.Server
}

func NewMetricsServer(cfg *config.AppConfig) (*MetricsServer, error) {
	addr := cfg.GetServer().GetMetrics().GetAddr()
	if addr == "" {
		addr = ":9100"
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := lynxhttp.NewServer(mux, lynxhttp.WithAddr(addr), lynxhttp.WithTimeout(30*time.Second))
	return &MetricsServer{srv}, nil
}
