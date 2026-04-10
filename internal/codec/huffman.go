package codec

import (
	"container/heap"
	"sort"
)

// huffmanNode is a node in a Huffman tree.
type huffmanNode struct {
	freq        uint32 // uint32 to avoid overflow when summing uint16 frequencies
	symbolIndex int    // -1 for internal nodes
	symbol      string
	left, right *huffmanNode
}

// leftmostLeafSymbol walks a subtree to find its leftmost leaf
// (depth-first, preferring left children), and returns that leaf's symbol.
func leftmostLeafSymbol(n *huffmanNode) string {
	for n != nil {
		if n.left != nil {
			n = n.left
		} else if n.right != nil {
			n = n.right
		} else {
			return n.symbol
		}
	}
	return ""
}

// huffmanCoder encodes symbols using a Huffman code built from frequencies.
type huffmanCoder struct {
	codes     []string // symbol index -> bit string ("010", "1101", etc.)
	numSymbol int
}

// newHuffmanCoder builds a Huffman coder from a frequency table.
// symbols provides the string name for each symbol index (used for tie-breaking).
func newHuffmanCoder(freqs []uint16, symbols []string) *huffmanCoder {
	n := len(freqs)
	hc := &huffmanCoder{codes: make([]string, n), numSymbol: n}

	// Collect non-zero frequency symbols
	var leaves []*huffmanNode
	for i, f := range freqs {
		if f > 0 {
			sym := ""
			if i < len(symbols) {
				sym = symbols[i]
			}
			leaves = append(leaves, &huffmanNode{freq: uint32(f), symbolIndex: i, symbol: sym})
		}
	}

	if len(leaves) == 0 {
		return hc
	}
	if len(leaves) == 1 {
		hc.codes[leaves[0].symbolIndex] = "0"
		return hc
	}

	// Build Huffman tree using a min-heap.
	// Tie-breaking (matching Apple's UCHuffmanCoder):
	//   When frequencies are equal, compare the leftmost-leaf symbol of each
	//   subtree lexicographically. The node with the SMALLER leftmost-leaf
	//   symbol is popped first (has higher priority).
	pq := &huffmanPQ{}
	heap.Init(pq)
	for _, leaf := range leaves {
		heap.Push(pq, leaf)
	}

	for pq.Len() > 1 {
		// First popped (smaller) becomes LEFT child (bit '0')
		left := heap.Pop(pq).(*huffmanNode)
		// Second popped becomes RIGHT child (bit '1')
		right := heap.Pop(pq).(*huffmanNode)
		combined := &huffmanNode{
			freq:        left.freq + right.freq,
			symbolIndex: -1,
			symbol:      "",
			left:        left,
			right:       right,
		}
		heap.Push(pq, combined)
	}

	root := heap.Pop(pq).(*huffmanNode)
	hc.buildCodes(root, "")
	return hc
}

func (hc *huffmanCoder) buildCodes(node *huffmanNode, prefix string) {
	if node == nil {
		return
	}
	if node.left == nil && node.right == nil {
		// Leaf node
		if prefix == "" {
			prefix = "0"
		}
		hc.codes[node.symbolIndex] = prefix
		return
	}
	// Left = '0', Right = '1' (matching Apple)
	hc.buildCodes(node.left, prefix+"0")
	hc.buildCodes(node.right, prefix+"1")
}

// encode returns the bit string for the symbol at the given index.
func (hc *huffmanCoder) encode(symbolIndex int) string {
	if symbolIndex < 0 || symbolIndex >= len(hc.codes) {
		return ""
	}
	return hc.codes[symbolIndex]
}

// canEncode returns true if the symbol has a valid code (non-zero frequency).
func (hc *huffmanCoder) canEncode(symbolIndex int) bool {
	return symbolIndex >= 0 && symbolIndex < len(hc.codes) && hc.codes[symbolIndex] != ""
}

// maxDepth returns the maximum depth of a Huffman tree.
func maxDepth(n *huffmanNode) int {
	if n == nil {
		return 0
	}
	if n.left == nil && n.right == nil {
		return 0
	}
	ld := maxDepth(n.left)
	rd := maxDepth(n.right)
	if ld > rd {
		return ld + 1
	}
	return rd + 1
}

