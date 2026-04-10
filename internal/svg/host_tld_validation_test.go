package svg

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rs/appclipcode/internal/codec"
)

const ianaTLDListURL = "https://data.iana.org/TLD/tlds-alpha-by-domain.txt"

func allSpecialTLDs() []string {
	tldList := codec.TLDList()
	fixedTLDIndex := codec.FixedTLDIndex()
	seen := make(map[string]bool, len(tldList)+len(fixedTLDIndex))
	for _, tld := range tldList {
		seen[tld] = true
	}
	for tld := range fixedTLDIndex {
		seen[tld] = true
	}

	out := make([]string, 0, len(seen))
	for tld := range seen {
		out = append(out, tld)
	}
	sort.Strings(out)
	return out
}

func representativeTLDURL(tld string) string {
	return "https://example" + tld
}

func compareURLAgainstApple(generatorPath, tmpDir, name, url string) error {
	applePath := filepath.Join(tmpDir, name+".svg")
	cmd := exec.Command(generatorPath, "generate", "--url", url, "--index", "0", "--output", applePath)
	appleOutput, appleErr := cmd.CombinedOutput()

	ours, oursErr := generateTemplateSVG(url, 0, CodeTypeCamera)

	switch {
	case appleErr != nil && oursErr != nil:
		return nil
	case appleErr != nil:
		return fmt.Errorf("Apple generator failed: %v: %s", appleErr, strings.TrimSpace(string(appleOutput)))
	case oursErr != nil:
		return fmt.Errorf("our generator failed: %v", oursErr)
	}

	appleSVG, err := os.ReadFile(applePath)
	if err != nil {
		return fmt.Errorf("read Apple SVG: %w", err)
	}

	diff := compareRingArcs(parseRingArcsSVG(string(appleSVG)), parseRingArcsSVG(string(ours)))
	if diff != "" {
		return fmt.Errorf("%s", diff)
	}

	return nil
}

func fetchIANATLDs() ([]string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(ianaTLDListURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", ianaTLDListURL, resp.Status)
	}

	var tlds []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tlds = append(tlds, "."+strings.ToLower(line))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return tlds, nil
}

func TestCompareSpecialTLDHostsAgainstApple(t *testing.T) {
	generatorPath, err := exec.LookPath("AppClipCodeGenerator")
	if err != nil {
		t.Skip("AppClipCodeGenerator not found")
	}

	tmpDir := repoPath(".tmp", "tld-special")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", tmpDir, err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	for _, tld := range allSpecialTLDs() {
		t.Run(strings.TrimPrefix(tld, "."), func(t *testing.T) {
			url := representativeTLDURL(tld)
			if err := compareURLAgainstApple(generatorPath, tmpDir, strings.TrimPrefix(tld, "."), url); err != nil {
				t.Fatalf("%s: %v", url, err)
			}
		})
	}
}

func TestIANAHostSweepAgainstApple(t *testing.T) {
	if os.Getenv("APPCLIPCODE_IANA_TLD_SWEEP") == "" {
		t.Skip("set APPCLIPCODE_IANA_TLD_SWEEP=1 to run the current IANA root-zone sweep")
	}

	generatorPath, err := exec.LookPath("AppClipCodeGenerator")
	if err != nil {
		t.Skip("AppClipCodeGenerator not found")
	}

	tlds, err := fetchIANATLDs()
	if err != nil {
		t.Fatalf("fetch IANA TLD list: %v", err)
	}

	tmpDir := repoPath(".tmp", "tld-iana")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", tmpDir, err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(tmpDir)
	})

	mismatches := 0
	for i, tld := range tlds {
		url := representativeTLDURL(tld)
		if err := compareURLAgainstApple(generatorPath, tmpDir, fmt.Sprintf("%04d_%s", i, strings.TrimPrefix(tld, ".")), url); err != nil {
			t.Errorf("%s: %v", url, err)
			mismatches++
			if mismatches >= 20 {
				t.Fatalf("stopping after %d mismatches", mismatches)
			}
		}
	}

	t.Logf("validated %d IANA TLDs against Apple", len(tlds))
}
