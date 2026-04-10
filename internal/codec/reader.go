package codec

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// DecodePayload reverses the codec encoding to recover the 16-byte compressed URL payload.
// Input: the reconstructed bit vector from extractBitsFromSVG (128 gap bits + color stream).
func DecodePayload(bits []bool) ([]byte, error) {
	if len(bits) < 128 {
		return nil, fmt.Errorf("need at least 128 bits, got %d", len(bits))
	}

	// The first 128 bits are the LUT-permuted gap+meta+template bits.
	ringBits := bits[:128]

	// Step 1: Reverse LUT permutation.
	prePerm := make([]bool, 128)
	for i := 0; i < 128; i++ {
		prePerm[i] = ringBits[kGapsBitsOrderLUT[i]]
	}

	// Step 2: Extract metadata (bits 0-15, four GF(16) symbols) and RS-decode it.
	metaCodeword := make([]int, 4)
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			if prePerm[i*4+j] {
				metaCodeword[i] |= 1 << (3 - j)
			}
		}
	}

	metaSyms, err := decodeRSCodeword(gf16, metaCodeword, 2)
	if err != nil {
		return nil, fmt.Errorf("decode metadata RS: %w", err)
	}

	ver := codecVersion((metaSyms[0] << 3) | (metaSyms[1] >> 1))
	inverted := (metaSyms[1] & 1) == 1

	if ver > 1 {
		return nil, fmt.Errorf("invalid version: %d (expected 0 or 1)", ver)
	}

	zeroCount128 := 0
	for _, b := range prePerm {
		if !b {
			zeroCount128++
		}
	}

	// Step 3: Extract gap symbols (bits 16-119, thirteen GF(256) symbols).
	gapBits := prePerm[16:120]

	// Un-invert if needed.
	if inverted {
		for i := range gapBits {
			gapBits[i] = !gapBits[i]
		}
	}

	gapCodeword := make([]int, 13)
	for i := 0; i < 13; i++ {
		for j := 0; j < 8; j++ {
			if gapBits[i*8+j] {
				gapCodeword[i] |= 1 << (7 - j)
			}
		}
	}

	// Step 4: Extract data symbols (strip RS parity).
	fp := formats[ver]
	if fp.gapsDataCount+fp.gapsParityCount != 13 {
		return nil, fmt.Errorf("unexpected gap symbol count for version %d", ver)
	}
	gapSyms, err := decodeRSCodeword(gf256, gapCodeword, fp.gapsParityCount)
	if err != nil {
		return nil, fmt.Errorf("decode gap RS: %w", err)
	}
	dataSyms := gapSyms[:fp.gapsDataCount]

	// When the post-128 color stream is available (e.g. from SVG decoding),
	// recover the arcs RS block as well. The codec is systematic, so exact input
	// lets us read the arcs data symbols directly without implementing RS
	// correction here.
	var arcsData []byte
	if len(bits) > 128 {
		colorStream := bits[128:]
		if len(colorStream) >= 57 {
			if colorStream[0] {
				return nil, fmt.Errorf("invalid separator bit: got 1, want 0")
			}

			arcsBits := colorStream[1:57]
			arcsCodeword := make([]int, 7)
			for i := 0; i < 7; i++ {
				for j := 0; j < 8; j++ {
					if arcsBits[i*8+j] {
						arcsCodeword[i] |= 1 << (7 - j)
					}
				}
			}

			arcsSyms, err := decodeRSCodeword(gf256, arcsCodeword, fp.arcsParityCount)
			if err != nil {
				return nil, fmt.Errorf("decode arcs RS: %w", err)
			}

			arcsData = make([]byte, fp.arcsDataCount)
			for i := 0; i < fp.arcsDataCount; i++ {
				arcsData[i] = byte(arcsSyms[i])
			}
		}
	}

	// Step 5: Unscramble.
	totalData := fp.gapsDataCount + fp.arcsDataCount
	scrambled := make([]byte, totalData)
	for i := 0; i < fp.gapsDataCount; i++ {
		scrambled[i] = byte(dataSyms[i])
	}
	if len(arcsData) == fp.arcsDataCount {
		copy(scrambled[fp.gapsDataCount:], arcsData)
	}

	padded := make([]byte, totalData)
	for i := 0; i < totalData; i++ {
		padded[totalData-1-i] = scrambled[i] ^ 0xa5
	}

	// Step 6: Build the 16-byte payload (right-aligned).
	payload := make([]byte, 16)
	copy(payload[16-totalData:], padded)

	return payload, nil
}