// limitTreeDepth rebuilds a Huffman tree with a maximum depth constraint.
// Uses the DEFLATE-style redistribution: clamp overflowing codes to maxLen,
// then split shorter codes to compensate for the Kraft inequality violation.
func limitTreeDepth(root *huffmanNode, freqs []uint16, symbols []string, maxLen int) *huffmanNode {
	// Step 1: Get current code lengths
	type symLen struct {
		index  int
		length int
		freq   uint16
	}
	var items []symLen
	var collectLengths func(n *huffmanNode, depth int)
	collectLengths = func(n *huffmanNode, depth int) {
		if n == nil {
			return
		}
		if n.left == nil && n.right == nil {
			items = append(items, symLen{n.symbolIndex, depth, freqs[n.symbolIndex]})
			return
		}
		collectLengths(n.left, depth+1)
		collectLengths(n.right, depth+1)
	}
	collectLengths(root, 0)

	// Step 2: Count codes at each bit length, clamping overflow
	blCount := make([]int, maxLen+1)
	overflow := 0
	for i := range items {
		if items[i].length > maxLen {
			blCount[maxLen]++
			items[i].length = maxLen
			overflow++
		} else {
			blCount[items[i].length]++
		}
	}

	// Step 3: Redistribute using DEFLATE algorithm
	for overflow > 0 {
		bits := maxLen - 1
		for bits >= 1 && blCount[bits] == 0 {
			bits--
		}
		if bits < 1 {
			break
		}
		blCount[bits]--
		blCount[bits+1] += 2
		blCount[maxLen]--
		overflow--
	}

	// Step 4: Assign new lengths — longest codes to lowest frequency symbols
	sort.Slice(items, func(i, j int) bool {
		return items[i].freq < items[j].freq
	})

	idx := 0
	for bits := maxLen; bits >= 1; bits-- {
		for c := 0; c < blCount[bits]; c++ {
			if idx < len(items) {
				items[idx].length = bits
				idx++
			}
		}
	}

	// Step 5: Build new tree from the assigned lengths using canonical-like construction.
	// Sort by (length ASC, frequency DESC) for consistent tree structure.
	sort.Slice(items, func(i, j int) bool {
		if items[i].length != items[j].length {
			return items[i].length < items[j].length
		}
		return items[i].freq > items[j].freq
	})

	// Build a tree by inserting leaves at the correct depths
	newRoot := &huffmanNode{symbolIndex: -1}
	for _, item := range items {
		insertAtDepth(newRoot, item.length, &huffmanNode{
			freq:        uint32(item.freq),
			symbolIndex: item.index,
			symbol:      symbols[item.index],
		})
	}

	return newRoot
}

// insertAtDepth inserts a leaf node at the specified depth in a binary tree.
func insertAtDepth(root *huffmanNode, depth int, leaf *huffmanNode) {
	if depth == 0 {
		return // shouldn't happen
	}
	node := root
	for d := 1; d < depth; d++ {
		if node.left == nil {
			node.left = &huffmanNode{symbolIndex: -1}
		}
		// Try left subtree first, then right
		if canInsertAt(node.left, depth-d) {
			node = node.left
		} else {
			if node.right == nil {
				node.right = &huffmanNode{symbolIndex: -1}
			}
			node = node.right
		}
	}
	// At the correct depth, insert as left or right child
	if node.left == nil {
		node.left = leaf
	} else if node.right == nil {
		node.right = leaf
	}
}

// canInsertAt checks if there's room to insert a leaf at the given remaining depth.
func canInsertAt(node *huffmanNode, remainingDepth int) bool {
	if node == nil {
		return remainingDepth >= 0
	}
	if node.left == nil && node.right == nil && node.symbolIndex >= 0 {
		return false // already a leaf
	}
	if remainingDepth <= 0 {
		return node.left == nil || node.right == nil
	}
	if node.left == nil {
		return true
	}
	if canInsertAt(node.left, remainingDepth-1) {
		return true
	}
	if node.right == nil {
		return true
	}
	return canInsertAt(node.right, remainingDepth-1)
}

// huffmanPQ implements heap.Interface for Huffman tree construction.
// This is a min-heap: the node with the lowest priority value is at the top.
type huffmanPQ []*huffmanNode

func (pq huffmanPQ) Len() int { return len(pq) }

func (pq huffmanPQ) Less(i, j int) bool {
	a, b := pq[i], pq[j]
	if a.freq != b.freq {
		return a.freq < b.freq
	}
	// Equal frequency: compare leftmost-leaf symbols lexicographically.
	// Smaller symbol string = higher priority (popped first).
	symA := leftmostLeafSymbol(a)
	symB := leftmostLeafSymbol(b)
	return symA < symB
}

