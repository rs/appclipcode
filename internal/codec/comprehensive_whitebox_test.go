package codec

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestComprehensiveEncodingSubtypes(t *testing.T) {
	pathCases := []struct {
		name            string
		component       string
		needsTerminator bool
		wantPrefix      string
	}{
		{name: "spq_text", component: "a-b", wantPrefix: "00"},
		{name: "numeric", component: "12345", wantPrefix: "01"},
		{name: "fixed6", component: "A", wantPrefix: "10"},
		{name: "wordbook", component: "product", wantPrefix: "11"},
	}
	for _, tc := range pathCases {
		t.Run("path_"+tc.name, func(t *testing.T) {
			bits, err := encodeSegmentedPathComponent(tc.component, tc.needsTerminator)
			if err != nil {
				t.Fatalf("encodeSegmentedPathComponent(%q): %v", tc.component, err)
			}
			if !strings.HasPrefix(bits, tc.wantPrefix) {
				t.Fatalf("encodeSegmentedPathComponent(%q) = %q, want prefix %q", tc.component, bits, tc.wantPrefix)
			}
		})
	}

	queryCases := []struct {
		name       string
		param      string
		hasMore    bool
		wantPrefix string
	}{
		{name: "text", param: "x=a-b", wantPrefix: "00"},
		{name: "text_extra_equals", param: "x=y=z", wantPrefix: "00"},
		{name: "numeric", param: "x=123", wantPrefix: "01"},
		{name: "fixed6", param: "x=A", wantPrefix: "10"},
	}
	for _, tc := range queryCases {
		t.Run("query_"+tc.name, func(t *testing.T) {
			bits, err := encodeSegmentedQueryComponent(tc.param, tc.hasMore)
			if err != nil {
				t.Fatalf("encodeSegmentedQueryComponent(%q): %v", tc.param, err)
			}
			if !strings.HasPrefix(bits, tc.wantPrefix) {
				t.Fatalf("encodeSegmentedQueryComponent(%q) = %q, want prefix %q", tc.param, bits, tc.wantPrefix)
			}
		})
	}
}

func TestComprehensiveEncodingFamilies(t *testing.T) {
	hostCases := []struct {
		url  string
		want int
	}{
		{url: "https://example.com", want: 0},
		{url: "https://app.nextdns.io", want: 1},
		{url: "https://my.app", want: 1},
		{url: "https://a.co", want: 2},
	}
	for _, tc := range hostCases {
		t.Run("host_"+tc.url, func(t *testing.T) {
			u, err := parseCompressionURL(tc.url)
			if err != nil {
				t.Fatalf("parseCompressionURL(%q): %v", tc.url, err)
			}
			_, fmtType, err := encodeHost(u.host, u.path != "" || u.query != "" || u.fragment != "")
			if err != nil {
				t.Fatalf("encodeHost(%q): %v", tc.url, err)
			}
			if fmtType != tc.want {
				t.Fatalf("encodeHost(%q) format = %d, want %d", tc.url, fmtType, tc.want)
			}
		})
	}

	modeCases := []struct {
		name         string
		path         string
		query        string
		fragment     string
		wantSelector byte
	}{
		{name: "template_word", path: "/shop"},
		{name: "segmented_slash", path: "/", wantSelector: '1'},
		{name: "segmented_query_text", query: "x=y", wantSelector: '1'},
		{name: "segmented_query_extra_equals", query: "x=y=z", wantSelector: '1'},
		{name: "combined_bare_query", query: "x", wantSelector: '0'},
		{name: "combined_fragment", fragment: "frag", wantSelector: '0'},
		{name: "combined_path_query_fragment", path: "/path", query: "x=1", fragment: "frag", wantSelector: '0'},
	}
	for _, tc := range modeCases {
		t.Run(tc.name, func(t *testing.T) {
			templateType := 0
			if tc.fragment == "" && tc.query == "" && isKnownWord(strings.TrimPrefix(tc.path, "/")) {
				templateType = 1
			}
			bits, err := encodePathQuery(tc.path, tc.query, tc.fragment, templateType)
			if err != nil {
				t.Fatalf("encodePathQuery(%q,%q,%q,%d): %v", tc.path, tc.query, tc.fragment, templateType, err)
			}
			if templateType == 1 {
				if len(bits) != 9 {
					t.Fatalf("template word encoding length = %d, want 9", len(bits))
				}
				return
			}
			if bits[0] != tc.wantSelector {
				t.Fatalf("encodePathQuery(%q,%q,%q) selector = %q, want %q", tc.path, tc.query, tc.fragment, bits[0], tc.wantSelector)
			}
		})
	}
}

func TestComprehensiveGeneratorValidationParity(t *testing.T) {
	generatorPath, err := exec.LookPath("AppClipCodeGenerator")
	if err != nil {
		t.Skip("AppClipCodeGenerator not found")
	}

	outDir := repoPath(".tmp", "comprehensive-validation")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", outDir, err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(outDir)
	})

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "accepted_bracket_path", url: "https://example.com/[", want: true},
		{name: "accepted_bracket_query", url: "https://example.com?x=[]", want: true},
		{name: "accepted_bracket_fragment", url: "https://example.com#[]", want: true},
		{name: "rejected_space", url: "https://example.com/ ", want: false},
		{name: "rejected_quote", url: "https://example.com?x=\"", want: false},
		{name: "rejected_invalid_percent", url: "https://example.com?x=%", want: false},
		{name: "rejected_backslash", url: "https://example.com#\\", want: false},
		{name: "rejected_caret", url: "https://example.com?x=^", want: false},
		{name: "rejected_backtick", url: "https://example.com?x=`", want: false},
		{name: "rejected_brace", url: "https://example.com/{", want: false},
		{name: "rejected_pipe", url: "https://example.com?x=|", want: false},
		{name: "rejected_punycode_tld", url: "https://example.xn--11b4c3d", want: false},
		{name: "rejected_punycode_label", url: "https://xn--abc.com", want: false},
		{name: "rejected_unicode", url: "https://example.com/é", want: false},
		{name: "rejected_fragment_hash", url: "https://example.com##", want: false},
		{name: "rejected_too_large", url: "https://example.com/aaaaaaaaaaaaaaaaaaa", want: false},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outPath := filepath.Join(outDir, fmt.Sprintf("%02d.svg", i))
			cmd := exec.Command(generatorPath, "generate", "--url", tc.url, "--index", "0", "--output", outPath)
			generatorErr := cmd.Run() != nil
			_, oursErr := CompressURL(tc.url)

			if tc.want {
				if generatorErr || oursErr != nil {
					t.Fatalf("expected success, generatorErr=%v oursErr=%v", generatorErr, oursErr)
				}
				return
			}
			if !generatorErr || oursErr == nil {
				t.Fatalf("expected failure, generatorErr=%v oursErr=%v", generatorErr, oursErr)
			}
		})
	}
}
