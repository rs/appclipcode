package codec

import (
	"embed"
	"encoding/binary"
	"fmt"
	"strings"
)

//go:embed data/h.data data/cpq.data data/spq.data
var dataFS embed.FS

// symbolFrequencyTrie is a k-ary trie stored as a flat array.
// Each node contains numSymbols uint16 frequency values (big-endian).
// Children: childOffset = numSymbols * parentOffset + 1 + symbolIndex
type symbolFrequencyTrie struct {
	data       []byte
	symbols    []string
	numSymbols int
	maxDepth   int
}

func loadTrie(filename string, symbols []string) (*symbolFrequencyTrie, error) {
	data, err := dataFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("load trie %s: %w", filename, err)
	}

	k := len(symbols)
	expectedNodes := 1 + k + k*k // depth 0, 1, 2
	expectedSize := expectedNodes * k * 2
	if len(data) != expectedSize {
		return nil, fmt.Errorf("trie %s: expected %d bytes, got %d", filename, expectedSize, len(data))
	}

	return &symbolFrequencyTrie{
		data:       data,
		symbols:    symbols,
		numSymbols: k,
		maxDepth:   2,
	}, nil
}

// getFrequencies returns the frequency table for a given node offset.
func (t *symbolFrequencyTrie) getFrequencies(nodeOffset int) []uint16 {
	freqs := make([]uint16, t.numSymbols)
	base := nodeOffset * t.numSymbols * 2
	for i := 0; i < t.numSymbols; i++ {
		freqs[i] = binary.BigEndian.Uint16(t.data[base+i*2 : base+i*2+2])
	}
	return freqs
}

// childOffset returns the child node offset for a given symbol index.
func (t *symbolFrequencyTrie) childOffset(parentOffset, symbolIndex int) int {
	return t.numSymbols*parentOffset + 1 + symbolIndex
}

// multiContextHuffmanCoder encodes symbols using context-dependent Huffman coding.
type multiContextHuffmanCoder struct {
	trie               *symbolFrequencyTrie
	symbolIndexByValue map[string]int
	cache              map[int]*huffmanCoder // nodeOffset → cached Huffman coder
}

func newMultiContextHuffmanCoder(trie *symbolFrequencyTrie) *multiContextHuffmanCoder {
	symbolIndexByValue := make(map[string]int, len(trie.symbols))
	for i, sym := range trie.symbols {
		symbolIndexByValue[sym] = i
	}
	return &multiContextHuffmanCoder{
		trie:               trie,
		symbolIndexByValue: symbolIndexByValue,
		cache:              make(map[int]*huffmanCoder),
	}
}

// coderForNode gets (or builds and caches) the Huffman coder for a trie node.
func (mc *multiContextHuffmanCoder) coderForNode(nodeOffset int) *huffmanCoder {
	if hc, ok := mc.cache[nodeOffset]; ok {
		return hc
	}
	hc := newHuffmanCoder(mc.trie.getFrequencies(nodeOffset), mc.trie.symbols)
	mc.cache[nodeOffset] = hc
	return hc
}

// symbolIndex returns the index of a symbol string, or -1 if not found.
func (mc *multiContextHuffmanCoder) symbolIndex(sym string) int {
	if idx, ok := mc.symbolIndexByValue[sym]; ok {
		return idx
	}
	return -1
}

// Encode encodes a sequence of symbol strings and returns the concatenated bit string.
func (mc *multiContextHuffmanCoder) Encode(syms []string) (string, error) {
	return mc.EncodeWithStartContext(syms, "")
}

// EncodeWithStartContext encodes symbols starting from a given context.
func (mc *multiContextHuffmanCoder) EncodeWithStartContext(syms []string, startCtx string) (string, error) {
	nodeOffset := 0
	depth := 0
	for _, c := range startCtx {
		s := string(c)
		idx := mc.symbolIndex(s)
		if idx < 0 {
			return "", fmt.Errorf("unknown start context symbol: %q", s)
		}
		nodeOffset, depth = mc.advanceContext(nodeOffset, depth, idx)
	}

	var bits strings.Builder
	for _, sym := range syms {
		idx := mc.symbolIndex(sym)
		if idx < 0 {
			return "", fmt.Errorf("unknown symbol: %q", sym)
		}
		hc := mc.coderForNode(nodeOffset)
		if !hc.canEncode(idx) {
			return "", fmt.Errorf("cannot encode symbol %q at context node %d", sym, nodeOffset)
		}
		bits.WriteString(hc.encode(idx))
		nodeOffset, depth = mc.advanceContext(nodeOffset, depth, idx)
	}
	return bits.String(), nil
}

func (mc *multiContextHuffmanCoder) advanceContext(nodeOffset, depth, symbolIndex int) (int, int) {
	if depth < mc.trie.maxDepth {
		return mc.trie.childOffset(nodeOffset, symbolIndex), depth + 1
	}
	prevSymIdx := (nodeOffset - 1) % mc.trie.numSymbols
	return mc.trie.childOffset(1+prevSymIdx, symbolIndex), depth
}

// Host coder symbols: - . 0-9 a-z |
var hostSymbols = []string{
	"-", ".", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l",
	"m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x",
	"y", "z", "|",
}

// Combined path+query symbols
var cpqSymbols = []string{
	"#", "%", "&", "+", ",", "-", ".", "/",
	"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
	":", ";", "=", "?",
	"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L",
	"M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X",
	"Y", "Z",
	"_",
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l",
	"m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x",
	"y", "z",
}

// Segmented path+query symbols
var spqSymbols = []string{
	"&", "+", "-", ".", "/",
	"0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
	"=", "?",
	"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L",
	"M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X",
	"Y", "Z",
	"_",
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l",
	"m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x",
	"y", "z", "|",
}

var (
	hostCoder *multiContextHuffmanCoder
	cpqCoder  *multiContextHuffmanCoder
	spqCoder  *multiContextHuffmanCoder
)

func initCoders() error {
	var err error

	ht, err := loadTrie("data/h.data", hostSymbols)
	if err != nil {
		return err
	}
	hostCoder = newMultiContextHuffmanCoder(ht)

	ct, err := loadTrie("data/cpq.data", cpqSymbols)
	if err != nil {
		return err
	}
	cpqCoder = newMultiContextHuffmanCoder(ct)

	st, err := loadTrie("data/spq.data", spqSymbols)
	if err != nil {
		return err
	}
	spqCoder = newMultiContextHuffmanCoder(st)

	return nil
}
