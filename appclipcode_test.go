package appclipcode_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appclipcode "github.com/rs/appclipcode"
)

func TestGenerate(t *testing.T) {
	svg, err := appclipcode.Generate("https://example.com", "FFFFFF", "000000", nil)
	if err != nil {
		t.Fatal(err)
	}

	s := string(svg)

	// Check SVG structure
	if !strings.Contains(s, `data-design="Fingerprint"`) {
		t.Error("missing data-design attribute")
	}
	if !strings.Contains(s, `data-payload="https://example.com"`) {
		t.Error("missing data-payload attribute")
	}
	if !strings.Contains(s, `viewBox="0 0 800 800"`) {
		t.Error("missing 800x800 viewBox")
	}
	if strings.Contains(s, `id="Badge"`) {
		t.Error("badge should not be rendered")
	}
	if !strings.Contains(s, `id="Background"`) {
		t.Error("missing background")
	}
	if !strings.Contains(s, `id="Markers"`) {
		t.Error("missing markers")
	}
	for i := 1; i <= 5; i++ {
		if !strings.Contains(s, `name="ring-`+string(rune('0'+i))+`"`) {
			t.Errorf("missing ring-%d", i)
		}
	}
	if !strings.Contains(s, `data-logo-type="Camera"`) {
		t.Error("missing camera logo")
	}

	// Check colors
	if !strings.Contains(s, `stroke:#ffffff`) {
		t.Error("missing foreground color in arcs")
	}
	if !strings.Contains(s, `fill:#000000`) {
		t.Error("missing background color")
	}
}

func TestGenerateNFC(t *testing.T) {
	svg, err := appclipcode.Generate("https://example.com", "FFFFFF", "000000", &appclipcode.Options{Type: appclipcode.CodeTypeNFC})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), `data-logo-type="phone"`) {
		t.Error("expected phone logo for NFC type")
	}
}

func TestGenerateSupportsAlphaHexColors(t *testing.T) {
	svg, err := appclipcode.Generate("https://example.com", "FFFFFF80", "00000000", &appclipcode.Options{Type: appclipcode.CodeTypeNFC})
	if err != nil {
		t.Fatal(err)
	}

	s := string(svg)
	if !strings.Contains(s, `stroke:#ffffff80`) {
		t.Error("missing foreground alpha in arcs")
	}
	if !strings.Contains(s, `fill:#00000000`) {
		t.Error("missing transparent background fill")
	}
	if !strings.Contains(s, `fill:#88888840;isolation:isolate`) {
		t.Error("missing derived third-color alpha")
	}
}

func TestGenerateWithTemplate(t *testing.T) {
	svg, err := appclipcode.GenerateWithTemplate("https://example.com", 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(svg)
	// Template 4: white on red
	if !strings.Contains(s, `stroke:#ffffff`) {
		t.Error("expected white foreground")
	}
	if !strings.Contains(s, `fill:#ff3b30`) {
		t.Error("expected red background")
	}
}

func TestInvalidURL(t *testing.T) {
	_, err := appclipcode.Generate("http://example.com", "FFFFFF", "000000", nil)
	if err == nil {
		t.Error("expected error for http URL")
	}
}

func TestInvalidColor(t *testing.T) {
	_, err := appclipcode.Generate("https://example.com", "GG0000", "000000", nil)
	if err == nil {
		t.Error("expected error for invalid hex color")
	}
}

func TestTemplates(t *testing.T) {
	templates := appclipcode.Templates()
	if len(templates) != 18 {
		t.Errorf("expected 18 templates, got %d", len(templates))
	}
}

func TestCompressURL(t *testing.T) {
	bytes, err := appclipcode.CompressURL("https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes) != 16 {
		t.Errorf("expected 16 bytes, got %d", len(bytes))
	}
}

func TestCompressURLCanonicalEscapes(t *testing.T) {
	tests := []struct {
		a string
		b string
	}{
		{a: "https://example.com/[", b: "https://example.com/%5B"},
		{a: "https://example.com/]", b: "https://example.com/%5D"},
		{a: "https://example.com?[", b: "https://example.com?%5B"},
		{a: "https://example.com#]", b: "https://example.com#%5D"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s==%s", tt.a, tt.b), func(t *testing.T) {
			a, err := appclipcode.CompressURL(tt.a)
			if err != nil {
				t.Fatalf("CompressURL(%q): %v", tt.a, err)
			}
			b, err := appclipcode.CompressURL(tt.b)
			if err != nil {
				t.Fatalf("CompressURL(%q): %v", tt.b, err)
			}
			if fmt.Sprintf("%x", a) != fmt.Sprintf("%x", b) {
				t.Fatalf("payload mismatch: %x != %x", a, b)
			}
		})
	}
}

