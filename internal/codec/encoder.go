package codec

import (
	"fmt"
	"math/big"
	"strings"
	"sync"
	"unicode/utf8"
)

var initOnce sync.Once
var initErr error

func ensureInit() error {
	initOnce.Do(func() {
		initErr = initCoders()
	})
	return initErr
}

// CompressURL compresses a URL into 16 bytes suitable for the App Clip Code codec.
func CompressURL(rawURL string) ([]byte, error) {
	if err := ensureInit(); err != nil {
		return nil, fmt.Errorf("init coders: %w", err)
	}

	u, err := parseCompressionURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	host := u.host

	// Check for appclip. subdomain
	subdomainType := 0
	if strings.HasPrefix(host, "appclip.") {
		subdomainType = 1
		host = host[len("appclip."):]
	}

	// Determine path/query
	pathStr := u.path
	hasPathOrQuery := pathStr != "" || u.query != "" || u.fragment != ""

	templateType := 0
	pqBits := ""
	if hasPathOrQuery {
		pqBits, templateType, err = choosePathQueryEncoding(pathStr, u.query, u.fragment)
		if err != nil {
			return nil, fmt.Errorf("encode path/query: %w", err)
		}
	}

	// Build the raw encoded bit string.
	// Format: [1=begin_marker][template_type][subdomain_type][host_format][host_data][path_data]
	var bits string
	bits += "1" // begin marker

	// Template type bit
	bits += boolBit(templateType == 1)

	// Subdomain type bit
	bits += boolBit(subdomainType == 1)

	// Encode host
	hostBits, hostFmt, err := encodeHost(host, hasPathOrQuery)
	if err != nil {
		return nil, fmt.Errorf("encode host: %w", err)
	}

	// Host format bits: "0" for format 0, "10" for format 1, "11" for format 2
	switch hostFmt {
	case 0:
		bits += "0"
	case 1:
		bits += "10"
	case 2:
		bits += "11"
	}

	// Host encoded data
	bits += hostBits

	// Path/query encoding (if "|" was appended to host, path data follows)
	bits += pqBits

	// Convert to 16 bytes: zero-pad on the left to 128 bits
	return rawBitsToBytes(bits)
}

