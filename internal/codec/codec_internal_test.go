package codec

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestGaloisField(t *testing.T) {
	gf := NewGF(0x11D, 256, 1)
	if gf.Exp(0) != 1 {
		t.Error("alpha^0 should be 1")
	}
	if gf.Exp(1) != 2 {
		t.Error("alpha^1 should be 2")
	}
	if gf.Exp(255) != gf.Exp(0) {
		t.Error("GF(256) should be cyclic with period 255")
	}
	if gf.Multiply(0, 5) != 0 {
		t.Error("0 * x should be 0")
	}
	if gf.Multiply(1, 5) != 5 {
		t.Error("1 * x should be x")
	}
}

func TestReedSolomon(t *testing.T) {
	gf := NewGF(0x11D, 256, 1)
	rs := NewRSEncoder(gf, 2)
	data := []int{0, 0, 0, 0, 0}
	result := rs.Encode(data)
	if len(result) != 7 {
		t.Errorf("expected 7 symbols, got %d", len(result))
	}
	for i := 5; i < 7; i++ {
		if result[i] != 0 {
			t.Errorf("parity[%d] = %d, expected 0 for all-zero data", i-5, result[i])
		}
	}
}

func TestEncodePayload(t *testing.T) {
	payload := make([]byte, 16)
	payload[14] = 0x42
	payload[15] = 0xAA

	bits, err := EncodePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(bits) < 129 {
		t.Errorf("expected at least 129 bits, got %d", len(bits))
	}
}

func TestComprehensivePayloadRoundTrip(t *testing.T) {
	vectors := mergeComprehensiveVectors(loadLegacyComprehensiveVectors(t), additionalComprehensiveVectors())

	for _, v := range vectors {
		payload, err := CompressURL(v.URL)
		if err != nil {
			t.Errorf("CompressURL %s: %v", v.URL, err)
			continue
		}

		bits, err := EncodePayload(payload)
		if err != nil {
			t.Errorf("EncodePayload %s: %v", v.URL, err)
			continue
		}

		recovered, err := DecodePayload(bits)
		if err != nil {
			t.Errorf("DecodePayload %s: %v", v.URL, err)
			continue
		}

		if got, want := hex.EncodeToString(recovered), hex.EncodeToString(payload); got != want {
			t.Errorf("payload round-trip %s:\n  want: %s\n  got:  %s", v.URL, want, got)
		}
	}
}

func TestDecodePayload(t *testing.T) {
	payload := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x8e, 0x33, 0xdb, 0x36}

	bits, err := EncodePayload(payload)
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := DecodePayload(bits[:128])
	if err != nil {
		t.Fatal(err)
	}

	if recovered[12] != 0x8e || recovered[13] != 0x33 ||
		recovered[14] != 0xdb || recovered[15] != 0x36 {
		t.Errorf("payload recovery failed: got %x", recovered)
	}

	fullPayload, err := hex.DecodeString("00048121254f89053722fb320b858ed5")
	if err != nil {
		t.Fatalf("decode full payload: %v", err)
	}

	fullBits, err := EncodePayload(fullPayload)
	if err != nil {
		t.Fatalf("encode full payload: %v", err)
	}

	recoveredFull, err := DecodePayload(fullBits)
	if err != nil {
		t.Fatalf("decode full payload bits: %v", err)
	}

	if got, want := hex.EncodeToString(recoveredFull), hex.EncodeToString(fullPayload); got != want {
		t.Fatalf("full payload recovery failed:\n  want: %s\n  got:  %s", want, got)
	}
}

