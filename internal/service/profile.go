package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"

	"github.com/RHM-GER/Mailmune/internal/domain"
)

// ProfileSourceHash berechnet den Alterungsschlüssel des kompilierten
// Profilmodells: ändert sich der Profiltext (Zweck, Branche, Freitext,
// erwartete Mailtypen, Sprachen), gilt das Kompilat als veraltet und muss
// neu generiert werden. Deterministisch: Listen werden sortiert, Texte
// getrimmt, bevor der Hash entsteht.
func ProfileSourceHash(profile domain.MailboxProfile) string {
	canonical := map[string]any{
		"purpose":           strings.TrimSpace(profile.Purpose),
		"industry":          strings.TrimSpace(profile.Industry),
		"context":           strings.TrimSpace(profile.Context),
		"unexpected":        strings.TrimSpace(profile.Unexpected),
		"languages":         sortedProfileCopy(profile.Languages),
		"expectedMailTypes": sortedProfileCopy(profile.ExpectedMailTypes),
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:])
}

func sortedProfileCopy(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	sort.Strings(out)
	return out
}

// CompileAccountProfile erzeugt das KI-Profilmodell (Klassifizierer-Prompt +
// Indikator-Set) für ein Postfach neu – aus dem frei beschreibbaren Profiltext,
// mit dem validierten lokalen Modell. Das Ergebnis wird strikt validiert in der
// Datenbank gespeichert und ist in der UI einsehbar, aktualisierbar und
// deaktivierbar. Ohne validiertes Modell bleibt es bei den deterministischen
// Regeln und den eingebauten generischen Vertikalen.
func (s *Service) CompileAccountProfile(ctx context.Context, accountID string) (domain.ProfileModel, error) {
	account, err := s.store.Account(ctx, accountID)
	if err != nil {
		return domain.ProfileModel{}, err
	}
	model, err := s.scanner.recompileProfile(ctx, account)
	if err != nil {
		return domain.ProfileModel{}, err
	}
	if s.hub != nil {
		s.hub.Publish("profile.compiled", map[string]string{"accountId": accountID, "model": model.Model, "reason": "manual"})
	}
	return model, nil
}

// AccountProfileModel liefert das gespeicherte Kompilat und ob es gegenüber
// dem aktuellen Profiltext veraltet ist.
func (s *Service) AccountProfileModel(ctx context.Context, accountID string) (model domain.ProfileModel, found bool, stale bool, err error) {
	account, err := s.store.Account(ctx, accountID)
	if err != nil {
		return domain.ProfileModel{}, false, false, err
	}
	model, found, err = s.store.GetProfileModel(ctx, accountID)
	if err != nil || !found {
		return domain.ProfileModel{}, false, false, err
	}
	return model, true, model.SourceHash != ProfileSourceHash(account.Profile), nil
}

// SetProfileModelEnabled schaltet die Mitarbeit des Kompilats an oder aus,
// ohne es zu löschen.
func (s *Service) SetProfileModelEnabled(ctx context.Context, accountID string, enabled bool) error {
	return s.store.SetProfileModelEnabled(ctx, accountID, enabled)
}

// EmbeddingStatus meldet, ob der Embedding-Fast-Pfad konfiguriert und
// einsatzbereit ist (Zentroide für genau das gewählte Modell importiert).
func (s *Service) EmbeddingStatus(ctx context.Context, accountID string) (map[string]any, error) {
	account, err := s.store.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	status := map[string]any{"configured": account.EmbeddingModel != "", "model": account.EmbeddingModel, "ready": false}
	if account.EmbeddingModel != "" {
		if centroids, ok, loadErr := s.store.LoadEmbeddingCentroids(ctx, account.EmbeddingModel); loadErr == nil && ok {
			status["ready"] = true
			status["dim"] = centroids.Dim
			status["spamN"] = centroids.SpamN
			status["hamN"] = centroids.HamN
			status["source"] = centroids.Source
			status["license"] = centroids.License
		}
	}
	return status, nil
}