func boolBit(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func isKnownWord(path string) bool {
	_, ok := knownWordIndex[path]
	return ok
}

func choosePathQueryEncoding(path, query, fragment string) (string, int, error) {
	type candidate struct {
		bits         string
		templateType int
	}

	var candidates []candidate

	if bits, err := encodeTemplatePathQuery(path, query, fragment); err == nil {
		candidates = append(candidates, candidate{bits: bits, templateType: 1})
	}

	if bits, err := encodeNonTemplatePathQuery(path, query, fragment); err == nil {
		candidates = append(candidates, candidate{bits: bits, templateType: 0})
	}

	if len(candidates) == 0 {
		return "", 0, fmt.Errorf("cannot encode path/query")
	}

	best := candidates[0]
	for _, cand := range candidates[1:] {
		if len(cand.bits) < len(best.bits) {
			best = cand
			continue
		}
		// Apple prefers non-template encoding when lengths tie.
		if len(cand.bits) == len(best.bits) && best.templateType == 1 && cand.templateType == 0 {
			best = cand
		}
	}

	return best.bits, best.templateType, nil
}

func encodeTemplatePathQuery(path, query, fragment string) (string, error) {
	if fragment != "" {
		return "", fmt.Errorf("template mode does not support fragments")
	}

	var bits strings.Builder

	pathWord, params, ok := matchAutoQueryTemplate(path, query)
	if !ok {
		return "", fmt.Errorf("path/query do not match template auto-query format")
	}

	if pathWord != "" {
		idx := knownWordIndex[pathWord]
		if idx > 0xff {
			return "", fmt.Errorf("template path word %q exceeds 8-bit auto-query range", pathWord)
		}
		bits.WriteByte('0')
		bits.WriteString(intToBits(idx, 8))
	}

	if len(params) > 0 {
		bits.WriteByte('1')
		for i, param := range params {
			componentBits, err := encodeAutoQueryTemplateQueryComponent(param, i+1 < len(params))
			if err != nil {
				return "", fmt.Errorf("encode template query component %q: %w", param, err)
			}
			bits.WriteString(componentBits)
		}
	}

	if bits.Len() == 0 {
		return "", fmt.Errorf("template mode requires a path word or auto-query parameters")
	}

	return bits.String(), nil
}

// matchAutoQueryTemplate implements Apple's PathWordBookAndAutoQueryTemplateFormat.
//
// The query-key treatment here is intentionally specific: only the fixed
// p-family ("p", then "p1", "p2", ...) can use template mode. Other query
// keys always fall back to non-template encoding.
//
// That p-family requirement is observed behavior from URLCompression.framework;
// the binary does not explain why Apple chose "p". A plausible motivation is
// Apple's own App Clip invocation URL shape, e.g.
// https://appclip.apple.com/id?p=<bundleId>, but that rationale is an
// inference, not something named by the framework itself.
func matchAutoQueryTemplate(path, query string) (pathWord string, params []string, ok bool) {
	if len(path) >= 2 && strings.HasSuffix(path, "/") {
		return "", nil, false
	}
	if strings.HasSuffix(query, "&") {
		return "", nil, false
	}

	pathParts := splitNonEmpty(path, '/')
	if len(pathParts) > 1 {
		return "", nil, false
	}
	if len(pathParts) == 1 {
		idx, found := knownWordIndex[pathParts[0]]
		if !found || idx > 0xff {
			return "", nil, false
		}
		pathWord = pathParts[0]
	}

	params = splitNonEmpty(query, '&')
	if len(params) == 0 {
		return pathWord, nil, pathWord != "" || path == "/"
	}

	for i, param := range params {
		key, _, hasValue := strings.Cut(param, "=")
		if !hasValue {
			return "", nil, false
		}
		// The template query keys are implicit by position and hard-coded to the
		// p-family only; no other key names participate in this format.
		wantKey := "p"
		if i > 0 {
			wantKey += fmt.Sprintf("%d", i)
		}
		if key != wantKey {
			return "", nil, false
		}
	}

	return pathWord, params, true
}

func splitNonEmpty(s string, sep byte) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, string(sep))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func encodeAutoQueryTemplateQueryComponent(param string, hasMore bool) (string, error) {
	_, value, ok := strings.Cut(param, "=")
	if !ok {
		return "", fmt.Errorf("template query parameter %q missing '='", param)
	}

	bestBits := ""
	if bits, err := encodeSPQValue("=", value, hasMore); err == nil {
		bestBits = "00" + bits
	}
	if bits, err := encodeULEB128Value(value); err == nil {
		bestBits = shorterBits(bestBits, "01"+bits)
	}
	if bits, err := encodeFixed6Value(value, hasMore); err == nil {
		bestBits = shorterBits(bestBits, "10"+bits)
	}
	if bestBits == "" {
		return "", fmt.Errorf("cannot encode template query value from %q", param)
	}
	return bestBits, nil
}

func encodePathQuery(path, query, fragment string, templateType int) (string, error) {
	if templateType == 1 {
		return encodeTemplatePathQuery(path, query, fragment)
	}
	return encodeNonTemplatePathQuery(path, query, fragment)
}

// encodeHost encodes the hostname and returns the encoded bits and format type.
func encodeHost(host string, hasPathOrQuery bool) (string, int, error) {
	// Separate TLD
	lastDot := strings.LastIndex(host, ".")
	if lastDot < 0 {
		return "", 0, fmt.Errorf("host has no TLD: %q", host)
	}
	tld := host[lastDot:]
	domain := host[:lastDot]

	// Append "|" separator if path/query follows
	if hasPathOrQuery {
		domain += "|"
	}

	domainChars := toCharSlice(domain)

	// Try Huffman TLD encoding (format 0)
	tldHC := tldHuffmanCoder()
	tldIdx := -1
	for i, t := range tldList {
		if t == tld {
			tldIdx = i
			break
		}
	}

	if tldIdx >= 0 && tldHC.canEncode(tldIdx) {
		domainBits, err := hostCoder.Encode(domainChars)
		if err == nil {
			tldBits := tldHC.encode(tldIdx)
			// TLD bits come FIRST, then domain bits
			return tldBits + domainBits, 0, nil
		}
	}

	// Try fixed-length TLD encoding (format 1): 8-bit index
	if tldIdx, ok := fixedTLDIndex[tld]; ok {
		domainBits, err := hostCoder.Encode(domainChars)
		if err == nil {
			fBits := intToBits(tldIdx, 8)
			// TLD bits come FIRST, then domain bits
			return fBits + domainBits, 1, nil
		}
	}

	// Fallback: encode full host with Huffman (format 2)
	fullHost := host
	if hasPathOrQuery {
		fullHost += "|"
	}
	hostChars := toCharSlice(fullHost)
	allBits, err := hostCoder.Encode(hostChars)
	if err != nil {
		return "", 0, fmt.Errorf("encode full host %q: %w", host, err)
	}
	return allBits, 2, nil
}