func (pq huffmanPQ) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *huffmanPQ) Push(x interface{}) { *pq = append(*pq, x.(*huffmanNode)) }
func (pq *huffmanPQ) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

// huffmanTLDs lists the 20 TLDs with hardcoded Huffman frequencies.
// Sorted alphabetically at init time for deterministic symbol ordering.
var huffmanTLDs = []struct {
	TLD  string
	Freq uint16
}{
	{".com", 0xfffe}, {".org", 0x26f6}, {".net", 0x1766},
	{".de", 0x1163}, {".ru", 0x0fed}, {".cn", 0x0cb7},
	{".uk", 0x0c86}, {".jp", 0x08e2}, {".it", 0x062c},
	{".fr", 0x059d}, {".nl", 0x0598}, {".au", 0x0513},
	{".br", 0x04ad}, {".ca", 0x0482}, {".info", 0x0449},
	{".in", 0x03d5}, {".edu", 0x03c1}, {".us", 0x0361},
	{".pl", 0x0352}, {".ga", 0x0346},
}

// tldList is the sorted TLD strings for index lookup.
var tldList []string

func init() {
	sort.Slice(huffmanTLDs, func(i, j int) bool { return huffmanTLDs[i].TLD < huffmanTLDs[j].TLD })
	tldList = make([]string, len(huffmanTLDs))
	for i, t := range huffmanTLDs {
		tldList[i] = t.TLD
	}
}

// tldHuffmanCoder builds a Huffman coder for the 20 Huffman-encoded TLDs.
func tldHuffmanCoder() *huffmanCoder {
	freqs := make([]uint16, len(huffmanTLDs))
	syms := make([]string, len(huffmanTLDs))
	for i, t := range huffmanTLDs {
		freqs[i] = t.Freq
		syms[i] = t.TLD
	}
	return newHuffmanCoder(freqs, syms)
}

// fixedTLDIndex maps TLDs to their 8-bit fixed-width encoding index (format 1).
// Extracted from Apple's AppClipCodeGenerator binary.
var fixedTLDIndex = map[string]int{
	".ae": 54, ".ai": 57, ".am": 68, ".app": 58, ".ar": 33, ".at": 7,
	".be": 6, ".bid": 93, ".bike": 111, ".biz": 17, ".business": 110,
	".by": 48, ".cc": 27, ".center": 98, ".cf": 13, ".ch": 2,
	".cl": 36, ".cloud": 66, ".club": 29, ".cm": 94, ".company": 86,
	".cz": 9, ".digital": 97, ".dk": 30, ".do": 74, ".es": 1,
	".estate": 113, ".eu": 3, ".fi": 38, ".fun": 64, ".gl": 107,
	".global": 85, ".gov": 10, ".gr": 12, ".gt": 90, ".help": 106,
	".hk": 40, ".host": 87, ".hu": 24, ".id": 21, ".ie": 31,
	".il": 42, ".int": 83, ".io": 4, ".is": 59, ".jobs": 70,
	".kr": 14, ".kz": 47, ".life": 71, ".live": 53, ".loan": 112,
	".ltd": 100, ".lu": 67, ".ly": 73, ".md": 76, ".me": 16,
	".media": 79, ".mo": 95, ".mobi": 56, ".museum": 108, ".mx": 22,
	".my": 39, ".name": 61, ".network": 65, ".news": 60, ".no": 34,
	".nu": 45, ".nz": 25, ".online": 35, ".ph": 52, ".pk": 49,
	".plus": 99, ".pm": 109, ".pt": 43, ".pub": 105, ".py": 91,
	".qa": 84, ".ro": 26, ".se": 19, ".services": 101, ".sg": 46,
	".shop": 77, ".site": 18, ".sk": 41, ".so": 102, ".space": 55,
	".store": 78, ".stream": 89, ".su": 50, ".support": 104,
	".tech": 62, ".tel": 96, ".th": 44, ".tk": 37, ".tn": 75,
	".to": 51, ".top": 28, ".tr": 20, ".travel": 81, ".tt": 103,
	".tv": 11, ".tw": 15, ".ua": 8, ".video": 92, ".vip": 63,
	".vn": 5, ".wang": 23, ".website": 69, ".wiki": 88, ".win": 72,
	".work": 82, ".world": 80, ".za": 32,
}

