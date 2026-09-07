// Package corpus loads labeled email corpora for offline evaluation and for
// the opt-in baseline import. It only reads local files the user explicitly
// points it at; nothing is downloaded and no corpus is bundled with the app.
//
// Supported input: CSV with a header row containing a label column and a text
// column. The label is normalized to ham/spam. Recognized label spellings:
// ham/legit/0 → ham; spam/phish/phishing/1/2 → spam. Column names are matched
// case-insensitively among common aliases so datasets like the MIT-licensed
// "biggest spam ham phish" corpus (columns: label,text) load directly.
package corpus

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/RHM-GER/Mailmune/internal/learning"
)

// Sample is one labeled message.
type Sample struct {
	Class learning.Class
	Text  string
}

// Options bound a load so a huge corpus cannot exhaust memory.
type Options struct {
	// MaxRows caps how many samples are read; 0 means no explicit cap beyond
	// MaxBytes. A negative value is treated as 0.
	MaxRows int
	// MaxTextBytes truncates each sample's text; 0 uses a default.
	MaxTextBytes int
}

const defaultMaxTextBytes = 64 << 10

// LoadFile reads a CSV corpus from disk.
func LoadFile(path string, opts Options) ([]Sample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return Load(file, opts)
}

// Load reads a CSV corpus from r. The first row must be a header naming the
// label and text columns.
func Load(r io.Reader, opts Options) ([]Sample, error) {
	if opts.MaxRows < 0 {
		opts.MaxRows = 0
	}
	maxText := opts.MaxTextBytes
	if maxText <= 0 {
		maxText = defaultMaxTextBytes
	}

	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1 // tolerate ragged rows
	reader.ReuseRecord = false
	reader.LazyQuotes = true

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	labelIndex, textIndex, err := findColumns(header)
	if err != nil {
		return nil, err
	}

	var samples []Sample
	skipped := 0
	for {
		if opts.MaxRows > 0 && len(samples) >= opts.MaxRows {
			break
		}
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Skip malformed rows instead of aborting the whole corpus.
			skipped++
			if skipped > 10000 {
				return nil, errors.New("too many malformed rows")
			}
			continue
		}
		if labelIndex >= len(record) || textIndex >= len(record) {
			skipped++
			continue
		}
		class, ok := normalizeLabel(record[labelIndex])
		if !ok {
			skipped++
			continue
		}
		text := record[textIndex]
		if len(text) > maxText {
			text = text[:maxText]
		}
		text = strings.TrimSpace(text)
		if text == "" {
			skipped++
			continue
		}
		samples = append(samples, Sample{Class: class, Text: text})
	}
	if len(samples) == 0 {
		return nil, errors.New("corpus contains no usable samples")
	}
	return samples, nil
}

func findColumns(header []string) (labelIndex, textIndex int, err error) {
	labelIndex, textIndex = -1, -1
	for i, raw := range header {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "label", "class", "target", "spam", "is_spam", "category":
			if labelIndex < 0 {
				labelIndex = i
			}
		case "text", "body", "message", "content", "email", "mail":
			if textIndex < 0 {
				textIndex = i
			}
		}
	}
	if labelIndex < 0 || textIndex < 0 {
		return 0, 0, fmt.Errorf("header must contain a label and a text column, got %v", header)
	}
	return labelIndex, textIndex, nil
}

// normalizeLabel maps a raw label to a class. Numeric labels follow the common
// convention 0=ham, 1=phish, 2=spam; both phish and spam collapse to spam
// because the local filter is a binary ham/spam decision.
func normalizeLabel(raw string) (learning.Class, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "0", "ham", "legit", "legitimate", "not_spam", "notspam", "false":
		return learning.ClassHam, true
	case "1", "2", "spam", "phish", "phishing", "true":
		return learning.ClassSpam, true
	default:
		return "", false
	}
}

// Counts summarizes a corpus for provenance reporting.
type Counts struct {
	Total int
	Spam  int
	Ham   int
}

// Count tallies the classes of a sample set.
func Count(samples []Sample) Counts {
	counts := Counts{Total: len(samples)}
	for _, sample := range samples {
		if sample.Class == learning.ClassSpam {
			counts.Spam++
		} else {
			counts.Ham++
		}
	}
	return counts
}
