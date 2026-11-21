// 命令 api 启动 HTTP 服务：读配置、连库、装配五层里的接口层与控制/数据适配器。
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

const version = "0.1.0"

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg := loadConfig()
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

	srv := api.New(
		api.NewPGAccounts(store.New(pool)),
		api.NewSessionCodec(cfg.sessionSecret, cfg.sessionTTL),
		cfg.cookieSecure,
		version,
		log,
	)

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
	addr          string
	databaseURL   string
	sessionSecret string
	sessionTTL    time.Duration
	cookieSecure  bool
}

func loadConfig() config {
	return config{
		addr:          env("ADDR", ":8081"),
		databaseURL:   env("DATABASE_URL", "postgres://postgres:spike@127.0.0.1:5433/smarthome?sslmode=disable"),
		sessionSecret: env("SESSION_SECRET", "dev-only-session-secret-change-me"),
		sessionTTL:    12 * time.Hour,
		cookieSecure:  os.Getenv("COOKIE_SECURE") == "true",
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
