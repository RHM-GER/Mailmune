// Command scandebug is a read-only diagnostic tool. It connects to the
// configured accounts exactly like the agent (same IMAP client, same rule
// pipeline) and prints the computed score and evidence codes of every
// message. Nothing is stored, moved or changed; passwords are read from the
// OS keyring like in production.
//
// Usage: go run ./cmd/scandebug <path-to-mailmune.db>
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/RHM-GER/Mailmune/internal/classifier"
	"github.com/RHM-GER/Mailmune/internal/domain"
	"github.com/RHM-GER/Mailmune/internal/learning"
	"github.com/RHM-GER/Mailmune/internal/mailbox"
	"github.com/RHM-GER/Mailmune/internal/secrets"
	"github.com/RHM-GER/Mailmune/internal/store"
)

type row struct {
	score    float64
	from     string
	subject  string
	codes    string
	textLen  int
	urlCount int
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: scandebug <pfad-zu-mailmune.db>")
	}
	db, err := store.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	accounts, err := db.ListAccounts(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if len(accounts) == 0 {
		log.Fatal("keine Konten in der Datenbank")
	}
	keyring := secrets.NewKeyringStore()
	rules := classifier.NewRules()

	for _, account := range accounts {
		fmt.Printf("\n=== Konto %s (%s@%s) ===\n", account.Name, account.Username, account.Host)
		password, err := keyring.Get(account.SecretRef)
		if err != nil {
			fmt.Printf("Passwort nicht lesbar: %v\n", err)
			continue
		}
		client := mailbox.NewClient()
		var rows []row
		outcome, err := client.SyncFolder(ctx, account, password, account.InboxFolder, nil,
			mailbox.SyncOptions{MaxMessages: 1000, FetchText: true},
			func(msg domain.MessageFeatures, text string) error {
				msg.Text = text
				msg.URLCount = classifier.CountURLs(text)
				features := learning.ExtractFeatures(msg.Subject, msg.From, msg.FromDomain, text)
				classification := rules.ClassifyWithFeatures(msg, account.Profile, features, nil)
				codes := make([]string, 0, len(classification.Evidence))
				for _, evidence := range classification.Evidence {
					codes = append(codes, evidence.Code)
				}
				rows = append(rows, row{score: classification.Score, from: msg.From, subject: msg.Subject, codes: strings.Join(codes, ","), textLen: len(text), urlCount: msg.URLCount})
				return nil
			})
		if err != nil {
			fmt.Printf("Sync-Fehler: %v\n", err)
			continue
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
		fmt.Printf("Gelesen: %d Nachrichten (UIDVALIDITY %d, letzte UID %d)\n\n", outcome.Processed, outcome.UIDValidity, outcome.LastUID)
		fmt.Printf("%-6s %-38s %-52s %-4s %-6s %s\n", "SCORE", "ABSENDER", "BETREFF", "TEXT", "URLS", "CODES")
		limit := 60
		if len(rows) < limit {
			limit = len(rows)
		}
		for _, item := range rows[:limit] {
			fmt.Printf("%-6.2f %-38.38s %-52.52s %-4d %-6d %s\n", item.score, item.from, item.subject, item.textLen, item.urlCount, item.codes)
		}
		buckets := map[string]int{"<0.2": 0, "0.2-0.4": 0, "0.4-0.6": 0, "0.6-0.8": 0, "0.8-0.98": 0, ">=0.98": 0}
		candidates := 0
		for _, item := range rows {
			switch {
			case item.score < 0.2:
				buckets["<0.2"]++
			case item.score < 0.4:
				buckets["0.2-0.4"]++
			case item.score < 0.6:
				buckets["0.4-0.6"]++
			case item.score < 0.8:
				buckets["0.6-0.8"]++
			case item.score < 0.98:
				buckets["0.8-0.98"]++
			default:
				buckets[">=0.98"]++
			}
			if item.score >= 0.6 {
				candidates++
			}
		}
		fmt.Printf("\nVerteilung: %v\nVerdachtsfaelle (>=0.60): %d von %d\n", buckets, candidates, len(rows))
	}
}