// DecompressURL decompresses a 16-byte App Clip Code payload back to a URL string.
func DecompressURL(payload []byte) (string, error) {
	if err := ensureInit(); err != nil {
		return "", err
	}

	// Find the begin marker (first "1" bit scanning from MSB).
	bits := make([]bool, 128)
	for i := 0; i < 16; i++ {
		for j := 7; j >= 0; j-- {
			bits[i*8+(7-j)] = (payload[i]>>j)&1 == 1
		}
	}

	// Find begin marker.
	startBit := -1
	for i, b := range bits {
		if b {
			startBit = i + 1 // data starts after the marker
			break
		}
	}
	if startBit < 0 {
		return "", fmt.Errorf("no begin marker found")
	}

	data := bits[startBit:]
	pos := 0

	readBit := func() (bool, error) {
		if pos >= len(data) {
			return false, fmt.Errorf("unexpected end of data at bit %d", pos)
		}
		b := data[pos]
		pos++
		return b, nil
	}

	readBits := func(n int) (int, error) {
		val := 0
		for i := 0; i < n; i++ {
			b, err := readBit()
			if err != nil {
				return 0, err
			}
			val = val<<1 | boolToInt(b)
		}
		return val, nil
	}

	// Template type.
	templateBit, err := readBit()
	if err != nil {
		return "", err
	}
	templateType := boolToInt(templateBit)

	// Subdomain type.
	subBit, err := readBit()
	if err != nil {
		return "", err
	}
	hasAppClip := subBit

	// Host format.
	fmtBit, err := readBit()
	if err != nil {
		return "", err
	}
	hostFormat := 0
	if fmtBit {
		// "1X" format
		fmtBit2, err := readBit()
		if err != nil {
			return "", err
		}
		if fmtBit2 {
			hostFormat = 2 // "11" = full Huffman
		} else {
			hostFormat = 1 // "10" = fixed TLD
		}
	}

	// Decode host.
	var host string
	hasPath := false

	switch hostFormat {
	case 0: // Huffman TLD
		// TLD comes first.
		tldHC := tldHuffmanCoder()
		tldIdx, err := huffmanDecode(tldHC, data, &pos)
		if err != nil {
			return "", fmt.Errorf("decode TLD: %w", err)
		}
		if tldIdx < 0 || tldIdx >= len(tldList) {
			return "", fmt.Errorf("invalid TLD index %d", tldIdx)
		}
		tld := tldList[tldIdx]

		// Then domain characters with host Huffman coder.
		domain, err := decodeHostChars(data, &pos)
		if err != nil {
			return "", fmt.Errorf("decode domain: %w", err)
		}
		if strings.HasSuffix(domain, "|") {
			domain = domain[:len(domain)-1]
			hasPath = true
		}
		host = domain + tld

	case 1: // Fixed-length TLD (8-bit index)
		tldIdx, err := readBits(8)
		if err != nil {
			return "", err
		}
		tld, ok := fixedTLDByIndex[tldIdx]
		if !ok {
			return "", fmt.Errorf("unknown fixed TLD index %d", tldIdx)
		}

		domain, err := decodeHostChars(data, &pos)
		if err != nil {
			return "", fmt.Errorf("decode domain: %w", err)
		}
		if strings.HasSuffix(domain, "|") {
			domain = domain[:len(domain)-1]
			hasPath = true
		}
		host = domain + tld

	case 2: // Full Huffman host
		fullHost, err := decodeHostChars(data, &pos)
		if err != nil {
			return "", fmt.Errorf("decode full host: %w", err)
		}
		if strings.HasSuffix(fullHost, "|") {
			fullHost = fullHost[:len(fullHost)-1]
			hasPath = true
		}
		host = fullHost
	}

	// Reconstruct URL.
	url := "https://"
	if hasAppClip {
		url += "appclip."
	}
	url += host

	// Decode path if present.
	if hasPath {
		if templateType == 1 {
			pathQuery, err := decodeAutoQueryTemplateRest(data, &pos)
			if err != nil {
				return "", fmt.Errorf("decode template path/query: %w", err)
			}
			url += pathQuery
		} else {
			// Non-template: read type bit then decode path.
			typeBit, err := readBit()
			if err != nil {
				return "", err
			}

			if !typeBit {
				// Combined (CPQ): decode full path/query/fragment string.
				path, err := decodeCPQChars(data, &pos)
				if err != nil {
					return "", fmt.Errorf("decode cpq path: %w", err)
				}
				if path != "" && !strings.HasPrefix(path, "/") && path[0] != '#' {
					path = "/" + path
				}
				url += path
			} else {
				path, err := decodeSegmentedPathQuery(data, &pos)
				if err != nil {
					return "", fmt.Errorf("decode segmented path/query: %w", err)
				}
				url += path
			}
		}
	}

	return url, nil
}

