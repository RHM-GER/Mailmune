package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
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