func encodeNonTemplatePathQuery(path, query, fragment string) (string, error) {
	combinedBits, combinedErr := encodeCombinedPathQuery(path, query, fragment)
	segmentedBits, segmentedErr := encodeSegmentedPathQuery(path, query, fragment)

	switch {
	case combinedErr == nil && segmentedErr == nil:
		// Apple uses `0` for combined and `1` for segmented, and prefers combined on ties.
		if len(combinedBits) <= len(segmentedBits) {
			return "0" + combinedBits, nil
		}
		return "1" + segmentedBits, nil
	case combinedErr == nil:
		return "0" + combinedBits, nil
	case segmentedErr == nil:
		return "1" + segmentedBits, nil
	default:
		return "", fmt.Errorf("cannot encode path/query: combined: %v, segmented: %v", combinedErr, segmentedErr)
	}
}

// rawBitsToBytes converts a raw bit string (starting with "1" begin marker)
// to a 16-byte payload by zero-padding on the left to 128 bits.
func rawBitsToBytes(bits string) ([]byte, error) {
	if len(bits) > 128 {
		return nil, fmt.Errorf("compressed URL too large: %d bits (max 128)", len(bits))
	}

	// Left-pad with zeros to 128 bits
	padded := strings.Repeat("0", 128-len(bits)) + bits

	// Convert to bytes
	result := make([]byte, 16)
	for i := 0; i < 16; i++ {
		var b byte
		for j := 0; j < 8; j++ {
			if padded[i*8+j] == '1' {
				b |= 1 << (7 - j)
			}
		}
		result[i] = b
	}

	return result, nil
}

type compressionURL struct {
	host     string
	path     string
	query    string
	fragment string
}

type urlComponentKind uint8

const (
	pathComponent urlComponentKind = iota
	queryComponent
	fragmentComponent
)

func parseCompressionURL(rawURL string) (compressionURL, error) {
	const scheme = "https://"
	if len(rawURL) < len(scheme) || !strings.EqualFold(rawURL[:len(scheme)], scheme) {
		return compressionURL{}, fmt.Errorf("URL scheme must be https")
	}

	rest := rawURL[len(scheme):]
	authorityEnd := strings.IndexAny(rest, "/?#")
	authority := rest
	suffix := ""
	if authorityEnd >= 0 {
		authority = rest[:authorityEnd]
		suffix = rest[authorityEnd:]
	}

	if authority == "" {
		return compressionURL{}, fmt.Errorf("URL must have a host")
	}
	if strings.Contains(authority, "@") {
		return compressionURL{}, fmt.Errorf("URL must not have user info")
	}
	if strings.Contains(authority, ":") {
		return compressionURL{}, fmt.Errorf("URL must not have a port")
	}
	host, err := canonicalizeHost(authority)
	if err != nil {
		return compressionURL{}, err
	}

	out := compressionURL{
		host: host,
	}

	if strings.HasPrefix(suffix, "/") {
		pathEnd := len(suffix)
		if i := strings.IndexAny(suffix, "?#"); i >= 0 {
			pathEnd = i
		}
		out.path, err = canonicalizeURLComponent(suffix[:pathEnd], pathComponent)
		if err != nil {
			return compressionURL{}, err
		}
		suffix = suffix[pathEnd:]
	}

	if strings.HasPrefix(suffix, "?") {
		suffix = suffix[1:]
		queryEnd := len(suffix)
		if i := strings.IndexByte(suffix, '#'); i >= 0 {
			queryEnd = i
		}
		out.query, err = canonicalizeURLComponent(suffix[:queryEnd], queryComponent)
		if err != nil {
			return compressionURL{}, err
		}
		suffix = suffix[queryEnd:]
	}

	if strings.HasPrefix(suffix, "#") {
		out.fragment, err = canonicalizeURLComponent(suffix[1:], fragmentComponent)
		if err != nil {
			return compressionURL{}, err
		}
	}

	return out, nil
}

func canonicalizeHost(authority string) (string, error) {
	lower := strings.ToLower(authority)
	for i := 0; i < len(lower); i++ {
		c := lower[i]
		if c >= utf8.RuneSelf {
			return "", fmt.Errorf("URL contains unsupported host characters")
		}
		if ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') || c == '.' || c == '-' {
			continue
		}
		return "", fmt.Errorf("URL contains unsupported host characters")
	}

	for _, label := range strings.Split(lower, ".") {
		if strings.HasPrefix(label, "xn--") {
			return "", fmt.Errorf("URL contains unsupported host characters")
		}
	}
	return lower, nil
}

