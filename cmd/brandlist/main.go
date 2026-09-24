// Command brandlist converts the CC0-licensed Wikidata SPARQL exports of
// company/brand websites into normalized data files for the classifier:
//
//   - legit_domains.txt: one registrable domain per line (trust list)
//   - brand_tokens.txt: "token<TAB>own1,own2" per line (impersonation list)
//
// Usage:
//
//	go run ./cmd/brandlist -world query_World_First_5000.json -ger query_GER_First_10000.json -out internal/classifier/data
//
// The input files are the user-provided exports of https://query.wikidata.org
// (license CC0 1.0). The tool never fetches anything from the network.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type entry struct {
	Item         string `json:"item"`
	Company      string `json:"company"`
	ItemLabel    string `json:"itemLabel"`
	CompanyLabel string `json:"companyLabel"`
	Website      string `json:"website"`
}

// genericWords are label words that must never become impersonation tokens,
// because they appear in countless unrelated legitimate domains.
var genericWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		media service services studio studios press verlag holding company
		germany deutschland international software technik system systems
		solution solutions digital online store shop shopping market trading
		consulting partner partners capital invest health pharma medical
		medizin motor motors audio video records music entertainment pictures
		films film games books book hotel hotels reisen travel tourismus
		tourism energie energy solar immobilien versicherung krankenkasse
		university universitaet college school academy schule institut
		institute zentrum center stiftung foundation verein verband gruppe
		group agency agentur global world bank banken sparkasse kredit
		versicherung auto autos car cars food eat drink coffee restaurant
		pizza bakery beauty style fashion mode clothing textil print druck
		werbung marketing design creative kunst art foto photo event events
		messe expo logistik logistics transport spedition security
		sicherheit cleaning reinigung garten garden blumen flowers tier
		tiere pets arzt klinik clinic dental zahnarzt apotheke pharmacy
		fitness gym yoga urlaub ferien haus home life sport sports news
		cloud data daten netz net web app apps mail email phone telefon
		mobil mobile kirche stadt land werk werke bau holz stahl metall
		kunststoff glas keramik papier druckerei werbeagentur webdesign
		softwareentwicklung beratung handel import export shipping cargo
		freight airline flug reise buero office arbeit jobs karriere
		personal recruitment immobilie wohnung mieten kaufen verkauf
		allnet ampere analytica alphabet andina arnold avantis barefoot
		bonita bonito boundary butter cactus cherry christ chiemsee closed
		concordia connect diplomat douglas durable ecolog getlink gloria
		gorillas gutmann hallmark hammer hellweg hengst hornets hotline
		ingame impuls interlink intermedia invitro kramer lightweight
		lilium madhouse majorette marcel meisterwerke mention metrolink
		metronet mindshare munich natgas naturenergie newday newswire
		newsworthy nordsee northstar olympia optimum orange ostermann
		panther passport pelikan picard piemont pierrot portfolium
		praktiker preserve prominent proven reformhaus reisezentrum
		repack roland roller salamander schleich schwalbe senator signal
		signum snipes snowdrop solaris sooner spectrum sportarena
		sputnik strauss streif sublimation taschen teehaus teekanne
		teleservice thalia tombola trigger ultimo ultrasonic universitas
		vicarious vollmer vorwerk warwick weidemann weihrauch wiedemann
		ziegler zigzag zufall zwischenfall
	`) {
		genericWords[w] = true
	}
}

// normalizeDomain turns a website URL into a lowercase registrable host
// without the "www." prefix. Returns "" for unusable values.
func normalizeDomain(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	host = strings.TrimSuffix(host, ".")
	if host == "" || !strings.Contains(host, ".") {
		return ""
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" {
			return ""
		}
	}
	host = strings.TrimPrefix(host, "www.")
	// Skip hosts that are obviously not registrable domains (IP addresses).
	if strings.HasPrefix(host, "[") {
		return ""
	}
	for _, r := range host {
		if r >= '0' && r <= '9' {
			continue
		}
		if (r < 'a' || r > 'z') && r != '.' && r != '-' {
			return "" // IDN/punycode or unicode hosts are skipped for now
		}
	}
	return host
}

// tokenCandidate derives an impersonation token from a label. Only clean,
// single-word, alphabetic labels of reasonable length are accepted; generic
// words are rejected.
func tokenCandidate(label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	if label == "" || strings.ContainsAny(label, " .&-_'/()") {
		return ""
	}
	if len(label) < 6 {
		return ""
	}
	for _, r := range label {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	if genericWords[label] {
		return ""
	}
	return label
}

func main() {
	world := flag.String("world", "", "path to query_World_First_5000.json")
	ger := flag.String("ger", "", "path to query_GER_First_10000.json")
	out := flag.String("out", "internal/classifier/data", "output directory")
	flag.Parse()

	domains := map[string]bool{}
	// token -> set of the brand's own domains that contain the token
	tokenOwn := map[string]map[string]bool{}

	load := func(path string) error {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var entries []entry
		if err := json.Unmarshal(raw, &entries); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		for _, e := range entries {
			d := normalizeDomain(e.Website)
			if d == "" {
				continue
			}
			domains[d] = true
			label := e.ItemLabel
			if label == "" {
				label = e.CompanyLabel
			}
			tok := tokenCandidate(label)
			if tok == "" || !strings.Contains(d, tok) {
				// The token must actually appear in the brand's own domain,
				// so impersonation matching stays grounded in real DNS names.
				continue
			}
			if tokenOwn[tok] == nil {
				tokenOwn[tok] = map[string]bool{}
			}
			tokenOwn[tok][d] = true
		}
		return nil
	}

	for _, p := range []string{*world, *ger} {
		if p == "" {
			continue
		}
		if err := load(p); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
	if len(domains) == 0 {
		fmt.Fprintln(os.Stderr, "no domains parsed; check -world/-ger paths")
		os.Exit(1)
	}

	// Expand each host with its registrable parent domains
	// (filiale.kaufland.de -> kaufland.de), skipping pseudo-TLD modifiers
	// like the "co" in co.uk so trust never leaks onto a whole TLD space.
	modifiers := map[string]bool{
		"co": true, "com": true, "net": true, "org": true, "gov": true,
		"edu": true, "ac": true, "or": true, "ne": true, "go": true,
		"adm": true, "k12": true,
	}
	var parents []string
	for d := range domains {
		labels := strings.Split(d, ".")
		for i := 1; i <= len(labels)-2; i++ {
			if modifiers[labels[i]] {
				continue
			}
			parents = append(parents, strings.Join(labels[i:], "."))
		}
	}
	for _, p := range parents {
		domains[p] = true
	}

	if err := os.MkdirAll(*out, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	stamp := time.Now().UTC().Format("2006-01-02")
	header := func(kind string) []string {
		return []string{
			"# Mailmune " + kind + " – generated by cmd/brandlist, DO NOT EDIT BY HAND.",
			"# Source: Wikidata SPARQL exports (https://query.wikidata.org), license CC0 1.0.",
			"# Inputs: query_World_First_5000.json (Q783794/Q4830453/Q431289 worldwide),",
			"#         query_GER_First_10000.json (Germany, P856, with de.wikipedia article).",
			"# Generated: " + stamp,
		}
	}

	writeLines := func(name string, head, lines []string) error {
		f, err := os.Create(filepath.Join(*out, name))
		if err != nil {
			return err
		}
		defer f.Close()
		w := bufio.NewWriter(f)
		for _, l := range append(head, lines...) {
			if _, err := fmt.Fprintln(w, l); err != nil {
				return err
			}
		}
		return w.Flush()
	}

	sortedDomains := make([]string, 0, len(domains))
	for d := range domains {
		sortedDomains = append(sortedDomains, d)
	}
	sort.Strings(sortedDomains)
	if err := writeLines("legit_domains.txt", header("legitimate company/brand domains"), sortedDomains); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	sortedTokens := make([]string, 0, len(tokenOwn))
	for t := range tokenOwn {
		sortedTokens = append(sortedTokens, t)
	}
	sort.Strings(sortedTokens)
	tokenLines := make([]string, 0, len(sortedTokens))
	for _, t := range sortedTokens {
		own := make([]string, 0, len(tokenOwn[t]))
		for d := range tokenOwn[t] {
			own = append(own, d)
		}
		sort.Strings(own)
		tokenLines = append(tokenLines, t+"\t"+strings.Join(own, ","))
	}
	if err := writeLines("brand_tokens.txt", header("brand impersonation tokens"), tokenLines); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	fmt.Printf("wrote %d domains and %d tokens to %s\n", len(sortedDomains), len(tokenLines), *out)
}
