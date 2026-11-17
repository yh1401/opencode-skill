package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aiops-platform/backend/internal/alarm"
	"aiops-platform/backend/internal/api"
	"aiops-platform/backend/internal/audit"
	"aiops-platform/backend/internal/auth"
	"aiops-platform/backend/internal/changes"
	"aiops-platform/backend/internal/config"
	"aiops-platform/backend/internal/diagnose"
	"aiops-platform/backend/internal/domain"
	"aiops-platform/backend/internal/identity"
	"aiops-platform/backend/internal/inspect"
	"aiops-platform/backend/internal/knowledge"
	"aiops-platform/backend/internal/mcp"
	"aiops-platform/backend/internal/relations"
	"aiops-platform/backend/internal/store"
	"aiops-platform/backend/internal/tickets"
	"aiops-platform/backend/internal/topo"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	cfg := config.Load()
	log.Printf("[boot] %s", cfg.String())

	tp, err := buildProvider(cfg)
	if err != nil {
		log.Fatalf("[boot] 拓扑数据源初始化失败: %v", err)
	}
	st, err := store.NewFileStore(cfg.StoreDir, cfg.PostgresDSN)
	if err != nil {
		log.Fatalf("[boot] 存储初始化失败: %v", err)
	}
	gw := mcp.NewGateway(cfg, st)
	reg := identity.New(tp, st, gw, cfg)
	ie := inspect.New(tp, st, cfg, gw, reg)
	de := diagnose.New(tp, st, gw, cfg, reg)
	ce := changes.New(tp, de, st)
	te := tickets.New(de, st)
	ae := audit.New(tp, gw, st, reg)
	al := alarm.New(cfg, gw, st, tp, de, te, reg)
	rel := relations.New(tp, st)
	kb := knowledge.New(de, st, knowledge.NewRanker(cfg.LLMAPIKey, cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMTimeout))
	asvc := auth.NewService(cfg, st)
	api.EnsureDefaultTask(st, cfg)

	if !cfg.AuthEnabled {
		log.Printf("[auth] 警告：AUTH_ENABLED=false，所有请求按内置管理员放行，仅可用于本地演示")
	} else {
		log.Printf("[auth] enabled ttl=%s init_admin=%s", cfg.AuthTokenTTL, cfg.AuthInitAdminUser)
	}

	// 身份映射：启动时异步重建（拓扑 + Pinpoint + CMDB），失败不阻断服务启动。
	if reg.Enabled() && cfg.IdentityRebuildOnBoot {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			stats, err := reg.Rebuild(ctx, "", "")
			if err != nil {
				log.Printf("[identity] rebuild failed: %v", err)
				return
			}
			log.Printf("[identity] rebuilt apps=%d hosts=%d pinpoint=%d/%d cmdb=%d in %dms",
				stats.Apps, stats.Hosts, stats.PinpointMatched, stats.PinpointTotal, stats.CMDBRecords, stats.DurationMs)
		}()
	}

	// T9：巡检 RISK 结论发布到告警中心（同一张告警表，复用去重 / 自动排障 / 通知）
	ie.SetRiskPublisher(func(task domain.InspectionTask, run *domain.InspectionRun, findings []domain.Finding) {
		n := al.IngestInspectionFindings(run, findings)
		log.Printf("[inspect->alarm] task=%s run=%s risk=%d published=%d", task.Name, run.ID, len(findings), n)
	})
	if al.NotifyEnabled() {
		log.Printf("[alarm-notify] enabled url=%s", cfg.AlarmNotifyURL)
	} else {
		log.Printf("[alarm-notify] disabled (ALARM_NOTIFY_URL empty)")
	}

	server := api.New(cfg, st, tp, gw, ie, de, ce, te, ae, al, rel, kb, reg, asvc)
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 共享 ctx：http 退出 + alarm 采集 都挂在这个上
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		log.Printf("[http] listening on %s | topo=%s | store=%s | dates=%v", cfg.Addr, tp.Source(), cfg.StoreDir, tp.Dates())
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("[http] %v", err)
		}
	}()

	if cfg.AlarmEnabled {
		go func() {
			log.Printf("[alarm] poll interval=%s cooldown=%s auto_diag=%v max_auto_diag=%d",
				cfg.AlarmPollInterval, cfg.AlarmCooldown, cfg.AlarmAutoDiag, cfg.AlarmMaxAutoDiag)
			al.Start(ctx)
		}()
		if cfg.DegradationEnabled {
			go func() {
				log.Printf("[degradation] interval=%s window=%dmin threshold=%.0f%% max_targets=%d",
					cfg.DegradationInterval, cfg.DegradationWindowMin, cfg.DegradationThresholdPct, cfg.DegradationMaxTargets)
				al.StartDegradation(ctx)
			}()
		}
		if cfg.BizMonEnabled {
			go func() {
				log.Printf("[biz-monitor] interval=%s", cfg.BizMonInterval)
				al.StartBiz(ctx)
			}()
		}
	} else {
		log.Printf("[alarm] disabled (ALARM_ENABLED=false)")
	}

	// 巡检定时调度（S2）：分钟级 tick，只在任务配置了 cron 时触发
	go func() {
		log.Printf("[inspect-cron] enabled=%v tick=%s", cfg.InspectCronEnabled, cfg.InspectCronTick)
		inspect.NewScheduler(ie, st, cfg).Start(ctx)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit

	// 停止后台循环
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	log.Println("[exit] bye")
}

// buildProvider file 模式零依赖直读 topo 快照；http 模式对接 topo 服务。
func buildProvider(cfg config.Config) (topo.Provider, error) {
	if cfg.TopoMode == "http" {
		return topo.NewHTTPProvider(cfg.TopoBaseURL, cfg.TopoToken, 15*time.Second), nil
	}
	return topo.NewFileProvider(cfg.TopoDataDir)
}
