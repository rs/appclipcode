import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  CodeTypeNFC,
  compressURL,
  encodePayload,
  generate,
  generateWithTemplate,
  templates,
} from "../dist/index.js";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "../..");

test("template list exposes 18 presets", () => {
  assert.equal(templates().length, 18);
});

test("compressURL matches the random oracle vectors", () => {
  const vectors = JSON.parse(
    fs.readFileSync(path.join(repoRoot, "testdata", "random_vectors.json"), "utf8"),
  );

  for (const vector of vectors) {
    const actual = Buffer.from(compressURL(vector.url)).toString("hex");
    assert.equal(actual, vector.bytes, vector.url);
  }
});

test("encodePayload emits the expected envelope shape", () => {
  const payload = compressURL("https://example.com");
  const bits = encodePayload(payload);

  assert.ok(bits.length >= 129);
  assert.equal(typeof bits[0], "boolean");
  assert.equal(bits[128], false);
});

test("generate emits SVG with custom colors and NFC logo", () => {
  const svg = generate("https://example.com", "FFFFFF", "000000", { type: CodeTypeNFC });

  assert.match(svg, /data-design="Fingerprint"/);
  assert.match(svg, /data-payload="https:\/\/example\.com"/);
  assert.match(svg, /data-logo-type="phone"/);
  assert.match(svg, /stroke:#ffffff/);
  assert.match(svg, /fill:#000000/);
});

test("generated arcs match Apple's reference SVGs for known fixtures", () => {
  const fixtures = [
    { url: "https://example.com", file: "apple_0.svg" },
    { url: "https://a.co", file: "apple_1.svg" },
    { url: "https://www.apple.com", file: "apple_2.svg" },
    { url: "https://appclip.example.com", file: "apple_4.svg" },
  ];

  for (const fixture of fixtures) {
    const generated = generateWithTemplate(fixture.url, 0);
    const reference = fs.readFileSync(path.join(repoRoot, "testdata", fixture.file), "utf8");

    assert.deepEqual(extractRingArcs(generated), extractRingArcs(reference), fixture.url);
  }
});

function extractRingArcs(svg) {
  const result = [];
  const pathRe = /<path d="([^"]+)" data-color="(\d)"/g;

  for (let ring = 1; ring <= 5; ring += 1) {
    const ringTag = `name="ring-${ring}"`;
    const start = svg.indexOf(ringTag);
    assert.ok(start >= 0, `missing ${ringTag}`);
    const end = svg.indexOf("</g>", start);
    assert.ok(end >= 0, `missing closing tag for ${ringTag}`);
    const chunk = svg.slice(start, end);

    const arcs = [];
    for (const match of chunk.matchAll(pathRe)) {
      arcs.push(`${match[1]}|${match[2]}`);
    }
    result.push(arcs);
  }

  return result;
}
