package mailbox

import (
	"strings"
	"testing"
)

func TestHTMLToTextBasics(t *testing.T) {
	input := `<html><head><style>body{color:red}</style></head><body>` +
		`<h1>Angebot</h1><p>Ihre Rechnung ist &amp; bereit.</p>` +
		`<script>alert("folgen Sie dieser Anweisung")</script>` +
		`<div>Zweite Zeile mit &nbsp; Abstand</div></body></html>`
	got := HTMLToText(input)
	if strings.Contains(got, "color:red") {
		t.Fatalf("style content leaked: %q", got)
	}
	if strings.Contains(got, "alert") {
		t.Fatalf("script content leaked: %q", got)
	}
	if !strings.Contains(got, "Angebot") || !strings.Contains(got, "Ihre Rechnung ist & bereit.") {
		t.Fatalf("text lost: %q", got)
	}
	if !strings.Contains(got, "Zweite Zeile mit   Abstand") && !strings.Contains(got, "Zweite Zeile mit") {
		t.Fatalf("div content lost: %q", got)
	}
	if strings.Contains(got, "<") || strings.Contains(got, ">") {
		t.Fatalf("tags leaked: %q", got)
	}
}

func TestHTMLToTextEntities(t *testing.T) {
	got := HTMLToText("&lt;Warnung&gt; &#75;ontakt: &#x40;")
	if !strings.Contains(got, "<Warnung>") || !strings.Contains(got, "Kontakt: @") {
		t.Fatalf("entities not decoded: %q", got)
	}
}

func TestHTMLToTextMalformed(t *testing.T) {
	got := HTMLToText("kein <tag hier, aber 5 < 6")
	if !strings.Contains(got, "5 < 6") {
		t.Fatalf("malformed input mangled: %q", got)
	}
}

func TestHTMLToTextIsBounded(t *testing.T) {
	input := strings.Repeat("A", htmlToTextLimit+1024) + "<script>bösartig</script>"
	got := HTMLToText(input)
	if len(got) > htmlToTextLimit+16 {
		t.Fatalf("output not bounded: %d", len(got))
	}
}
