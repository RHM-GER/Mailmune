package classifier

import "testing"

// The embedded data comes from cmd/brandlist (CC0 Wikidata exports). These
// tests pin the contract: legitimate company domains are trusted, lookalikes
// of Wikidata brands are flagged, and built-in brand siblings stay aligned.

func TestLegitCompanyDomainFromWikidataExport(t *testing.T) {
	for _, d := range []string{"egmont-manga.de", "arri.com", "kaufland.de", "filiale.kaufland.de", "mail.basf.com"} {
		if !isLegitCompanyDomain(d) {
			t.Errorf("isLegitCompanyDomain(%q) = false, want true", d)
		}
	}
	for _, d := range []string{"egmont-manga-login.com", "arri-service.net", "de", "com"} {
		if isLegitCompanyDomain(d) {
			t.Errorf("isLegitCompanyDomain(%q) = true, want false", d)
		}
	}
}

func TestImpersonationOfWikidataBrand(t *testing.T) {
	brand, ok := impersonatedBrand("barclaycard-service-login.com")
	if !ok || brand != "barclaycard" {
		t.Errorf("impersonatedBrand = (%q, %v), want (barclaycard, true)", brand, ok)
	}
	brand, ok = impersonatedBrand("kaufland-gewinnspiel.xyz")
	if !ok || brand != "kaufland" {
		t.Errorf("impersonatedBrand = (%q, %v), want (kaufland, true)", brand, ok)
	}
}

func TestLegitDomainsAreNotImpersonating(t *testing.T) {
	for _, d := range []string{"barclaycard.co.uk", "kaufland.de", "filiale.kaufland.de", "siemens.com", "googlemail.com", "paypal.de"} {
		if brand, ok := impersonatedBrand(d); ok {
			t.Errorf("impersonatedBrand(%q) = (%q, true), want no impersonation", d, brand)
		}
	}
}

func TestCanonicalBrandCoversWikidataExport(t *testing.T) {
	if !isCanonicalBrand("egmont-manga.de") || !isCanonicalBrand("accounts.google.com") {
		t.Error("isCanonicalBrand should cover Wikidata export domains and built-in subdomains")
	}
	if isCanonicalBrand("egmont-manga-secure.com") {
		t.Error("lookalike domain must not be canonical")
	}
}

func TestSameBrandAcrossWikidataSiblings(t *testing.T) {
	if !sameBrand("barclaycard.com", "barclaycard.co.uk") {
		t.Error("barclaycard.com and barclaycard.co.uk should count as same brand")
	}
	if sameBrand("barclaycard.com", "commerzbank.de") {
		t.Error("unrelated domains must not match as same brand")
	}
}
