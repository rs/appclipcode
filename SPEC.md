# App Clip Code Specification

This document describes the format and generation of Apple App Clip Codes — circular visual codes that encode a URL for use with iOS App Clips.

## Overview

An App Clip Code is a circular SVG image containing 5 concentric rings of colored arc segments. The arcs encode a compressed URL using multi-context Huffman coding and Reed-Solomon error correction.

```
URL → Compress → Encode → Render

1. URL Compression:  HTTPS URL → 16 bytes (128 bits)
2. Codec Encoding:   16 bytes → 185+ bits (gap data + arc data)  
3. SVG Rendering:    bits → 5-ring circular fingerprint SVG
```

### Practical Limits

App Clip Codes do **not** encode arbitrary URLs. The shipping generator accepts only a constrained subset:

- Scheme: only `https://`
- Authority: host required; user info and ports are rejected
- Host alphabet: ASCII letters, digits, `.` and `-` only
- Host labels starting with `xn--` are rejected, so punycode A-label hosts/TLDs are not supported
- Path/query/fragment are encoded from their textual form; they are not percent-decoded first
- Raw non-ASCII characters are rejected
- Some raw ASCII characters must already be percent-encoded, while a few accepted raw characters such as `[` and `]` are canonicalized to `%5B` / `%5D`

The payload budget is also fixed:

- URL compression must fit in **128 bits total**
- This is a compressed-size limit, not a character-count limit
- As a result, there is no single maximum URL length in characters
- Short URLs with poorly compressible text can be rejected
- Longer URLs with common host/TLD/path patterns can still fit
- The shipping `AppClipCodeGenerator` rejects any URL whose raw compressed bitstream exceeds 128 bits, even though the lower-level `URLCompression.framework` can emit longer raw bitstreams

In practice, host-only URLs and URLs that align well with Apple's host/TLD tables and path/query coders have the most headroom. Query strings, fragments, uncommon hosts, and percent-heavy payloads consume that budget faster.

## 1. URL Compression

### 1.1 Generator-Compatible Input Rules

These rules describe the behavior of the shipping `AppClipCodeGenerator` CLI, not just the lower-level `URLCompression.framework`.

- The scheme must be `https` (case-insensitive on input, encoded as lowercased host data).
- A host is required.
- User info is rejected.
- Ports are rejected.
- Host bytes must be ASCII letters, digits, `.` or `-`.
- Any host label starting with `xn--` is rejected. Apple does not accept punycode A-label hosts or TLDs.
- The generator works on the textual URL components. It does **not** percent-decode the path/query/fragment before compression.
- Existing percent escapes are preserved verbatim, including hex case.
- The generator rejects raw non-ASCII characters. Non-ASCII content must already be percent-encoded.
- The generator rejects raw component characters:
  - Path/query: space, `"`, `%` (unless it begins a valid `%xx` escape), `<`, `>`, `\`, `^`, `` ` ``, `{`, `|`, `}`
  - Fragment: the same set, plus raw `#`
- Some raw characters that are accepted by Foundation but not present in the compression alphabets, notably `[` and `]`, are normalized to the same payload as `%5B` / `%5D`.

### 1.2 URL Component Extraction

Apple effectively works with these textual pieces:

1. Strip the `https://` prefix.
2. Split authority from the remainder at the first `/`, `?`, or `#`.
3. Lowercase the host.
4. If the host starts with `appclip.`, strip that prefix and set `subdomain_type = 1`; otherwise `subdomain_type = 0`.
5. Parse:
   - `path`: substring from the first `/` up to `?` or `#`, if present
   - `query`: substring after `?` up to `#`, if present
   - `fragment`: substring after `#`, if present

Important edge cases:

- `https://example.com` has no path/query/fragment payload.
- `https://example.com/` does have path payload: a single slash item.
- `https://example.com?` and `https://example.com#` are treated like host-only URLs.
- `https://example.com/?` and `https://example.com/#` are treated like `https://example.com/`.
- Host validation is not full DNS normalization: odd ASCII hosts such as `https://foo..com`, `https://-foo.com`, and `https://foo-.com` are still accepted.

### 1.3 Context Coders

Three pre-trained frequency tries drive the compression. All three use the same **standard Huffman** construction; there is no special SPQ/CPQ escape mode.