func canonicalizeURLComponent(s string, kind urlComponentKind) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c == '%' {
			if i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
				b.WriteByte('%')
				b.WriteByte(s[i+1])
				b.WriteByte(s[i+2])
				i += 3
				continue
			}
			return "", fmt.Errorf("URL contains invalid percent escape")
		}
		if c < 0x20 || c == 0x7f || c >= utf8.RuneSelf {
			return "", fmt.Errorf("URL contains unsupported characters")
		}
		if rejectsRawURLComponentByte(c, kind) {
			return "", fmt.Errorf("URL contains unsupported characters")
		}
		if c < utf8.RuneSelf {
			if isAllowedURLComponentByte(c, kind) {
				b.WriteByte(c)
			} else {
				writePercentEncodedByte(&b, c)
			}
			i++
			continue
		}
	}
	return b.String(), nil
}

func rejectsRawURLComponentByte(c byte, kind urlComponentKind) bool {
	switch c {
	case ' ', '"', '%', '<', '>', '\\', '^', '`', '{', '|', '}':
		return true
	case '#':
		return kind == fragmentComponent
	default:
		return false
	}
}

func isAllowedURLComponentByte(c byte, kind urlComponentKind) bool {
	if isASCIIAlphaNum(c) {
		return true
	}
	switch c {
	case '-', '.', '_', '~',
		'!', '$', '&', '\'', '(', ')', '*', '+', ',', ';', '=',
		':', '@':
		return true
	case '/':
		return true
	case '?':
		return kind != pathComponent
	case '#':
		return false
	default:
		return false
	}
}

func isASCIIAlphaNum(c byte) bool {
	return ('0' <= c && c <= '9') || ('A' <= c && c <= 'Z') || ('a' <= c && c <= 'z')
}

func isHexDigit(c byte) bool {
	return ('0' <= c && c <= '9') || ('A' <= c && c <= 'F') || ('a' <= c && c <= 'f')
}

func writePercentEncodedByte(b *strings.Builder, c byte) {
	const hex = "0123456789ABCDEF"
	b.WriteByte('%')
	b.WriteByte(hex[c>>4])
	b.WriteByte(hex[c&0x0f])
}

func intToBits(val, numBits int) string {
	var sb strings.Builder
	for i := numBits - 1; i >= 0; i-- {
		if (val>>i)&1 == 1 {
			sb.WriteByte('1')
		} else {
			sb.WriteByte('0')
		}
	}
	return sb.String()
}

func encodeCombinedPathQuery(path, query, fragment string) (string, error) {
	var s strings.Builder
	s.WriteString(path)
	if query != "" {
		s.WriteByte('?')
		s.WriteString(query)
	}
	if fragment != "" {
		s.WriteByte('#')
		s.WriteString(fragment)
	}

	combined := s.String()
	if strings.HasPrefix(combined, "/") && (len(combined) == 1 || combined[1] != '#') {
		combined = combined[1:]
	}
	if combined == "" {
		return "", fmt.Errorf("combined path/query is empty")
	}

	return cpqCoder.Encode(toCharSlice(combined))
}

func encodeSegmentedPathQuery(path, query, fragment string) (string, error) {
	if fragment != "" {
		return "", fmt.Errorf("segmented mode does not support fragments")
	}

	items := buildSegmentedPathItems(path)
	var bits strings.Builder

	for i, item := range items {
		if item == "/" {
			bits.WriteString("10")
			continue
		}

		hasMorePathItems := i+1 < len(items)
		componentBits, err := encodeSegmentedPathComponent(item, hasMorePathItems || query != "")
		if err != nil {
			return "", err
		}
		bits.WriteByte('0')
		bits.WriteString(componentBits)
	}

	if query != "" {
		params := strings.Split(query, "&")
		if len(params) == 0 {
			return "", fmt.Errorf("invalid segmented query")
		}
		bits.WriteString("11")
		for i, param := range params {
			componentBits, err := encodeSegmentedQueryComponent(param, i+1 < len(params))
			if err != nil {
				return "", err
			}
			bits.WriteString(componentBits)
		}
	}

	if bits.Len() == 0 {
		return "", fmt.Errorf("segmented path/query is empty")
	}
	return bits.String(), nil
}

func buildSegmentedPathItems(path string) []string {
	if path == "" {
		return nil
	}

	trimmed := strings.TrimPrefix(path, "/")
	parts := strings.Split(trimmed, "/")
	items := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		if part == "" {
			continue
		}
		items = append(items, part)
	}

	if len(items) == 0 || strings.HasSuffix(path, "/") {
		items = append(items, "/")
	}
	return items
}

