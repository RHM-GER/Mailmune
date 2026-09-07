package corpus

import (
	"strings"
	"testing"

	"github.com/RHM-GER/Mailmune/internal/learning"
)

func TestLoadHFStyleCorpus(t *testing.T) {
	csv := "label,text\n0,hallo wie geht es dir heute im buero\n2,gratis gewinn jetzt sofort klicken und bestaetigen\n1,ihre identitaet bestaetigen sonst sperrung\n0,projekt meeting termin naechste woche\n"
	samples, err := Load(strings.NewReader(csv), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 4 {
		t.Fatalf("samples = %d, want 4", len(samples))
	}
	if samples[0].Class != learning.ClassHam {
		t.Fatalf("label 0 must be ham, got %s", samples[0].Class)
	}
	if samples[1].Class != learning.ClassSpam || samples[2].Class != learning.ClassSpam {
		t.Fatalf("labels 1 and 2 must collapse to spam: %s %s", samples[1].Class, samples[2].Class)
	}
	counts := Count(samples)
	if counts.Spam != 2 || counts.Ham != 2 || counts.Total != 4 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestLoadWordLabelsAndAliases(t *testing.T) {
	csv := "class,body\nham,legitimer text\nSpam,böser text\nphishing,noch böser\n"
	samples, err := Load(strings.NewReader(csv), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 3 || samples[0].Class != learning.ClassHam || samples[2].Class != learning.ClassSpam {
		t.Fatalf("unexpected samples: %+v", samples)
	}
}

func TestLoadSkipsMalformedRows(t *testing.T) {
	csv := "label,text\n0,guter text\nunbekannt,wird uebersprungen\n2,spam text\n0,\n"
	samples, err := Load(strings.NewReader(csv), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("samples = %d, want 2 (bad label + empty text skipped)", len(samples))
	}
}

func TestLoadRespectsMaxRowsAndTextLimit(t *testing.T) {
	long := strings.Repeat("wort ", 5000)
	csv := "label,text\n2," + long + "\n0,kurz\n2,auch kurz\n"
	samples, err := Load(strings.NewReader(csv), Options{MaxRows: 2, MaxTextBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("MaxRows not honored: %d", len(samples))
	}
	if len(samples[0].Text) > 100 {
		t.Fatalf("text not truncated to <=100: %d", len(samples[0].Text))
	}
	if len(samples[0].Text) >= 5000 {
		t.Fatalf("text not meaningfully truncated: %d", len(samples[0].Text))
	}
}

func TestLoadRejectsMissingColumns(t *testing.T) {
	if _, err := Load(strings.NewReader("foo,bar\n1,x\n"), Options{}); err == nil {
		t.Fatal("missing label/text columns must fail")
	}
	if _, err := Load(strings.NewReader("label,text\n"), Options{}); err == nil {
		t.Fatal("empty corpus must fail")
	}
}
