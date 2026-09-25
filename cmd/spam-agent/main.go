package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/RHM-GER/Mailmune/internal/api"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/service"
	"github.com/RHM-GER/Mailmune/internal/store"
)

func main() {
	dataDir := flag.String("data-dir", "", "application data directory")
	flag.Parse()
	if *dataDir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			log.Fatal(err)
		}
		*dataDir = filepath.Join(base, "Mailmune")
	}
	if err := os.MkdirAll(*dataDir, 0700); err != nil {
		log.Fatal(err)
	}
	// Debug-Logdatei neben der Datenbank: alles, was der Agent loggt (Scans,
	// KI-Consults, Profil-Indikatoren, Fehler), landet zusätzlich hier und
	// lässt sich jederzeit öffnen – unabhängig davon, ob ein Terminal lauscht.
	if logFile, openErr := os.OpenFile(filepath.Join(*dataDir, "mailmune-debug.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); openErr == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
		defer logFile.Close()
	} else {
		log.Printf("debug log disabled: %v", openErr)
	}
	log.Printf("agent start: data-dir=%s", *dataDir)
	token := os.Getenv("MAILMUNE_TOKEN")
	if token == "" {
		token = randomToken()
	}
	db, err := store.Open(filepath.Join(*dataDir, "mailmune.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	svc := service.New(db, secrets.NewKeyringStore())
	server, err := api.New(token, svc)
	if err != nil {
		log.Fatal(err)
	}
	handshake := map[string]string{"type": "ready", "endpoint": server.Address(), "token": token}
	_ = json.NewEncoder(os.Stdout).Encode(handshake)
	go func() {
		if err := server.Serve(); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()
	// Periodic UID reconciliation so new mail is detected without the user
	// clicking; stopped cleanly on shutdown below.
	schedulerCtx, stopScheduler := context.WithCancel(context.Background())
	svc.StartScheduler(schedulerCtx)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if count, err := svc.Purge(context.Background()); err != nil {
				log.Printf("metadata retention: %v", err)
			} else if count > 0 {
				log.Printf("redacted %d old audit rows", count)
			}
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	stopScheduler()
	svc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(ctx)
}

func randomToken() string {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Errorf("session token: %w", err))
	}
	return hex.EncodeToString(raw)
}
