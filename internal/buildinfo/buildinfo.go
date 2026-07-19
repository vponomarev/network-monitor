package buildinfo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// Info identifies the exact binary currently serving requests.
type Info struct {
	Service   string `json:"service"`
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

// New returns build information enriched with the runtime toolchain/platform.
func New(service, version, gitCommit, buildTime string) Info {
	return Info{
		Service:   service,
		Version:   version,
		GitCommit: gitCommit,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
}

// WriteText prints a human-readable representation for --version.
func (i *Info) WriteText(w io.Writer) error {
	_, err := fmt.Fprintf(w, "%s %s (commit=%s, built=%s, go=%s, %s/%s)\n",
		i.Service, i.Version, i.GitCommit, i.BuildTime, i.GoVersion, i.GOOS, i.GOARCH)
	return err
}

// Handler exposes build information for the running process.
func (i *Info) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(i); err != nil {
			http.Error(w, "encoding response", http.StatusInternalServerError)
		}
	})
}

// Collector returns a constant build_info metric for the running process.
func (i *Info) Collector() prometheus.Collector {
	return prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: i.Service,
		Name:      "build_info",
		Help:      "Build information for the running process.",
		ConstLabels: prometheus.Labels{
			"version":    i.Version,
			"git_commit": i.GitCommit,
			"build_time": i.BuildTime,
			"go_version": i.GoVersion,
		},
	}, func() float64 { return 1 })
}
