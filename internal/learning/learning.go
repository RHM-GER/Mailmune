// Package learning implements a small, fully local and explainable online
// learner used as the statistical stage of the classification pipeline.
//
// It is a multinomial Naive Bayes model over bounded token features. It is
// deliberately simple and transparent: every prediction can be traced back
// to per-token spam/ham counts that only ever come from human-confirmed
// reviews. There is no external model, no network access and no bundled
// training data.
package learning

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Version identifies the feature/model format. Persisted models carry it so
// a future change can migrate or reject incompatible data instead of
// silently mis-scoring.
const Version = 1

// Class is the label a confirmed review assigns to a message.
type Class string

const (
	ClassSpam Class = "spam"
	ClassHam  Class = "ham"
)

const (
	// Guard rails against poisoning and runaway growth.
	maxTokensPerMessage  = 500
	maxOccurrencesPerTok = 3
	minTokenRunes        = 3
	maxTokenRunes        = 24
	// Smoothing keeps unseen tokens from producing hard 0/1 probabilities.
	smoothingAlpha = 1.0
	// Only the most discriminative tokens drive the combined score.
	combineTopK = 15
)

// Model holds per-token counts for one account. It is not safe for
// concurrent use; callers serialize access (the store does).
type Model struct {
	Version int
	// SpamCounts and HamCounts map a token to the number of times it was
	// seen in confirmed spam / ham messages.
	SpamCounts map[string]uint64
	HamCounts  map[string]uint64
	// SpamTokens and HamTokens are the total token occurrences seen.
	SpamTokens uint64
	HamTokens  uint64
	// SpamMessages and HamMessages count trained examples.
	SpamMessages uint64
	HamMessages  uint64
}

// NewModel returns an empty model at the current version.
func NewModel() *Model {
	return &Model{Version: Version, SpamCounts: map[string]uint64{}, HamCounts: map[string]uint64{}}
}

// Trained reports the total number of confirmed examples the model saw.
func (m *Model) Trained() uint64 { return m.SpamMessages + m.HamMessages }

// Train folds one message's features into the model. The same feature map
// must always be produced by ExtractFeatures so training and scoring stay
// reproducible.
func (m *Model) Train(features map[string]int, class Class) {
	if len(features) == 0 {
		return
	}
	counts := m.SpamCounts
	if class == ClassHam {
		counts = m.HamCounts
	}
	var total uint64
	for token, n := range features {
		if n <= 0 {
			continue
		}
		counts[token] += uint64(n)
		total += uint64(n)
	}
	if class == ClassSpam {
		m.SpamTokens += total
		m.SpamMessages++
	} else {
		m.HamTokens += total
		m.HamMessages++
	}
}

// probability returns the smoothed P(token | class) for one side.
func (m *Model) probability(token string, spam bool) float64 {
	var count, total uint64
	if spam {
		count = m.SpamCounts[token]
		total = m.SpamTokens
	} else {
		count = m.HamCounts[token]
		total = m.HamTokens
	}
	return (float64(count) + smoothingAlpha) / (float64(total) + smoothingAlpha*float64(m.vocabSize()))
}

func (m *Model) vocabSize() int {
	seen := map[string]struct{}{}
	for token := range m.SpamCounts {
		seen[token] = struct{}{}
	}
	for token := range m.HamCounts {
		seen[token] = struct{}{}
	}
	if len(seen) == 0 {
		return 1
	}
	return len(seen)
}

// Score returns the spam probability for a feature set and whether the model
// considered the message at all. It blends the prior (message counts) with
// the most discriminative tokens so a model trained on one side only does not
// become overconfident.
func (m *Model) Score(features map[string]int) (float64, bool) {
	if len(features) == 0 || m.Trained() == 0 {
		return 0, false
	}
	if m.SpamMessages == 0 || m.HamMessages == 0 {
		// Only one class was ever confirmed; the model cannot discriminate.
		return 0, false
	}

	type scored struct {
		token string
		dev   float64
	}
	candidates := make([]scored, 0, len(features))
	for token := range features {
		spam := m.probability(token, true)
		ham := m.probability(token, false)
		p := spam / (spam + ham)
		dev := math.Abs(p - 0.5)
		if dev < 0.02 {
			continue
		}
		candidates = append(candidates, scored{token: token, dev: dev})
	}
	if len(candidates) == 0 {
		return 0, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].dev > candidates[j].dev })
	if len(candidates) > combineTopK {
		candidates = candidates[:combineTopK]
	}

	// Combine in log space to avoid underflow, starting from the prior.
	prior := float64(m.SpamMessages) / float64(m.Trained())
	logOdds := math.Log(prior) - math.Log(1-prior)
	for _, candidate := range candidates {
		spam := m.probability(candidate.token, true)
		ham := m.probability(candidate.token, false)
		logOdds += math.Log(spam) - math.Log(ham)
	}
	probability := 1 / (1 + math.Exp(-logOdds))
	return clamp01(probability), true
}