// TestCompareBytes verifies URL compression produces byte-identical output
// to Apple's URLCompression.framework.
func TestCompareBytes(t *testing.T) {
	tests := []struct {
		url   string
		apple string
	}{
		{"https://example.com", "0000000000000000000000008e33db36"},
		{"https://a.co", "00000000000000000000000000004e2f"},
		{"https://www.apple.com", "000000000000000000000478a3cee226"},
		{"https://example.com/path", "000000000000000000238cf6cdb5d582"},
		{"https://appclip.example.com", "000000000000000000000000ae33db36"},
		{"https://app.nextdns.io?p=", "0000000000000000d02424a9f120a6e4"},
		{"https://app.nextdns.io?p=a", "000000000000001a0484953e2414dc85"},
		{"https://app.nextdns.io?p=a&p1=b", "000000000001a0484953e2414dc85031"},
		{"https://app.nextdns.io?p=a&", "000000000902424a9f120a6e3ccda046"},
		{"https://app.nextdns.io?p=abcdef", "000000003409092a7c4829b90b858ed5"},
		{"https://app.nextdns.io?&p=a&&p1=b", "000000000001a0484953e2414dc85031"},
		{"https://app.nextdns.io?x=a", "0000000000902424a9f120a6e720cea5"},
		{"https://app.nextdns.io/bag", "000000000000003409092a7c4829b809"},
		{"https://app.nextdns.io/biz", "000000000000003409092a7c4829b80a"},
		{"https://app.nextdns.io/cat", "000000000000003409092a7c4829b812"},
		{"https://app.nextdns.io/shop?p=a", "0000000000003409092a7c4829b87785"},
		{"https://app.nextdns.io/use", "000000000000003409092a7c4829b894"},
		{"https://app.nextdns.io/a?p=abcdef", "00048121254f89053722fb320b858ed5"},
		{"https://app.nextdns.io/id?p=a&p1=b", "0000000003409092a7c4829b83e85031"},
		{"https://app.nextdns.io/id?p=abcdef", "00000068121254f89053707d0b858ed5"},
		{"https://qr.netflix.com/C/AAAA", "0000000116d5992f39664d1bfa2cb2cb"},
	}
	for _, tt := range tests {
		ours, err := appclipcode.CompressURL(tt.url)
		if err != nil {
			t.Errorf("%s: %v", tt.url, err)
			continue
		}
		oursHex := fmt.Sprintf("%032x", ours)
		if oursHex != tt.apple {
			t.Errorf("✗ %s\n  apple: %s\n  ours:  %s", tt.url, tt.apple, oursHex)
		}
	}
}

// TestReadOurSVG tests that we can read back URLs from our own generated SVGs.
func TestReadOurSVG(t *testing.T) {
	urls := []string{
		"https://example.com",
		"https://a.co",
		"https://www.apple.com",
		"https://appclip.example.com",
		"https://app.nextdns.io/a?p=abcdef",
	}

	for _, url := range urls {
		svg, err := appclipcode.GenerateWithTemplate(url, 0, nil)
		if err != nil {
			t.Errorf("Generate %s: %v", url, err)
			continue
		}

		got, err := appclipcode.ReadSVG(svg)
		if err != nil {
			t.Errorf("ReadSVG %s: %v", url, err)
			continue
		}

		if got != url {
			t.Errorf("ReadSVG round-trip:\n  expected: %s\n  got:      %s", url, got)
		} else {
			t.Logf("✓ %s", url)
		}
	}
}

// TestReadAppleSVG tests reading URLs from Apple-generated SVGs.
func TestReadAppleSVG(t *testing.T) {
	tests := []struct {
		file string
		url  string
	}{
		{"testdata/apple_0.svg", "https://example.com"},
		{"testdata/apple_1.svg", "https://a.co"},
		{"testdata/apple_2.svg", "https://www.apple.com"},
		{"testdata/apple_4.svg", "https://appclip.example.com"},
	}

	for _, tt := range tests {
		data, err := os.ReadFile(tt.file)
		if err != nil {
			t.Skipf("skip %s: %v", tt.file, err)
			continue
		}

		got, err := appclipcode.ReadSVG(data)
		if err != nil {
			t.Errorf("ReadSVG %s: %v", tt.file, err)
			continue
		}

		if got != tt.url {
			t.Errorf("ReadSVG %s:\n  expected: %s\n  got:      %s", tt.file, tt.url, got)
		} else {
			t.Logf("✓ %s → %s", tt.file, got)
		}
	}
}

