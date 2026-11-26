// 命令 api 启动 HTTP 服务：读配置、连库、装配五层里的接口层与控制/数据适配器。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/device"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

const (
	version              = "0.1.0"
	defaultSessionSecret = "dev-only-session-secret-change-me"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := loadConfig()
	if cfg.sessionSecret == defaultSessionSecret {
		log.Warn("正在使用默认会话密钥，仅限本地开发；部署前必须设置 SESSION_SECRET")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		log.Error("连接数据库失败", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Error("数据库不可达", "err", err, "hint", "先跑 scripts/reset.sh 或 docker compose up -d db")
		os.Exit(1)
	}

	commandHandler := control.NewCommandHandler(
		pool,
		device.NewSimulatorAdapter(cfg.simulatorDelay, cfg.simulatorFailureRate),
		cfg.commandTimeout,
	)
	hub := api.NewHub(storegen.New(pool))
	commandHandler.SetBroadcaster(hub)

	srv := api.New(
		api.NewPGAccounts(storegen.New(pool)),
		api.NewPGPlan(pool),
		api.NewPGItemTypes(pool),
		itemstore.NewItems(pool),
		commandHandler,
		api.NewSessionCodec(cfg.sessionSecret, cfg.sessionTTL),
		cfg.cookieSecure,
		version,
		log,
	)
	srv.SetHub(hub)
	srv.SetModeRunner(control.NewModeRunner(pool, commandHandler, hub))

	sensorReporter := control.NewSensorReporter(pool, hub)
	srv.SetSensorReporter(sensorReporter)
	go func() {
		_ = sensorReporter.Report(ctx)
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = sensorReporter.Report(ctx)
			}
		}
	}()

	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("接口层已启动", "addr", cfg.addr, "version", version)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("监听失败", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	log.Info("收到退出信号，正在关闭")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("关闭超时", "err", err)
	}
}

type config struct {
	addr                 string
	databaseURL          string
	sessionSecret        string
	sessionTTL           time.Duration
	cookieSecure         bool
	simulatorDelay       time.Duration
	simulatorFailureRate float64
	commandTimeout       time.Duration
}

func loadConfig() config {
	return config{
		addr:                 env("ADDR", ":8081"),
		databaseURL:          env("DATABASE_URL", "postgres://postgres:spike@127.0.0.1:5433/smarthome?sslmode=disable"),
		sessionSecret:        env("SESSION_SECRET", defaultSessionSecret),
		sessionTTL:           12 * time.Hour,
		cookieSecure:         os.Getenv("COOKIE_SECURE") == "true",
		simulatorDelay:       envDuration("SIMULATOR_DELAY", 100*time.Millisecond),
		simulatorFailureRate: envFloat("SIMULATOR_FAILURE_RATE", 0),
		commandTimeout:       envDuration("COMMAND_TIMEOUT", 3*time.Second),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envFloat(key string, fallback float64) float64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < 0 || parsed > 1 {
		return fallback
	}
	return parsed
}
