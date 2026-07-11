package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"sub2api-giftcode/backend/internal/app"
	"sub2api-giftcode/backend/internal/config"
	"sub2api-giftcode/backend/internal/db"
	"sub2api-giftcode/backend/internal/httpapi"
	"sub2api-giftcode/backend/internal/mail"
	"sub2api-giftcode/backend/internal/sub2api"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "../config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	err = os.MkdirAll(filepath.Dir(cfg.Database.Path), 0755)
	if err != nil {
		log.Fatalf("mkdir: %v", err)
	}

	store, err := db.Open(cfg.Database.Driver, cfg.Database.Path)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	upstream := sub2api.NewClient(cfg.Sub2API.BaseURL, cfg.Sub2API.AdminAPIKey)
	mailer := mail.New(mail.Config{
		SMTPHost:       cfg.Mail.SMTPHost,
		SMTPPort:       cfg.Mail.SMTPPort,
		SMTPUsername:   cfg.Mail.SMTPUsername,
		SMTPPassword:   cfg.Mail.SMTPPassword,
		FromAddress:    cfg.Mail.FromAddress,
		AdminToAddress: cfg.Mail.AdminToAddress,
		SubjectPrefix:  cfg.Mail.SubjectPrefix,
	})
	service := app.New(cfg, store, upstream, mailer)
	router := httpapi.NewRouter(cfg, service)

	runCtx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	if cfg.Sync.IntervalSeconds > 0 {
		go runSyncLoop(runCtx, service, time.Duration(cfg.Sync.IntervalSeconds)*time.Second)
	}
	concurrencyMonitorDone := make(chan struct{})
	go func() {
		defer close(concurrencyMonitorDone)
		runSubscriptionConcurrencyLoop(runCtx, service.ReconcileSubscriptionConcurrency, 30*time.Minute)
	}()

	srv := &http.Server{
		Addr:              cfg.App.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		<-runCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("sub2api giftcode backend listening on %s", cfg.App.ListenAddr)
	listenErr := srv.ListenAndServe()
	cancel()
	<-concurrencyMonitorDone
	if listenErr != nil && listenErr != http.ErrServerClosed {
		log.Fatalf("server error: %v", listenErr)
	}
}

func runSyncLoop(ctx context.Context, service *app.Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := service.SyncRedeemCodes(ctx); err != nil {
				log.Printf("sync redeem codes failed: %v", err)
			}
		}
	}
}

func runSubscriptionConcurrencyLoop(ctx context.Context, reconcile func(context.Context) error, interval time.Duration) {
	if err := reconcile(ctx); err != nil {
		log.Printf("reconcile subscription concurrency failed: %v", err)
	}
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := reconcile(ctx); err != nil {
				log.Printf("reconcile subscription concurrency failed: %v", err)
			}
		}
	}
}