func TestReadOurSVGComprehensive(t *testing.T) {
	vectors := mergeComprehensiveVectors(loadLegacyComprehensiveVectors(t), additionalComprehensiveVectors())

	for _, v := range vectors {
		payload, err := appclipcode.CompressURL(v.URL)
		if err != nil {
			t.Errorf("CompressURL %s: %v", v.URL, err)
			continue
		}

		want, err := appclipcode.DecompressURL(payload)
		if err != nil {
			t.Errorf("DecompressURL %s: %v", v.URL, err)
			continue
		}

		svg, err := appclipcode.GenerateWithTemplate(v.URL, 0, nil)
		if err != nil {
			t.Errorf("Generate %s: %v", v.URL, err)
			continue
		}

		got, err := appclipcode.ReadSVG(svg)
		if err != nil {
			t.Errorf("ReadSVG %s: %v", v.URL, err)
			continue
		}

		if got != want {
			t.Errorf("ReadSVG comprehensive round-trip:\n  source:   %s\n  expected: %s\n  got:      %s", v.URL, want, got)
		}
	}
}

func TestReadAppleSVGComprehensive(t *testing.T) {
	vectors := loadLegacyComprehensiveVectors(t)

	for _, v := range vectors {
		if v.File == "" {
			continue
		}

		payload, err := appclipcode.CompressURL(v.URL)
		if err != nil {
			t.Errorf("CompressURL %s: %v", v.URL, err)
			continue
		}

		want, err := appclipcode.DecompressURL(payload)
		if err != nil {
			t.Errorf("DecompressURL %s: %v", v.URL, err)
			continue
		}

		path := filepath.Join("testdata", "apple_comprehensive", v.File)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}

		got, err := appclipcode.ReadSVG(data)
		if err != nil {
			t.Errorf("ReadSVG %s: %v", path, err)
			continue
		}

		if got != want {
			t.Errorf("ReadAppleSVG comprehensive:\n  source:   %s\n  expected: %s\n  got:      %s", v.URL, want, got)
		}
	}
}

func TestReadImageSVG(t *testing.T) {
	urls := []string{
		"https://example.com",
		"https://a.co",
		"https://www.apple.com",
	}
	for _, url := range urls {
		svg, err := appclipcode.GenerateWithTemplate(url, 0, nil)
		if err != nil {
			t.Fatalf("generate %s: %v", url, err)
		}
		got, err := appclipcode.ReadImage(svg)
		if err != nil {
			t.Errorf("ReadImage SVG %s: %v", url, err)
			continue
		}
		if got != url {
			t.Errorf("ReadImage SVG: got %q, want %q", got, url)
		}
	}
}

type comprehensiveVector struct {
	URL  string `json:"url"`
	File string `json:"file"`
}

func loadLegacyComprehensiveVectors(t testing.TB) []comprehensiveVector {
	t.Helper()

	data, err := os.ReadFile("testdata/comprehensive_vectors.json")
	if err != nil {
		t.Fatalf("read test vectors: %v", err)
	}

	var vectors []comprehensiveVector
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatalf("parse test vectors: %v", err)
	}
	return vectors
}

func additionalComprehensiveVectors() []comprehensiveVector {
	return []comprehensiveVector{
		{URL: "https://example.com/"},
		{URL: "https://example.com/A/"},
		{URL: "https://example.com/a-b"},
		{URL: "https://example.com/12345"},
		{URL: "https://example.com/product/A"},
		{URL: "https://example.com?x=y"},
		{URL: "https://example.com?x=123"},
		{URL: "https://example.com?x=A"},
		{URL: "https://example.com?=123"},
		{URL: "https://example.com?=A"},
		{URL: "https://example.com?x=a-b&z=456"},
		{URL: "https://example.com/product/123?x=A&y=456"},
		{URL: "https://example.com?x"},
		{URL: "https://example.com?x=y=z"},
		{URL: "https://example.com#frag"},
		{URL: "https://example.com/path#frag"},
		{URL: "https://example.com/path?x=1#frag"},
		{URL: "https://example.com/#frag"},
		{URL: "https://example.com/["},
		{URL: "https://example.com?x=[]"},
		{URL: "https://example.com#[]"},
		{URL: "https://my.app?x=A"},
		{URL: "https://a.co?x=A"},
		{URL: "https://appclip.example.com/open?x=A"},
	}
}

func mergeComprehensiveVectors(base, extra []comprehensiveVector) []comprehensiveVector {
	seen := make(map[string]bool, len(base)+len(extra))
	merged := make([]comprehensiveVector, 0, len(base)+len(extra))
	for _, v := range append(base, extra...) {
		if seen[v.URL] {
			continue
		}
		seen[v.URL] = true
		merged = append(merged, v)
	}
	return merged
}
