package metrics

import (
	"runtime"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/tjhop/prometheus-mcp-server/internal/version"
)

const (
	MetricNamespace = "prom_mcp" // 指标命名空间前缀
)

var (
	once     sync.Once              // 确保 Registry 只初始化一次
	Registry *prometheus.Registry   // Prometheus 指标注册表
)

func init() {
	once.Do(func() {
		Registry = prometheus.NewRegistry()

		// 创建构建信息指标
		metricBuildInfo := prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Name: prometheus.BuildFQName(MetricNamespace, "build", "info"),
				Help: "一个值恒为 '1' 的指标，带有版本号、提交哈希和构建日期标签，用于标识 prometheus-mcp-server 的构建信息。",
				ConstLabels: prometheus.Labels{
					"version":    version.Version,
					"commit":     version.Commit,
					"build_date": version.BuildDate,
					"goversion":  runtime.Version(),
				},
			},
			func() float64 { return 1 },
		)

		Registry.MustRegister(
			// 添加标准的进程/Go 运行时指标到注册表
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			collectors.NewGoCollector(),
			// 注册构建信息指标
			metricBuildInfo,
		)
	})
}