| Coder | Symbols | Data File | Usage |
|-------|---------|-----------|-------|
| Host | 39: `-.0-9a-z\|` | `h.data` | Domain + TLD encoding |
| SPQ | 71: `&+-./ 0-9=? A-Z_a-z\|` | `spq.data` | Segmented path/query |
| CPQ | 75: `#%&+,-./ 0-9:;=? A-Z_a-z` | `cpq.data` | Combined path/query/fragment |

Trie layout:

```
node_count = 1 + k + k²
node_size  = k * 2 bytes
child(node, symbol_index) = k*node + 1 + symbol_index
```

Each node stores `k` big-endian `uint16` frequencies. Context depth is 0, 1, or 2 previous symbols.

Huffman construction details:

- Use a min-heap.
- Primary sort key: lower frequency first.
- Tie-breaker: lexicographically smaller **leftmost leaf symbol** wins.
- First popped node becomes LEFT (`0`), second popped node becomes RIGHT (`1`).
- Accumulate frequencies in at least `uint32`.

This construction exactly reproduces Apple for Host, SPQ, and CPQ.

### 1.4 Host Encoding

The host is split at the last `.` into:

- `domain` = everything before the last dot
- `tld` = everything from the last dot onward

If any path/query/fragment payload follows, append `|` to the host-side string before encoding.

Apple tries three host formats and picks the shortest:

1. **Format 0**: 20 common TLDs encoded with a Huffman code, followed by Huffman-coded domain bits.
2. **Format 1**: extracted fixed-TLD table encoded as an **8-bit index**, followed by Huffman-coded domain bits.
3. **Format 2**: encode the entire host (including TLD and optional `|`) with the Host coder.

For formats 0 and 1, the TLD bits come **before** the domain bits.

Additional findings:

- The format-1 table is much larger than the early partial extraction suggested.
- Validated against Apple's generator on April 11, 2026, the current accepted root-zone set contains **113** format-1 TLDs.
- The table includes many ccTLDs and other non-Huffman TLDs such as `.ae`, `.ai`, `.at`, `.ch`, `.tv`, `.vn`, `.gov`, `.loan`, `.plus`, `.stream`, and `.tel`.
- A plain `strings` pass over the framework is not enough to recover this behavior; the complete table must be derived from Apple-oracle validation or equivalent binary analysis.
- `xn--...` punycode-style host labels are rejected during generator validation rather than being encoded with any host format.

### 1.5 Template Type Selection

After the begin marker and host data, path/query/fragment payload is encoded in one of two template families:

- `template_type = 1`: Apple's `PathWordBookAndAutoQueryTemplateFormat`
- `template_type = 0`: non-template path/query/fragment encoding

Apple first checks whether the URL matches the template family. If it does, Apple evaluates both the template bitstream and the non-template bitstream and picks the shorter of the two.

Tie-break rule between template and non-template: prefer **non-template** when the two bitstreams have equal length.

### 1.5.1 Template Family: `PathWordBookAndAutoQueryTemplateFormat`

The template family supports:

- optional single path word from Apple's path wordbook
- optional auto-numbered query parameters with fixed implicit keys `p`, `p1`, `p2`, ...
- no fragment

Only the `p` family gets this special template treatment. Other query key names
always use the non-template path/query encoders.

The framework does not explain why Apple chose `p`. A plausible reason is that
Apple's own App Clip invocation URLs use shapes such as
`https://appclip.apple.com/id?p=<bundleId>`, but that rationale is an
inference from observed behavior rather than something named in the binary.

The matcher rules are:

- the fragment must be empty
- after dropping empty `/` segments, the path may contain at most one non-empty component
- if a path component is present, it must exist in Apple's path wordbook
- if the raw path length is at least 2 bytes and ends in `/`, template mode is rejected
- the query is split on `&`
- empty query segments are ignored for template matching
- if the raw query ends with `&`, template mode is rejected
- every non-empty query component must contain `=`
- template query keys are fixed and must be exactly `p`, then `p1`, `p2`, `p3`, ... in order

This means all of the following can use `template_type = 1`:

- `https://host/shop`
- `https://host?id` is **not** valid template syntax
- `https://host?p=a`
- `https://host/?p=a`
- `https://host/id?p=a&p1=b`
- `https://host?&p=a&&p1=b`

But these cannot:

- `https://host/a/b?p=a` (too many path components)
- `https://host/id/?p=a` (trailing slash on non-root path)
- `https://host?p=a&` (raw trailing `&`)
- `https://host?q=a` (wrong key name)
- `https://host?p=a&p2=b` (key numbering gap)

