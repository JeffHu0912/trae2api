// main.go trae2api 入口：加载配置 → 构建 pool → 起 HTTP 服务。
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"trae2api/internal/auth"
	"trae2api/internal/pool"
	"trae2api/internal/scheduler"
	"trae2api/internal/server"
	"trae2api/internal/upstream"
)

func main() {
	cfgPath := flag.String("config", "config.json", "path to config json")
	flag.Parse()

	cfg, err := Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	auths, err := auth.LoadDir(cfg.AuthDir)
	if err != nil {
		log.Fatalf("load auths: %v", err)
	}
	log.Printf("loaded %d account(s) from %s", len(auths), cfg.AuthDir)

	p := pool.New(cfg.StateFile)
	p.SyncToDir(auths) // 对齐：剔除 state.json 中已删除 auth 文件的幽灵账号

	up := upstream.New()
	up.HTTP.Timeout = time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	// 流式客户端无总超时，仅用首字节兜底（时长由 SSE 流本身决定）。
	if tr, ok := up.StreamHTTP.Transport.(*http.Transport); ok {
		tr.ResponseHeaderTimeout = time.Duration(cfg.Upstream.TimeoutSeconds) * time.Second
	}

	workCfg := upstream.DefaultWorkClientConfig()
	if cfg.WorkHost != "" {
		workCfg.Host = cfg.WorkHost
	}
	if cfg.WorkBridgeURL != "" {
		workCfg.BridgeURL = cfg.WorkBridgeURL
	}
	if cfg.WorkBridgeToken != "" {
		workCfg.BridgeToken = cfg.WorkBridgeToken
	}
	if cfg.WorkMode != "" {
		workCfg.Mode = upstream.WorkMode(cfg.WorkMode)
	}
	workClient := upstream.NewWorkClient(workCfg)

	sch := scheduler.New(scheduler.Config{
		Pool:         p,
		Upstream:     up,
		CheckinHour:  cfg.Schedule.CheckinHour,
		RefreshHours: cfg.Schedule.RefreshHours,
		RefreshSkew:  24 * time.Hour,
	})

	h := server.NewHandler(server.Config{
		Pool:            p,
		Upstream:        up,
		WorkClient:      workClient,
		WorkMode:        upstream.WorkMode(cfg.WorkMode),
		APIKey:          cfg.APIKey,
		AuthDir:         cfg.AuthDir,
		PlanCooldown:    cfg.PlanCreditDur,
		SoftCooldown:    cfg.SoftRateDur,
		ErrThreshold:    cfg.Cooldown.ErrThresh,
		ErrCooldown:     cfg.ErrCooldownDur,
		DefaultModel:    cfg.DefaultModel,
		WorkBridgeURL:   cfg.WorkBridgeURL,
		WorkBridgeToken: cfg.WorkBridgeToken,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go sch.Run(ctx)

	// 启动时在后台探测所有可用账号的 work_credits 积分状态
	if workClient.Mode() != upstream.WorkModeDisabled {
		go func() {
			probeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, a := range auths {
				snap, err := workClient.ProbeCredits(probeCtx, a)
				if err == nil {
					p.SetWorkCredits(a.UID, snap.WorkCredits)
					log.Printf("[WorkPool] account %s (%s) work_credits: %.4f", a.UID, a.Nickname, snap.WorkCredits)
				}
			}
		}()
	}

	srv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           h,
		ReadHeaderTimeout: 30 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	// 第二个 http.Server：监听 CallbackPort（默认 18080），只处理 /authorize 回调。
	// 复用同一 Handler（/authorize 已在主 mux 注册）。
	// cfg.CallbackPort == "0" 时不启动（纯手动粘贴模式）。
	var cbSrv *http.Server
	if cfg.CallbackPort != "" && cfg.CallbackPort != "0" {
		cbSrv = &http.Server{
			Addr:              ":" + cfg.CallbackPort,
			Handler:           h,
			ReadHeaderTimeout: 30 * time.Second,
		}
		go func() {
			<-ctx.Done()
			sc, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = cbSrv.Shutdown(sc)
		}()
		go func() {
			log.Printf("trae2api callback server on :%s (TRAE login /authorize)", cfg.CallbackPort)
			if err := cbSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				// 端口被占用（login.sh / 旧实例）不致命，降级为手动粘贴模式。
				log.Printf("callback server (:%s) failed: %v — web 登录降级为手动粘贴回调链接", cfg.CallbackPort, err)
			}
		}()
	}

	log.Printf("trae2api listening on %s (api_key=%v)", cfg.Listen, cfg.APIKey != "")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http: %v", err)
	}
	log.Printf("bye")
}
