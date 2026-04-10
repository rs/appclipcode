#!/bin/bash
# Generate comprehensive test vectors using Apple's AppClipCodeGenerator.
# For each test URL, generate an SVG via Apple's tool, then store the results
# next to the checked-in fixture data in testdata/.

set -euo pipefail

if ! command -v AppClipCodeGenerator >/dev/null 2>&1; then
    echo "AppClipCodeGenerator not found in PATH" >&2
    exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTDIR="$SCRIPT_DIR/apple_comprehensive"
VECTOR_JSON="$SCRIPT_DIR/comprehensive_vectors.json"

mkdir -p "$OUTDIR"

# Deterministic "random" paths using a fixed seed approach
URLS=(
    # Host-only (various TLDs)
    "https://example.com"
    "https://a.co"
    "https://www.apple.com"
    "https://appclip.example.com"
    "https://test.org"
    "https://shop.net"
    "https://my.app"
    "https://x.de"
    "https://example.info"
    "https://test.edu"

    # Known words (template type 1)
    "https://example.com/shop"
    "https://example.com/about"
    "https://example.com/download"
    "https://example.com/help"
    "https://example.com/contact"
    "https://example.com/search"
    "https://example.com/login"
    "https://example.com/cart"
    "https://example.com/video"
    "https://a.co/watch"
    "https://test.org/play"
    "https://shop.net/buy"
    "https://www.apple.com/store"

    # Short lowercase (SPQ)
    "https://example.com/a"
    "https://example.com/z"
    "https://example.com/ab"
    "https://example.com/zz"
    "https://example.com/test"
    "https://example.com/path"
    "https://example.com/hello"
    "https://a.co/go"
    "https://a.co/xyz"
    "https://a.co/data"
    "https://test.org/info"
    "https://test.org/docs"
    "https://shop.net/sale"
    "https://my.app/run"
    "https://www.apple.com/mac"

    # Medium lowercase
    "https://example.com/product"
    "https://example.com/settings"
    "https://a.co/redirect"
    "https://test.org/articles"
    "https://shop.net/checkout"
    "https://my.app/explore"
    "https://www.apple.com/support"
    "https://qr.netflix.com/watch"

    # Multi-segment lowercase
    "https://example.com/a/b"
    "https://example.com/foo/bar"
    "https://example.com/path/to/page"
    "https://a.co/go/now"
    "https://a.co/x/y/z"
    "https://test.org/api/data"
    "https://test.org/v2/docs/help"
    "https://shop.net/item/detail"
    "https://my.app/user/profile"
    "https://www.apple.com/shop/buy"
    "https://qr.netflix.com/title/watch"

    # Mixed case (CPQ)
    "https://example.com/A"
    "https://example.com/AB"
    "https://example.com/Hello"
    "https://example.com/MyPage"
    "https://a.co/Go"
    "https://a.co/X1"
    "https://test.org/V2"
    "https://shop.net/ID"
    "https://my.app/OK"
    "https://www.apple.com/MacBook"
    "https://qr.netflix.com/C/AAAA"
    "https://qr.netflix.com/Show"

    # Digits
    "https://example.com/123"
    "https://example.com/v2"
    "https://a.co/42"
    "https://test.org/2024"
    "https://shop.net/item123"

    # Dots and hyphens
    "https://example.com/v1.0"
    "https://example.com/api-v2"
    "https://example.com/my-page"
    "https://a.co/file.html"
    "https://test.org/data-set"
    "https://shop.net/item-123"
    "https://www.apple.com/mac-pro"

    # Appclip subdomain
    "https://appclip.example.com"
    "https://appclip.example.com/open"
    "https://appclip.test.org/scan"
    "https://appclip.shop.net/buy"

    # Single uppercase A-Z
    "https://x.com/A"
    "https://x.com/B"
    "https://x.com/C"
    "https://x.com/D"
    "https://x.com/F"
    "https://x.com/G"
    "https://x.com/H"
    "https://x.com/I"
    "https://x.com/J"
    "https://x.com/K"
    "https://x.com/L"
    "https://x.com/M"
    "https://x.com/N"
    "https://x.com/O"
    "https://x.com/P"
    "https://x.com/Q"
    "https://x.com/R"
    "https://x.com/S"
    "https://x.com/T"
    "https://x.com/U"
    "https://x.com/V"
    "https://x.com/W"
    "https://x.com/X"
    "https://x.com/Y"
    "https://x.com/Z"

    # Real-world patterns
    "https://example.com/product/12345"
    "https://shop.net/item/abc"
    "https://my.app/user/settings"
    "https://test.org/api/v2/data"
    "https://a.co/B/1234"
    "https://example.com/p/sale"
    "https://qr.netflix.com/title/80100172"
    "https://www.apple.com/shop/buy-iphone"

    # Edge cases
    "https://a.co/a"
    "https://a.co/z"
    "https://a.co/0"
    "https://a.co/9"
    "https://x.com/test"
    "https://example.com/index"
    "https://example.com/event"
    "https://example.com/guide"
)

echo "Generating ${#URLS[@]} test SVGs..."
i=0
for url in "${URLS[@]}"; do
    file=$(printf "%s/test_%03d.svg" "$OUTDIR" "$i")
    if [ ! -f "$file" ]; then
        if ! AppClipCodeGenerator generate --url "$url" --index 0 --output "$file" 2>/dev/null; then
            echo "  SKIP: $url (encoding failed)"
            i=$((i + 1))
            continue
        fi
    fi
    i=$((i + 1))
done

# Build JSON vector file: [{url, file}, ...]
echo "[" > "$VECTOR_JSON"
first=1
i=0
for url in "${URLS[@]}"; do
    file=$(printf "test_%03d.svg" "$i")
    svgpath=$(printf "%s/test_%03d.svg" "$OUTDIR" "$i")
    if [ -f "$svgpath" ]; then
        if [ "$first" -eq 0 ]; then
            echo "," >> "$VECTOR_JSON"
        fi
        first=0
        printf '  {"url": "%s", "file": "%s"}' "$url" "$file" >> "$VECTOR_JSON"
    fi
    i=$((i + 1))
done
echo "" >> "$VECTOR_JSON"
echo "]" >> "$VECTOR_JSON"

echo "Done. $(grep -c '"url"' "$VECTOR_JSON") vectors in $VECTOR_JSON"
