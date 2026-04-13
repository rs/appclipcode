package svg

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/appclipcode/internal/codec"
)

func repoPath(parts ...string) string {
	elems := append([]string{"..", ".."}, parts...)
	return filepath.Join(elems...)
}

type comprehensiveVector struct {
	URL  string `json:"url"`
	File string `json:"file"`
}

func loadLegacyComprehensiveVectors(t testing.TB) []comprehensiveVector {
	t.Helper()

	data, err := os.ReadFile(repoPath("testdata", "comprehensive_vectors.json"))
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

func testTemplatePalette(index int) (Palette, error) {
	switch index {
	case 0:
		return Palette{
			Foreground: Color{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
			Background: Color{R: 0x00, G: 0x00, B: 0x00, A: 0xFF},
			Third:      Color{R: 0x88, G: 0x88, B: 0x88, A: 0xFF},
		}, nil
	case 1:
		return Palette{
			Foreground: Color{R: 0x00, G: 0x00, B: 0x00, A: 0xFF},
			Background: Color{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF},
			Third:      Color{R: 0x88, G: 0x88, B: 0x88, A: 0xFF},
		}, nil
	default:
		return Palette{}, fmt.Errorf("unsupported test template index %d", index)
	}
}

func generateTemplateSVG(rawURL string, templateIndex int, codeType CodeType) ([]byte, error) {
	pal, err := testTemplatePalette(templateIndex)
	if err != nil {
		return nil, err
	}

	compressed, err := codec.CompressURL(rawURL)
	if err != nil {
		return nil, err
	}
	bits, err := codec.EncodePayload(compressed)
	if err != nil {
		return nil, err
	}
	return RenderSVG(bits, pal, rawURL, codeType), nil
}
