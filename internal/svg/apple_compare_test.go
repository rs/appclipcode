package svg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompareArcs generates SVGs with both Apple's tool and ours,
// then compares arc segments ring by ring.
func TestCompareArcs(t *testing.T) {
	if _, err := exec.LookPath("AppClipCodeGenerator"); err != nil {
		t.Skip("AppClipCodeGenerator not found")
	}

	tests := []string{
		"https://example.com",
		"https://a.co",
		"https://www.apple.com",
		"https://example.com/path",
		"https://appclip.example.com",
		"https://app.nextdns.io?p=a&p1=b",
		"https://app.nextdns.io?p=a&",
		"https://app.nextdns.io?x=a",
		"https://app.nextdns.io/bag",
		"https://app.nextdns.io/biz",
		"https://app.nextdns.io/cat",
		"https://app.nextdns.io/shop?p=a",
		"https://app.nextdns.io/use",
		"https://app.nextdns.io?p=abcdef",
		"https://app.nextdns.io/id?p=a&p1=b",
		"https://app.nextdns.io/id?p=abcdef",
		"https://qr.netflix.com/C/AAAA",
	}

	tmpDir := repoPath(".tmp", "compare")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", tmpDir, err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	for i, url := range tests {
		applePath := filepath.Join(tmpDir, fmt.Sprintf("apple_%d.svg", i))
		oursPath := filepath.Join(tmpDir, fmt.Sprintf("ours_%d.svg", i))

		cmd := exec.Command("AppClipCodeGenerator", "generate",
			"--url", url, "--index", "0", "--output", applePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("Apple tool: %v: %s", err, out)
		}

		svg, err := generateTemplateSVG(url, 0, CodeTypeCamera)
		if err != nil {
			t.Errorf("Generate %s: %v", url, err)
			continue
		}
		if err := os.WriteFile(oursPath, svg, 0o644); err != nil {
			t.Fatalf("write %s: %v", oursPath, err)
		}

		appleArcs := extractRingArcsFromFile(t, applePath)
		oursArcs := extractRingArcsFromFile(t, oursPath)

		allMatch := true
		for ring := 0; ring < 5; ring++ {
			a := appleArcs[ring]
			o := oursArcs[ring]
			if len(a) != len(o) {
				t.Errorf("%s ring%d: apple %d arcs, ours %d", url, ring+1, len(a), len(o))
				allMatch = false
				continue
			}
			for j := range a {
				if a[j] != o[j] {
					t.Errorf("%s ring%d arc%d: apple=%v ours=%v", url, ring+1, j, a[j], o[j])
					allMatch = false
					break
				}
			}
		}
		if allMatch {
			t.Logf("✓ %s", url)
		}
	}
}

// TestComprehensive validates SVG generation against Apple's generator.
//
// Coverage includes:
//   - Host formats 0/1/2
//   - Template-word paths
//   - Combined vs segmented non-template selection
//   - Segmented path subtypes: SPQ text / numeric / fixed6 / wordbook
//   - Segmented query subtypes: text / numeric / fixed6
//   - Empty-key and multi-parameter query handling
//   - Fragment-only and path/query/fragment combined mode
//   - Slash-only and trailing-slash segmented paths
//   - appclip. subdomain handling
//   - Raw `[` and `]` canonicalization accepted by the generator
func TestComprehensive(t *testing.T) {
	vectors := mergeComprehensiveVectors(loadLegacyComprehensiveVectors(t), additionalComprehensiveVectors())

	generatorPath, generatorErr := exec.LookPath("AppClipCodeGenerator")
	tmpDir := ""
	if generatorErr == nil {
		tmpDir = repoPath(".tmp", "comprehensive")
		if err := os.MkdirAll(tmpDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", tmpDir, err)
		}
		t.Cleanup(func() {
			_ = os.RemoveAll(tmpDir)
		})
	}

	passed, failed, skipped := 0, 0, 0
	for _, v := range vectors {
		applePath := ""
		switch {
		case generatorErr == nil:
			applePath = filepath.Join(tmpDir, fmt.Sprintf("%03d.svg", passed+failed+skipped))
			cmd := exec.Command(generatorPath, "generate", "--url", v.URL, "--index", "0", "--output", applePath)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("%s: Apple generator error: %v: %s", v.URL, err, strings.TrimSpace(string(out)))
				failed++
				continue
			}
		case v.File != "":
			applePath = repoPath("testdata", "apple_comprehensive", v.File)
			if _, err := os.Stat(applePath); err != nil {
				t.Logf("SKIP %s: no Apple reference SVG", v.URL)
				skipped++
				continue
			}
		default:
			t.Logf("SKIP %s: AppClipCodeGenerator not available", v.URL)
			skipped++
			continue
		}

		ourSVG, err := generateTemplateSVG(v.URL, 0, CodeTypeCamera)
		if err != nil {
			t.Errorf("%s: generate error: %v", v.URL, err)
			failed++
			continue
		}

		appleArcs := extractRingArcsFromFile(t, applePath)
		ourArcs := extractRingArcsFromBytes(t, ourSVG)

		allMatch := true
		for ring := 0; ring < 5; ring++ {
			a := appleArcs[ring]
			o := ourArcs[ring]
			if len(a) != len(o) {
				t.Errorf("%s ring%d: apple %d arcs, ours %d", v.URL, ring+1, len(a), len(o))
				allMatch = false
				continue
			}
			for j := range a {
				if a[j] != o[j] {
					t.Errorf("%s ring%d arc%d: apple=%v ours=%v", v.URL, ring+1, j, a[j], o[j])
					allMatch = false
					break
				}
			}
		}
		if allMatch {
			passed++
		} else {
			failed++
		}
	}

	t.Logf("Results: %d passed, %d failed, %d skipped", passed, failed, skipped)
	if failed > 0 {
		t.Fatalf("%d URLs produced different SVG arcs than Apple's tool", failed)
	}
}