// fixedTLDByIndex maps 8-bit indices back to TLD strings (for decoding).
var fixedTLDByIndex map[int]string

func init() {
	fixedTLDByIndex = make(map[int]string, len(fixedTLDIndex))
	for tld, idx := range fixedTLDIndex {
		fixedTLDByIndex[idx] = tld
	}
}

// knownWordIndex maps Apple path-word dictionary entries to their 8-bit index.
// In auto-query template mode the path word is encoded as "0" + this 8-bit index.
// Extracted from Apple's URLCompression framework and verified against Apple decoding.
var knownWordIndex = map[string]int{
	"about":         0,
	"access":        1,
	"account":       2,
	"add":           3,
	"app":           4,
	"archives":      5,
	"article":       6,
	"attraction":    7,
	"author":        8,
	"bag":           9,
	"biz":           10,
	"book":          11,
	"brand":         12,
	"brands":        13,
	"browse":        14,
	"buy":           15,
	"cancel":        16,
	"cart":          17,
	"cat":           18,
	"catalog":       19,
	"category":      20,
	"categories":    21,
	"channel":       22,
	"charts":        23,
	"checkin":       24,
	"checkout":      25,
	"collection":    26,
	"collections":   27,
	"company":       28,
	"compare":       29,
	"connect":       30,
	"contact":       31,
	"content":       32,
	"contents":      33,
	"cost":          34,
	"coupons":       35,
	"create":        36,
	"data":          37,
	"demo":          38,
	"destinations":  39,
	"detail":        40,
	"discover":      41,
	"download":      42,
	"entry":         43,
	"event":         44,
	"events":        45,
	"explore":       46,
	"faq":           47,
	"fetch":         48,
	"finance":       49,
	"find":          50,
	"food":          51,
	"fund":          52,
	"game":          53,
	"gift":          54,
	"goods":         55,
	"guide":         56,
	"health":        57,
	"help":          58,
	"home":          59,
	"hotel":         60,
	"hotels":        61,
	"id":            62,
	"index":         63,
	"info":          64,
	"item":          65,
	"item_id":       66,
	"join":          67,
	"lifestyle":     68,
	"list":          69,
	"listen":        70,
	"live":          71,
	"local":         72,
	"location":      73,
	"locations":     74,
	"locator":       75,
	"login":         76,
	"manage":        77,
	"menu":          78,
	"more":          79,
	"music":         80,
	"name":          81,
	"news":          82,
	"note":          83,
	"open":          84,
	"order":         85,
	"overview":      86,
	"park":          87,
	"part":          88,
	"pay":           89,
	"payment":       90,
	"payments":      91,
	"play":          92,
	"post":          93,
	"posts":         94,
	"preview":       95,
	"product":       96,
	"product_id":    97,
	"products":      98,
	"profile":       99,
	"promotion":     100,
	"purchase":      101,
	"rate":          102,
	"recipe":        103,
	"recipes":       104,
	"reservation":   105,
	"reservations":  106,
	"reserve":       107,
	"retail":        108,
	"review":        109,
	"rewards":       110,
	"sale":          111,
	"scan":          112,
	"schedule":      113,
	"search":        114,
	"sell":          115,
	"send":          116,
	"service":       117,
	"share":         118,
	"shop":          119,
	"show":          120,
	"showtime":      121,
	"site":          122,
	"song":          123,
	"special":       124,
	"stations":      125,
	"status":        126,
	"store":         127,
	"store-locator": 128,
	"stores":        129,
	"stories":       130,
	"story":         131,
	"tag":           132,
	"tags":          133,
	"terms":         134,
	"tickets":       135,
	"tips":          136,
	"title":         137,
	"today":         138,
	"top":           139,
	"topic":         140,
	"tours":         141,
	"track":         142,
	"transaction":   143,
	"travel":        144,
	"try":           145,
	"update":        146,
	"upload":        147,
	"use":           148,
	"user":          149,
	"vehicles":      150,
	"video":         151,
	"view":          152,
	"visit":         153,
	"watch":         154,
	"wiki":          155,
}

// knownWordByIndex maps path-word indices back to strings for decoding.
var knownWordByIndex map[int]string

func init() {
	knownWordByIndex = make(map[int]string, len(knownWordIndex))
	for word, idx := range knownWordIndex {
		knownWordByIndex[idx] = word
	}
}