// Reset clears all learned state, keeping the model usable.
func (m *Model) Reset() {
	m.SpamCounts = map[string]uint64{}
	m.HamCounts = map[string]uint64{}
	m.SpamTokens, m.HamTokens = 0, 0
	m.SpamMessages, m.HamMessages = 0, 0
}

// Merged returns a new model combining m (weight 1) with other scaled by
// otherWeight. Scaling lets a large imported baseline act as a bounded prior
// that the user's confirmed learning can still outweigh; it never mutates
// either input.
func (m *Model) Merged(other *Model, otherWeight float64) *Model {
	out := NewModel()
	addScaledCounts(out, m, 1.0)
	addScaledCounts(out, other, otherWeight)
	return out
}

// VirtualWeight returns the merge weight that makes the model contribute
// approximately targetMessages examples, preserving its class proportions.
// A huge corpus and a tiny one then influence scoring comparably.
func (m *Model) VirtualWeight(targetMessages uint64) float64 {
	trained := m.Trained()
	if trained == 0 || targetMessages == 0 {
		return 0
	}
	return float64(targetMessages) / float64(trained)
}

func addScaledCounts(dst *Model, src *Model, weight float64) {
	if src == nil || weight <= 0 {
		return
	}
	for token, count := range src.SpamCounts {
		dst.SpamCounts[token] += scaleCount(count, weight)
	}
	for token, count := range src.HamCounts {
		dst.HamCounts[token] += scaleCount(count, weight)
	}
	dst.SpamTokens += scaleCount(src.SpamTokens, weight)
	dst.HamTokens += scaleCount(src.HamTokens, weight)
	dst.SpamMessages += scaleCount(src.SpamMessages, weight)
	dst.HamMessages += scaleCount(src.HamMessages, weight)
}

func scaleCount(value uint64, weight float64) uint64 {
	if weight <= 0 || value == 0 {
		return 0
	}
	return uint64(math.Round(float64(value) * weight))
}

// ExtractFeatures turns the metadata and bounded text of a message into a
// reproducible token histogram. It never stores raw text: only normalized,
// length-bounded tokens and a small number of structured markers survive.
func ExtractFeatures(subject, from, fromDomain, text string) map[string]int {
	features := map[string]int{}
	addTokens(features, subject, 1)
	addTokens(features, text, 1)
	if domain := strings.ToLower(strings.TrimSpace(fromDomain)); domain != "" {
		addSingle(features, "dom:"+domain, 2)
	}
	if local, host, ok := splitAddress(from); ok {
		addSingle(features, "snd:"+strings.ToLower(local+"@"+host), 2)
	}
	return features
}

func addTokens(features map[string]int, input string, weight int) {
	if input == "" {
		return
	}
	var builder strings.Builder
	flush := func() {
		token := builder.String()
		builder.Reset()
		runes := []rune(token)
		if len(runes) < minTokenRunes || len(runes) > maxTokenRunes {
			return
		}
		if len(features) >= maxTokensPerMessage {
			return
		}
		features[token] += weight
		if features[token] > maxOccurrencesPerTok {
			features[token] = maxOccurrencesPerTok
		}
	}
	for _, character := range input {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			builder.WriteRune(unicode.ToLower(character))
			continue
		}
		if builder.Len() > 0 {
			flush()
		}
	}
	if builder.Len() > 0 {
		flush()
	}
}

func addSingle(features map[string]int, token string, weight int) {
	if len(features) >= maxTokensPerMessage {
		return
	}
	features[token] += weight
	if features[token] > maxOccurrencesPerTok {
		features[token] = maxOccurrencesPerTok
	}
}

func splitAddress(address string) (string, string, bool) {
	parts := strings.SplitN(strings.TrimSpace(address), "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
