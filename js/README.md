# App Clip Code Encoder

TypeScript library for generating Apple App Clip Code SVGs.

This package reproduces the encoder pipeline for Apple App Clip Codes:

1. validate and compress an accepted `https://` URL into a 128-bit payload
2. encode that payload with the App Clip Code Reed-Solomon envelope
3. render the circular SVG fingerprint

The implementation is intentionally encoder-only. It does not include:

- SVG reading
- payload decoding
- raster image scanning

## Status

The current package covers the generator path only and is tested against:

- bundled URL compression vectors
- bundled Apple reference SVG fixtures

Runtime note:

- the trie codebooks are compiled into the package in a compact compressed form,
  so the library does not fetch or load separate data files at runtime
- the library runtime is compatible with both browsers and Node.js

## Requirements

- Node.js `>=18`
- ESM runtime

## Install

Install from npm:

```bash
npm install appclipcode
```

Use the CLI without installing globally:

```bash
npx appclipcode https://example.com > code.svg
```

Build the package:

```bash
npm run build
```

Run the test suite:

```bash
npm test
```

## Quick Start

Generate an App Clip Code SVG with a built-in color template:

```ts
import { generateWithTemplate } from "appclipcode";

const svg = generateWithTemplate("https://example.com", 0);
```

Generate with custom foreground and background colors:

```ts
import { generate } from "appclipcode";

const svg = generate("https://example.com", "FFFFFF", "000000");
```

Generate an inline SVG data URL:

```ts
import { generateWithTemplate } from "appclipcode";

const svg = generateWithTemplate("https://example.com", 0);
const dataUrl = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
```

Generate an NFC-style code instead of the camera-style code:

```ts
import { CodeTypeNFC, generateWithTemplate } from "appclipcode";

const svg = generateWithTemplate("https://example.com", 4, {
  type: CodeTypeNFC,
});
```

Use the lower-level encoder steps directly:

```ts
import { compressURL, encodePayload, renderSvg, templateByIndex } from "appclipcode";

const url = "https://example.com";
const compressed = compressURL(url);
const bits = encodePayload(compressed);
const palette = templateByIndex(0);
const svg = renderSvg(bits, palette, url);
```

## CLI

The package ships a CLI named `appclipcode`.

Usage:

```bash
appclipcode <url> [--index N] [--type cam|nfc] [-o FILE]
appclipcode <url> --fg HEX --bg HEX [--type cam|nfc] [-o FILE]
appclipcode templates
```

Examples:

```bash
npx appclipcode https://example.com > code.svg
npx appclipcode https://example.com --index 4 -o code.svg
npx appclipcode https://example.com --fg FFFFFF --bg 000000 --type nfc -o code.svg
npx appclipcode templates
```

Behavior:

- the URL is positional, not passed as `--url`
- if neither `--index` nor `--fg/--bg` is provided, template `0` is used
- `--index` cannot be combined with `--fg/--bg`
- output defaults to stdout unless `-o` or `--output` is provided
- `templates` lists the built-in template palette values

## API

All public exports come from `src/index.ts`.

### Constants

#### `CodeTypeCamera`

String constant for the default camera-style logo. Value: `"cam"`.

#### `CodeTypeNFC`

String constant for the NFC-style phone logo. Value: `"nfc"`.

### Types

#### `Options`

```ts
interface Options {
  type?: "cam" | "nfc";
}
```

Used by the high-level `generate` and `generateWithTemplate` helpers.

#### `Palette`

```ts
interface Palette {
  foreground: Color;
  background: Color;
  third: Color;
}
```

Represents the three colors used in the SVG.

#### `Template`

```ts
interface Template extends Palette {
  index: number;
}
```

Represents one of the built-in color templates.

### Classes

#### `Color`

```ts
new Color(r, g, b, a?)
```

Represents an RGBA color. Alpha defaults to `0xff` when omitted. The `hex()`
method returns a lowercase CSS hex string such as `#ff3b30` or `#ff3b3080`.