### 1.5.2 Template Bit Layout

The template payload is assembled as:

```text
[optional path word: 0 + index8]
[optional query section: 1 + query components...]
```

So the three legal structural shapes are:

- path only: `0 + index8`
- query only: `1 + components...`
- path + query: `0 + index8 + 1 + components...`

If the path word is present, its index is emitted as an **8-bit** value. The older "9-bit template word" understanding was incomplete; the leading `0` is the structural path/query discriminator, not part of the word index itself.

For query components, the key is not encoded at all. It is implied by position:

- first value -> `p`
- second value -> `p1`
- third value -> `p2`
- etc.

Each template query component begins with a 2-bit value type:

- `00` = SPQ text value with start context `=`
- `01` = unsigned LEB128 decimal value
- `10` = fixed 6-bit alphabet value

Apple tries all encodable value forms and picks the shortest.

There is no template query component type `11`.

The fixed-6 alphabet is the same one used elsewhere:

```text
.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz|
```

The `|` terminator is appended only when another template query component follows.

### 1.5.3 Query-Only Canonicalization

For query-only template URLs, the compressed representation does not distinguish between textual inputs with an empty path and a root path:

- `https://host?p=a`
- `https://host/?p=a`

Apple's generator emits the same compressed payload for both. Apple's decoder canonicalizes such payloads to the `/?...` form.

### 1.6 Non-Template Selector

For `template_type = 0`, Apple tries both non-template encoders and picks the shorter result:

- selector bit `0` = **combined**
- selector bit `1` = **segmented**

Tie-break rule: prefer **combined** when the two bitstreams have equal length.

#### 1.6.1 Combined Mode

Build the textual string:

```
combined = path + ["?" + query] + ["#" + fragment]
```

Then apply one normalization:

- If `combined` starts with `/` and either:
  - it is exactly `/`, or
  - the second byte is not `#`
  then strip exactly one leading slash.

Examples:

- `/a` -> encode `a`
- `/a/b` -> encode `a/b`
- `?x=y` -> encode `?x=y`
- `#frag` -> encode `#frag`
- `/#frag` -> encode `/#frag` (the slash is **not** stripped because the second byte is `#`)

The resulting string is encoded directly with the CPQ coder.

#### 1.6.2 Segmented Mode Overview

Segmented mode is only available when the fragment is empty.

The path is converted into items:

- Strip one leading `/` if present.
- Split on `/`.
- Drop empty components.
- If there were no non-empty components, or the original path ended with `/`, append a literal slash item.

Examples:

- `/` -> `["/"]`
- `/a/b` -> `["a", "b"]`
- `/a/` -> `["a", "/"]`
- `//a///b/` -> `["a", "b", "/"]`

Emit each path item:

- Normal component: prefix `0`, then an encoded path component.
- Literal slash item: `10`
- Query section start: `11`

There is **no explicit separator** between adjacent normal path components. The decoder inserts `/` between them structurally.

#### 1.6.3 Segmented Path Component Types

Each normal path component starts with a 2-bit type:

- `00` = SPQ text
- `01` = unsigned LEB128 decimal
- `10` = fixed 6-bit alphabet
- `11` = path wordbook

Apple tries all encodable choices and picks the shortest.

SPQ text:

- Encode the component with the SPQ coder.
- Start context is empty.
- Append `|` before encoding when another path item or query section follows.

Unsigned LEB128 decimal:

- Only decimal digits are allowed.
- Parse as an arbitrary-size unsigned integer.
- Emit standard unsigned LEB128 bytes, low 7-bit groups first.
- Each output byte is written MSB-first into the bitstream.

Fixed 6-bit alphabet:

```
.0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz|
```

- Encode the 6-bit index of each byte, MSB-first.
- `|` is the terminator symbol, so append it only when another path item or query section follows.

Path wordbook:

- Use the same extracted word table as template type 1.
- In segmented mode only indices `0..255` are usable, so this is an 8-bit form.

#### 1.6.4 Segmented Query Components

The raw query is split on `&`.

Each query component must contain at least one `=`:

- bare keys are not encodable in segmented mode
- the first `=` splits key from value
- any additional `=` bytes remain inside the value and are encoded as text if segmented mode wins

After the query-start marker `11`, each parameter begins with a 2-bit value type:

- `00` = SPQ text value
- `01` = unsigned LEB128 decimal value
- `10` = fixed 6-bit value