func TestDecompressURL(t *testing.T) {
	tests := []struct {
		url     string
		payload string
	}{
		{"https://example.com", "0000000000000000000000008e33db36"},
		{"https://a.co", "00000000000000000000000000004e2f"},
		{"https://www.apple.com", "000000000000000000000478a3cee226"},
		{"https://example.com/path", "000000000000000000238cf6cdb5d582"},
		{"https://example.com#frag", "00000000000004719ed9b68f63272dea"},
		{"https://appclip.example.com", "000000000000000000000000ae33db36"},
		{"https://app.nextdns.io/?x=a", "0000000000902424a9f120a6e720cea5"},
		{"https://app.nextdns.io?p=", "0000000000000000d02424a9f120a6e4"},
		{"https://app.nextdns.io?p=a", "000000000000001a0484953e2414dc85"},
		{"https://app.nextdns.io?p=a&p1=b", "000000000001a0484953e2414dc85031"},
		{"https://app.nextdns.io?p=abcdef", "000000003409092a7c4829b90b858ed5"},
		{"https://app.nextdns.io/a?p=abcdef", "00048121254f89053722fb320b858ed5"},
		{"https://app.nextdns.io/bag", "000000000000003409092a7c4829b809"},
		{"https://app.nextdns.io/biz", "000000000000003409092a7c4829b80a"},
		{"https://app.nextdns.io/cat", "000000000000003409092a7c4829b812"},
		{"https://app.nextdns.io/shop?p=a", "0000000000003409092a7c4829b87785"},
		{"https://app.nextdns.io/id?p=a&p1=b", "0000000003409092a7c4829b83e85031"},
		{"https://app.nextdns.io/id?p=abcdef", "00000068121254f89053707d0b858ed5"},
		{"https://app.nextdns.io/use", "000000000000003409092a7c4829b894"},
	}

	for _, tt := range tests {
		payload, err := hex.DecodeString(tt.payload)
		if err != nil {
			t.Fatalf("decode payload %q: %v", tt.payload, err)
		}

		url, err := DecompressURL(payload)
		if err != nil {
			t.Errorf("DecompressURL %s: %v", tt.url, err)
			continue
		}

		if url != tt.url {
			t.Errorf("DecompressURL:\n  expected: %s\n  got:      %s", tt.url, url)
		}
	}
}

func TestDecodeCPQRoundTrip(t *testing.T) {
	if err := ensureInit(); err != nil {
		t.Fatal(err)
	}

	tests := []string{
		"?x=a",
		"a?p=abcdef",
	}

	for _, tt := range tests {
		bitString, err := cpqCoder.Encode(toCharSlice(tt))
		if err != nil {
			t.Fatalf("encode %q: %v", tt, err)
		}

		bits := make([]bool, len(bitString))
		for i := range bitString {
			bits[i] = bitString[i] == '1'
		}

		pos := 0
		got, err := decodeCPQChars(bits, &pos)
		if err != nil {
			t.Fatalf("decode %q: %v", tt, err)
		}
		if got != tt {
			t.Fatalf("cpq round-trip failed:\n  want: %q\n  got:  %q", tt, got)
		}
	}
}

func TestRandomURLs(t *testing.T) {
	data, err := os.ReadFile(repoPath("testdata", "random_vectors.json"))
	if err != nil {
		t.Skip("testdata/random_vectors.json not found")
	}

	var vectors []struct {
		URL   string `json:"url"`
		Bytes string `json:"bytes"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}

	pass, fail := 0, 0
	for _, v := range vectors {
		ours, err := CompressURL(v.URL)
		if err != nil {
			t.Logf("✗ %s: ERROR %v", v.URL, err)
			fail++
			continue
		}
		oursHex := fmt.Sprintf("%032x", ours)
		if oursHex == v.Bytes {
			pass++
		} else {
			t.Errorf("✗ %s\n  apple: %s\n  ours:  %s", v.URL, v.Bytes, oursHex)
			fail++
		}
	}

	t.Logf("Results: %d/%d passed, %d failed", pass, len(vectors), fail)
}

func TestCompressURLValidation(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{name: "punycode tld", url: "https://example.xn--11b4c3d", wantErr: "unsupported host characters"},
		{name: "punycode label", url: "https://xn--abc.com", wantErr: "unsupported host characters"},
		{name: "raw space", url: "https://example.com/ ", wantErr: "unsupported characters"},
		{name: "invalid percent", url: "https://example.com?x=%", wantErr: "invalid percent escape"},
		{name: "raw unicode", url: "https://example.com/é", wantErr: "unsupported characters"},
		{name: "too large", url: "https://example.com/aaaaaaaaaaaaaaaaaaa", wantErr: "compressed URL too large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompressURL(tt.url)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("CompressURL(%q) error = %v, want substring %q", tt.url, err, tt.wantErr)
			}
		})
	}
}