#### `GaloisField`

Low-level finite field implementation used by the Reed-Solomon encoder.

#### `RSEncoder`

Low-level Reed-Solomon encoder class used by payload encoding.

### High-Level Functions

#### `generate(rawURL, foreground, background, options?)`

Validates and compresses the URL, encodes the payload, and returns the final SVG
string using custom colors.

Parameters:

- `rawURL: string`
- `foreground: string`
- `background: string`
- `options?: Options`

Returns:

- `string` containing the SVG markup

Notes:

- `foreground` and `background` must be 6- or 8-digit hex strings (`RRGGBB` or `RRGGBBAA`)
- the third palette color is inferred from built-in template matches or computed
  as the midpoint between foreground and background

#### `generateWithTemplate(rawURL, templateIndex, options?)`

Same as `generate`, but uses one of the built-in templates.

Parameters:

- `rawURL: string`
- `templateIndex: number`
- `options?: Options`

Returns:

- `string` containing the SVG markup

### Lower-Level Functions

#### `compressURL(rawURL)`

Compresses a supported App Clip URL into a 16-byte payload.

Returns:

- `Uint8Array`

Throws when the URL is not accepted by the App Clip Code format or when the
compressed result exceeds 128 bits.

#### `encodePayload(payload)`

Applies the App Clip Code payload envelope to compressed bytes.

Parameters:

- `Uint8Array` or `number[]`

Returns:

- `boolean[]` bit vector used by the SVG renderer

#### `renderSvg(bits, palette, url, codeType?)`

Renders the circular App Clip Code SVG from already-encoded bits.

Parameters:

- `bits: boolean[]`
- `palette: Palette`
- `url: string`
- `codeType?: "cam" | "nfc"`

Returns:

- `string` containing the SVG markup

#### `parseHexColor(value)`

Parses a 6- or 8-digit hex color string and returns a `Color`. When alpha is
omitted, it defaults to opaque.

#### `templates()`

Returns all 18 built-in templates as `Template[]`.

#### `templateByIndex(index)`

Returns a single template palette by index.

Valid range:

- `0` through `17`

## URL Support and Constraints

This encoder is intentionally strict and follows the App Clip Code format's
input expectations.

Supported input shape:

- scheme must be `https://`
- host is required
- no user info
- no port
- ASCII host only
- no punycode `xn--...` labels

Important behavior:

- path, query, and fragment are encoded from their textual form
- invalid raw characters are rejected
- some characters are canonicalized through percent-encoding
- the compressed payload must fit in 128 bits

As a result, App Clip Codes do not support arbitrary URLs.

## Templates

The package exposes 18 predefined templates.

- indices `0, 2, 4, ... 16`: white foreground on colored background
- indices `1, 3, 5, ... 17`: colored foreground on white background

Use `templates()` to inspect the full list programmatically, or
`templateByIndex(index)` to select a specific one.

## Output

The generator returns SVG markup as a JavaScript string.

It includes:

- `data-design="Fingerprint"`
- `data-payload="<url>"`
- the rendered ring arcs
- the center logo for camera or NFC mode

## Error Handling

Most public APIs throw `Error` when input is invalid.

Common failure cases:

- invalid color string
- unsupported URL syntax
- unsupported host characters
- invalid percent escapes
- compressed payload exceeding 128 bits
- invalid template index

## Example: Write SVG to Disk

```ts
import fs from "node:fs/promises";
import { generateWithTemplate } from "appclipcode";

const svg = generateWithTemplate("https://example.com", 0);
await fs.writeFile("code.svg", svg, "utf8");
```

## Example: Inspect the Raw Payload

```ts
import { compressURL } from "appclipcode";

const payload = compressURL("https://example.com");
console.log(Buffer.from(payload).toString("hex"));
```

## Non-Goals

This package does not currently try to provide:

- decoding support
- camera or raster scanning support

If those are needed, they should be added as separate scope rather than implied
by the encoder package.