func encodeSegmentedPathComponent(component string, needsTerminator bool) (string, error) {
	if component == "" {
		return "", fmt.Errorf("cannot encode empty path component")
	}

	bestBits := ""

	if bits, err := encodeSPQValue("", component, needsTerminator); err == nil {
		bestBits = "00" + bits
	}
	if bits, err := encodeULEB128Value(component); err == nil {
		bestBits = shorterBits(bestBits, "01"+bits)
	}
	if bits, err := encodeFixed6Value(component, needsTerminator); err == nil {
		bestBits = shorterBits(bestBits, "10"+bits)
	}
	if bits, err := encodeKnownWordValue(component); err == nil {
		bestBits = shorterBits(bestBits, "11"+bits)
	}

	if bestBits == "" {
		return "", fmt.Errorf("cannot encode segmented path component %q", component)
	}
	return bestBits, nil
}

func encodeSegmentedQueryComponent(param string, hasMore bool) (string, error) {
	key, value, ok := strings.Cut(param, "=")
	if !ok {
		return "", fmt.Errorf("cannot encode segmented query parameter %q", param)
	}

	keyWithTerminator, err := encodeSPQValue("?", key, true)
	if err != nil {
		return "", fmt.Errorf("encode query key %q: %w", key, err)
	}
	keyNoTerminator, err := encodeSPQValue("?", key, hasMore)
	if err != nil {
		return "", fmt.Errorf("encode query key %q: %w", key, err)
	}

	bestBits := ""
	if bits, err := encodeSPQValue("=", value, hasMore); err == nil {
		bestBits = "00" + keyWithTerminator + bits
	}
	if bits, err := encodeULEB128Value(value); err == nil {
		bestBits = shorterBits(bestBits, "01"+bits+keyNoTerminator)
	}
	if bits, err := encodeFixed6Value(value, hasMore); err == nil {
		bestBits = shorterBits(bestBits, "10"+keyWithTerminator+bits)
	}

	if bestBits == "" {
		return "", fmt.Errorf("cannot encode segmented query parameter %q", param)
	}
	return bestBits, nil
}

func encodeSPQValue(startContext, value string, needsTerminator bool) (string, error) {
	s := value
	if needsTerminator {
		s += "|"
	}
	return spqCoder.EncodeWithStartContext(toCharSlice(s), startContext)
}

func encodeFixed6Value(value string, needsTerminator bool) (string, error) {
	s := value
	if needsTerminator {
		s += "|"
	}
	return encodeFixed6(s)
}

func encodeKnownWordValue(value string) (string, error) {
	idx, ok := knownWordIndex[value]
	if !ok || idx > 0xff {
		return "", fmt.Errorf("unknown word %q", value)
	}
	return intToBits(idx, 8), nil
}

func encodeULEB128Value(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("empty numeric value")
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("non-decimal digit in %q", value)
		}
	}

	n := new(big.Int)
	if _, ok := n.SetString(value, 10); !ok {
		return "", fmt.Errorf("invalid integer %q", value)
	}

	var bytes []byte
	if n.Sign() == 0 {
		bytes = []byte{0}
	} else {
		sevenBits := big.NewInt(0x7f)
		for n.Sign() > 0 {
			chunk := new(big.Int).And(n, sevenBits)
			n.Rsh(n, 7)
			b := byte(chunk.Uint64())
			if n.Sign() > 0 {
				b |= 0x80
			}
			bytes = append(bytes, b)
		}
	}

	var bits strings.Builder
	for _, b := range bytes {
		bits.WriteString(intToBits(int(b), 8))
	}
	return bits.String(), nil
}

var fixed6Alphabet = []byte(".0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz|")
var fixed6Index = func() map[byte]int {
	m := make(map[byte]int, len(fixed6Alphabet))
	for i, b := range fixed6Alphabet {
		m[b] = i
	}
	return m
}()

func encodeFixed6(value string) (string, error) {
	var bits strings.Builder
	for i := 0; i < len(value); i++ {
		idx, ok := fixed6Index[value[i]]
		if !ok {
			return "", fmt.Errorf("symbol %q not encodable by fixed6", value[i])
		}
		bits.WriteString(intToBits(idx, 6))
	}
	return bits.String(), nil
}

func shorterBits(current, candidate string) string {
	if current == "" || len(candidate) < len(current) {
		return candidate
	}
	return current
}

// toCharSlice splits a string into a slice of single-character strings.
func toCharSlice(s string) []string {
	chars := make([]string, len(s))
	for i, c := range s {
		chars[i] = string(c)
	}
	return chars
}