// huffmanDecode reads bits from data starting at *pos and returns the decoded symbol index.
func huffmanDecode(hc *huffmanCoder, data []bool, pos *int) (int, error) {
	// Walk the Huffman tree from root, reading one bit per level.
	for i, code := range hc.codes {
		if code == "" {
			continue
		}
		// Check if data starting at *pos matches this code.
		if *pos+len(code) <= len(data) {
			match := true
			for j := 0; j < len(code); j++ {
				expected := code[j] == '1'
				if data[*pos+j] != expected {
					match = false
					break
				}
			}
			if match {
				*pos += len(code)
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("no matching Huffman code at position %d", *pos)
}

// decodeHostChars decodes host characters using the multi-context Huffman host coder.
// Returns when it encounters the "|" separator or runs out of data.
// decodeChars decodes symbols from a bit stream using a multi-context Huffman coder.
// If stopSym is non-empty, decoding stops after encountering that symbol.
func decodeChars(coder *multiContextHuffmanCoder, symbols []string, data []bool, pos *int, stopSym string) (string, error) {
	return decodeCharsWithStartContext(coder, symbols, data, pos, stopSym, "")
}

func decodeCharsWithStartContext(coder *multiContextHuffmanCoder, symbols []string, data []bool, pos *int, stopSym, startCtx string) (string, error) {
	if err := ensureInit(); err != nil {
		return "", err
	}

	var result strings.Builder
	nodeOffset := 0
	depth := 0

	for _, c := range startCtx {
		idx := coder.symbolIndex(string(c))
		if idx < 0 {
			return "", fmt.Errorf("unknown start context symbol: %q", string(c))
		}
		nodeOffset, depth = coder.advanceContext(nodeOffset, depth, idx)
	}

	for *pos < len(data) {
		hc := coder.coderForNode(nodeOffset)
		idx, err := huffmanDecode(hc, data, pos)
		if err != nil {
			break
		}
		if idx < 0 || idx >= len(symbols) {
			break
		}
		sym := symbols[idx]
		result.WriteString(sym)

		if stopSym != "" && sym == stopSym {
			break
		}

		// Advance trie context (sliding window of last 2 symbols)
		if depth < coder.trie.maxDepth {
			nodeOffset = coder.trie.childOffset(nodeOffset, idx)
			depth++
		} else {
			prevSymIdx := (nodeOffset - 1) % coder.trie.numSymbols
			nodeOffset = coder.trie.childOffset(1+prevSymIdx, idx)
		}
	}

	return result.String(), nil
}

func decodeHostChars(data []bool, pos *int) (string, error) {
	return decodeChars(hostCoder, hostSymbols, data, pos, "|")
}

func decodeSPQChars(data []bool, pos *int) (string, error) {
	return decodeChars(spqCoder, spqSymbols, data, pos, "")
}

func decodeSPQCharsWithStartContext(data []bool, pos *int, startCtx string) (string, error) {
	return decodeCharsWithStartContext(spqCoder, spqSymbols, data, pos, "", startCtx)
}

func decodeAutoQueryTemplateRest(data []bool, pos *int) (string, error) {
	if *pos >= len(data) {
		return "", nil
	}

	first, err := readBitFromSlice(data, pos)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	if !first {
		wordIdx, err := readBitsFromSlice(data, pos, 8)
		if err != nil {
			return "", err
		}
		word, ok := knownWordByIndex[wordIdx]
		if !ok {
			return "", fmt.Errorf("unknown template path word index %d", wordIdx)
		}
		sb.WriteByte('/')
		sb.WriteString(word)

		if *pos >= len(data) {
			return sb.String(), nil
		}

		queryIndicator, err := readBitFromSlice(data, pos)
		if err != nil {
			return "", err
		}
		if !queryIndicator {
			return "", fmt.Errorf("encountered path indicator while decoding template query")
		}
	}

	if *pos >= len(data) {
		return sb.String(), nil
	}

	sb.WriteByte('?')
	for i := 0; *pos < len(data); i++ {
		if i > 0 {
			sb.WriteByte('&')
		}
		// In Apple's template format the query keys are not encoded at all. The
		// decoder reconstructs the fixed p-family by position: p, p1, p2, ...
		sb.WriteByte('p')
		if i > 0 {
			sb.WriteString(strconv.Itoa(i))
		}
		sb.WriteByte('=')

		componentType, err := readBitsFromSlice(data, pos, 2)
		if err != nil {
			return "", err
		}

		var value string
		switch componentType {
		case 0:
			value, err = decodeSPQValueUntilTerminator(data, pos, "=")
		case 1:
			value, err = decodeULEB128String(data, pos)
		case 2:
			value, err = decodeFixed6String(data, pos)
		default:
			return "", fmt.Errorf("invalid template query component type %d", componentType)
		}
		if err != nil {
			return "", err
		}
		sb.WriteString(value)
	}

	return sb.String(), nil
}

func readBitFromSlice(data []bool, pos *int) (bool, error) {
	if *pos >= len(data) {
		return false, fmt.Errorf("unexpected end of data at bit %d", *pos)
	}
	b := data[*pos]
	*pos = *pos + 1
	return b, nil
}

func readBitsFromSlice(data []bool, pos *int, n int) (int, error) {
	val := 0
	for i := 0; i < n; i++ {
		b, err := readBitFromSlice(data, pos)
		if err != nil {
			return 0, err
		}
		val = val<<1 | boolToInt(b)
	}
	return val, nil
}

func decodeSPQValueUntilTerminator(data []bool, pos *int, startCtx string) (string, error) {
	s, err := decodeCharsWithStartContext(spqCoder, spqSymbols, data, pos, "|", startCtx)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(s, "|"), nil
}

func decodeSegmentedPathQuery(data []bool, pos *int) (string, error) {
	var pathParts []string
	rootOnly := false
	trailingSlash := false

	for *pos < len(data) {
		first, err := readBitFromSlice(data, pos)
		if err != nil {
			return "", err
		}

		if first {
			second, err := readBitFromSlice(data, pos)
			if err != nil {
				return "", err
			}
			if !second {
				if len(pathParts) == 0 {
					rootOnly = true
				}
				trailingSlash = true
				continue
			}

			query, err := decodeSegmentedQueryString(data, pos)
			if err != nil {
				return "", err
			}
			path := buildSegmentedPath(pathParts, rootOnly, trailingSlash)
			if path == "" {
				path = "/"
			}
			return path + query, nil
		}

		component, err := decodeSegmentedPathComponent(data, pos)
		if err != nil {
			return "", err
		}
		pathParts = append(pathParts, component)
		rootOnly = false
		trailingSlash = false
	}

	return buildSegmentedPath(pathParts, rootOnly, trailingSlash), nil
}

func buildSegmentedPath(pathParts []string, rootOnly, trailingSlash bool) string {
	if len(pathParts) == 0 {
		if rootOnly {
			return "/"
		}
		return ""
	}

	path := "/" + strings.Join(pathParts, "/")
	if trailingSlash {
		path += "/"
	}
	return path
}

func decodeSegmentedPathComponent(data []bool, pos *int) (string, error) {
	componentType, err := readBitsFromSlice(data, pos, 2)
	if err != nil {
		return "", err
	}

	switch componentType {
	case 0:
		return decodeSPQValueUntilTerminator(data, pos, "")
	case 1:
		return decodeULEB128String(data, pos)
	case 2:
		return decodeFixed6String(data, pos)
	case 3:
		wordIdx, err := readBitsFromSlice(data, pos, 8)
		if err != nil {
			return "", err
		}
		word, ok := knownWordByIndex[wordIdx]
		if !ok {
			return "", fmt.Errorf("unknown segmented path word index %d", wordIdx)
		}
		return word, nil
	default:
		return "", fmt.Errorf("invalid segmented path component type %d", componentType)
	}
}

func decodeSegmentedQueryString(data []bool, pos *int) (string, error) {
	var sb strings.Builder
	sb.WriteByte('?')

	firstComponent := true
	for *pos < len(data) {
		key, value, err := decodeSegmentedQueryComponent(data, pos)
		if err != nil {
			return "", err
		}
		if !firstComponent {
			sb.WriteByte('&')
		}
		firstComponent = false
		sb.WriteString(key)
		sb.WriteByte('=')
		sb.WriteString(value)
	}

	return sb.String(), nil
}

func decodeSegmentedQueryComponent(data []bool, pos *int) (string, string, error) {
	componentType, err := readBitsFromSlice(data, pos, 2)
	if err != nil {
		return "", "", err
	}

	switch componentType {
	case 0:
		key, err := decodeSPQValueUntilTerminator(data, pos, "?")
		if err != nil {
			return "", "", err
		}
		value, err := decodeSPQValueUntilTerminator(data, pos, "=")
		if err != nil {
			return "", "", err
		}
		return key, value, nil
	case 1:
		value, err := decodeULEB128String(data, pos)
		if err != nil {
			return "", "", err
		}
		key, err := decodeSPQValueUntilTerminator(data, pos, "?")
		if err != nil {
			return "", "", err
		}
		return key, value, nil
	case 2:
		key, err := decodeSPQValueUntilTerminator(data, pos, "?")
		if err != nil {
			return "", "", err
		}
		value, err := decodeFixed6String(data, pos)
		if err != nil {
			return "", "", err
		}
		return key, value, nil
	default:
		return "", "", fmt.Errorf("invalid segmented query component type %d", componentType)
	}
}

func decodeULEB128String(data []bool, pos *int) (string, error) {
	n := new(big.Int)
	shift := uint(0)
	for {
		b, err := readBitsFromSlice(data, pos, 8)
		if err != nil {
			return "", err
		}
		chunk := big.NewInt(int64(b & 0x7f))
		chunk.Lsh(chunk, shift)
		n.Add(n, chunk)
		if (b & 0x80) == 0 {
			return n.String(), nil
		}
		shift += 7
	}
}

func decodeFixed6String(data []bool, pos *int) (string, error) {
	var sb strings.Builder
	for *pos+6 <= len(data) {
		idx, err := readBitsFromSlice(data, pos, 6)
		if err != nil {
			return "", err
		}
		if idx < 0 || idx >= len(fixed6Alphabet) {
			return "", fmt.Errorf("invalid fixed6 index %d", idx)
		}
		c := fixed6Alphabet[idx]
		if c == '|' {
			break
		}
		sb.WriteByte(c)
	}
	return sb.String(), nil
}

func decodeCPQChars(data []bool, pos *int) (string, error) {
	return decodeChars(cpqCoder, cpqSymbols, data, pos, "")
}