Key encoding always uses the SPQ coder with start context `?`.

Bit layouts:

- Type `00`:
  - `00`
  - `SPQ("key|", start="?")`
  - `SPQ(value [+ "|"], start="=")`
- Type `01`:
  - `01`
  - `ULEB128(value)`
  - `SPQ(key [+ "|"], start="?")`
- Type `10`:
  - `10`
  - `SPQ("key|", start="?")`
  - `fixed6(value [+ "|"])`

The special case in numeric mode is the ordering: the numeric bytes come **before** the key bits.

### 1.7 Raw Bit Layout

The raw compressed bitstring is:

```
[1]                            begin marker
[template_type:1]
[subdomain_type:1]
[host_format: variable]
[host bits]
[path/query/fragment bits if present]
```

Host format codes:

- `0`   -> format 0
- `10`  -> format 1
- `11`  -> format 2

### 1.8 Generator Size Limit

The lower-level URL encoder can emit raw bitstrings longer than 128 bits, and `UCAppClipCodeURLEncodingResult.appClipCodeBytes` then expands beyond 16 bytes.

The shipping `AppClipCodeGenerator` CLI rejects such URLs with:

> Compressed URL too large: The compressed URL byte size exceeds supported payload size of the App Clip Code.

For URLs accepted by the CLI:

- raw bit length is at most 128
- the bitstring is left-padded with zeroes to 128 bits
- the final compressed payload is exactly 16 bytes

```
payload = left_pad_with_zeroes(raw_bits, 128)
```

## 2. Codec Encoding

### 2.1 Version Selection

Strip leading zero bytes from the 16-byte payload:
- Remaining ≤ 14 bytes → **Version 0**
- Remaining ≤ 16 bytes → **Version 1**

### 2.2 Format Parameters

| Parameter | Version 0 | Version 1 |
|-----------|-----------|-----------|
| Gaps data symbols | 9 | 11 |
| Gaps parity symbols | 4 | 2 |
| Arcs data symbols | 5 | 5 |
| Arcs parity symbols | 2 | 2 |
| Total data bytes | 14 | 16 |

### 2.3 Scrambling

Pad the trimmed payload back to `totalData` bytes with leading zeros. Then:

```
for i in 0..totalData-1:
    scrambled[i] = padded[totalData-1-i] XOR 0xA5
```

### 2.4 Data Split

```
gaps_data = scrambled[0 : gaps_data_count]
arcs_data = scrambled[totalData - arcs_data_count : totalData]
```

### 2.5 Reed-Solomon Encoding

Two Galois fields are used:

| Field | Primitive Poly | Size | Generator Base (fcr) | Usage |
|-------|---------------|------|---------------------|-------|
| GF(2⁴) | 0x13 (x⁴+x+1) | 16 | 0 | Metadata |
| GF(2⁸) | 0x11D (x⁸+x⁴+x³+x²+1) | 256 | 1 | Gaps & Arcs |

Generator polynomial: `g(x) = ∏(x + α^(fcr+i))` for i = 0..numParity-1.

Three RS encodes:

1. **Gaps**: RS(13,9) or RS(13,11) over GF(256) → 104 bits
2. **Metadata**: RS(4,2) over GF(16) → 16 bits
3. **Arcs**: RS(7,5) over GF(256) → 56 bits

### 2.6 Gap Bit Inversion

Count zero bits in the 104 gap bits. If `zero_count ≤ 51`, **invert all 104 bits** and set `inversion_flag = 1`. This ensures the gap ring has more zero bits (gaps) than one bits (arcs).

### 2.7 Metadata Encoding

```
metadata[0] = version >> 3
metadata[1] = inversion_flag | ((version & 7) << 1)
```

RS encode with 2 parity symbols → 4 GF(16) symbols → 16 bits (MSB-first, 4 bits per symbol).

### 2.8 Template Byte

Always `0x2A` (binary `00101010`). Stored as 8 bits in **LSB-first** order:
`[0, 1, 0, 1, 0, 1, 0, 0]`

### 2.9 Bit Assembly

128 pre-permutation bits assembled in order:

| Bits | Source | Count |
|------|--------|-------|
| 0–15 | Metadata RS bits | 16 |
| 16–119 | Gap RS bits (possibly inverted) | 104 |
| 120–127 | Template byte (LSB-first) | 8 |

### 2.10 LUT Permutation

The 128 pre-permutation bits are spatially permuted using a lookup table:

```
for i in 0..127:
    output[LUT[i]] = prePerm[i]
```

The LUT is a fixed permutation of integers 0–127.

### 2.11 Final Output

After the 128 LUT-permuted bits:

| Position | Content |
|----------|---------|
| 0–127 | LUT-permuted gap+meta+template bits |
| 128 | Separator (always 0) |
| 129–184 | Arc RS bits (56 bits) |
| 185+ | Extra gap bits: first `max(0, zeroCount128 - 56)` bits of the gap vector |

Total length = `129 + zeroCount128` where `zeroCount128` is the zero count in the 128-bit pre-permutation vector.

## 3. SVG Rendering

### 3.1 Ring Parameters

| Ring | Radius (px) | Rotation (°) | Positions | Half-Gap (°) |
|------|-------------|-------------|-----------|---------------|
| 1 | 177.2016 | −78 | 17 | 7.5 |
| 2 | 224.1012 | −85 | 23 | 5.6 |
| 3 | 271.0008 | −70 | 26 | 5.0 |
| 4 | 317.9004 | −63 | 29 | 4.2 |
| 5 | 364.8000 | −70 | 33 | 3.5 |

- **Center**: (400, 400)
- **Background radius**: 400
- **Stroke width**: 23.5
- **Radius step**: 46.8996

### 3.2 Bit-to-Ring Distribution

The first 128 output bits are distributed sequentially:

```
Ring 1: bits[0:17]
Ring 2: bits[17:40]
Ring 3: bits[40:66]
Ring 4: bits[66:95]
Ring 5: bits[95:128]
```

### 3.3 Arc Rendering

Each ring has N evenly-spaced positions, each spanning `360°/N` degrees. Each position has a visible arc:

- **Bit value 0** → Foreground color
- **Bit value 1** → Third (derived) color

Consecutive same-color positions **merge** into a single arc. The gap between arcs is `2 × halfGap` degrees.

For a run of K consecutive bits starting at position P:

```
startAngle = P × (360/N) + halfGap
endAngle   = (P+K) × (360/N) − halfGap
```

SVG arcs are emitted from the **end angle** (M point) to the **start angle** (A endpoint) with `sweep-flag=0`:

```xml
<path d="M {endX} {endY} A {radius} {radius} 0 {largeArc} 0 {startX} {startY}"
      data-color="{0|1}" style="fill:none;stroke:{color};stroke-linecap:round;
      stroke-miterlimit:10;stroke-width:23.5px"/>
```

### 3.4 SVG Structure

```xml
<svg data-design="Fingerprint" data-payload="{URL}"
     viewBox="0 0 800 800" xmlns="...">
  <title>App Clip Code</title>
  <circle cx="400" cy="400" r="400" id="Background" style="fill:{background}"/>
  <g id="Markers">
    <g name="ring-1" transform="rotate(-78 400 400)">
      <path ... data-color="0|1"/>
      ...
    </g>
    ...5 ring groups...
  </g>
  <g id="Logo" data-logo-type="Camera|phone" transform="...">...</g>
</svg>
```

### 3.5 Color Palette

9 base palettes with foreground, background, and third (derived) color:

| # | FG | BG | Third |
|---|--------|--------|---------|
| 0 | 000000 | FFFFFF | 888888 |
| 1 | 777777 | FFFFFF | AAAAAA |
| 2 | FF3B30 | FFFFFF | FF9999 |
| 3 | EE7733 | FFFFFF | EEBB88 |
| 4 | 33AA22 | FFFFFF | 99DD99 |
| 5 | 00A6A1 | FFFFFF | 88DDCC |
| 6 | 007AFF | FFFFFF | 77BBFF |
| 7 | 5856D6 | FFFFFF | BBBBEE |
| 8 | CC73E1 | FFFFFF | EEBBEE |

18 template indices: even = white on color, odd = color on white.

## 4. Data Files

### 4.1 Trie Data Files

The three trie data files (`h.data`, `spq.data`, `cpq.data`) are sourced from:
```
/Library/Developer/AppClipCodeGenerator/AppClipCodeGenerator.bundle/
  Contents/Frameworks/URLCompression.framework/Versions/A/Resources/
```

These files are installed when Xcode's App Clip Code Generator is present. They contain the pre-trained symbol frequency tables in big-endian uint16 format.

### 4.2 LUT Permutation Table

The 128-entry gap bit permutation LUT (`kGapsBitsOrderLUT`) is extracted from the binary at a fixed address. It maps pre-permutation bit positions to final ring positions.

### 4.3 SVG Assets

The camera and phone logo SVG paths are extracted from reference SVGs generated by Apple's tool.

## 5. Reading (Decoding) an App Clip Code

### 5.1 Visual Detection

1. Detect the circular fingerprint pattern in the image
2. Identify the 5 concentric rings by radius
3. Determine the ring rotation from the known angles

### 5.2 Bit Extraction

For each ring:
1. Divide the ring into N equal angular segments
2. Sample the color at each segment's center
3. Map colors to bit values: foreground → 0, third color → 1

### 5.3 Codec Decoding

1. **Reverse LUT**: `prePerm[i] = ringBits[LUT[i]]` for i = 0..127
2. **Extract components**:
   - Metadata: prePerm[0:16] → 4 GF(16) symbols
   - Gap data: prePerm[16:120] → 13 GF(256) symbols
   - Template: prePerm[120:128]
3. **RS decode metadata** → extract version and inversion flag
4. **Un-invert gaps** if inversion flag is set
5. **RS decode gaps** → recover 9 or 11 data symbols
6. **Unscramble**: XOR each byte with 0xA5, then reverse
7. Result: 14 or 16 payload bytes

### 5.4 URL Decompression

1. Find the begin marker (first "1" bit) in the payload
2. Read the bit stream:
   - Template type, subdomain type, host format
   - Decode host using multi-context Huffman
   - If "|" encountered: decode path using appropriate coder
3. Reconstruct: `https://` + [appclip.] + host + path

## 6. Implementation Notes

### 6.1 No Proprietary SPQ/CPQ Escape Model

The earlier theory that SPQ/CPQ used a custom escape tree was wrong.

What actually matches Apple:

- Host, SPQ, and CPQ all use the same standard Huffman builder described in §1.3.
- The trie data files are sufficient; no extra extracted per-context code tables are needed.
- There is no precomputed-URL bypass requirement in a correct implementation.

### 6.2 URLCompression vs AppClipCodeGenerator

There are two distinct layers:

1. `URLCompression.framework` can encode textual URLs into a raw bitstring and may return byte arrays longer than 16 bytes.
2. `AppClipCodeGenerator` adds an extra product constraint: reject any URL whose raw encoded bitstring exceeds 128 bits.

This distinction matters during reverse engineering:

- `UCAppClipCodeURLEncodingResult.rawEncodedBits` is still useful as an oracle for the compression algorithm.
- Generator-compatible libraries must also reproduce the CLI validation and 128-bit limit.

### 6.3 Mode Selection Is Global

Combined vs segmented selection is performed over the entire non-template payload, not per component.

Consequences:

- Adding one byte can switch the whole payload from segmented to combined, or vice versa.
- Query and fragment presence affect whether path components need terminators, so they also affect the winner.
- Equality is resolved in favor of combined mode.

### 6.4 Extracted Tables

A reimplementation needs three extracted data sources in addition to the logic above:

- Trie frequency files: `data/h.data`, `data/spq.data`, `data/cpq.data`
- Fixed-TLD index table for host format 1
- Known-path word index table used by template type 1 and segmented path type `11`

In this repository, the extracted fixed-TLD and known-word tables live in `huffman.go`. They are normative parts of the reverse-engineered format.

Notes on the fixed-TLD table:

- The checked-in table reflects the Apple behavior validated on April 11, 2026.
- It contains 113 currently accepted format-1 TLDs.
- This is a behavioral extraction, not just a symbol dump from the framework binary.
- Re-validating against the current IANA root-zone list is a useful regression check because Apple rejects some listed TLDs outright, especially punycode `xn--...` entries.

Notes on the known-path word table:

- The checked-in table reflects the Apple behavior validated on April 11, 2026.
- It contains **156** entries at indices `0..155`.
- Early partial extraction missed four valid entries: `bag`, `biz`, `cat`, and `use`.
- Completeness for this framework build was verified by exhaustive decoder probing of all `0..255` possible 8-bit template path-word indices against Apple-generated template payload structure.
- That sweep produced valid path words only for indices `0..155`; no additional valid indices were observed in `156..255`.
- So for the current shipped framework build, the path word dictionary can be treated as fully extracted.
- This does **not** guarantee future Apple builds will keep the same dictionary; if Apple updates `URLCompression.framework`, the table should be re-validated.
