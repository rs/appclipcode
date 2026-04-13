import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const cliPath = path.join(__dirname, "../dist/cli.js");

test("cli prints svg to stdout for a positional url", () => {
  const result = spawnSync(process.execPath, [cliPath, "https://example.com"], {
    encoding: "utf8",
  });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /<svg data-design="Fingerprint"/);
  assert.match(result.stdout, /data-payload="https:\/\/example\.com"/);
});

test("cli writes svg to a file when --output is provided", () => {
  const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), "appclipcode-cli-"));
  const outputPath = path.join(tempDir, "code.svg");

  try {
    const result = spawnSync(
      process.execPath,
      [cliPath, "https://example.com", "--index", "4", "--output", outputPath],
      { encoding: "utf8" },
    );

    assert.equal(result.status, 0, result.stderr);
    assert.equal(result.stdout, "");
    const svg = fs.readFileSync(outputPath, "utf8");
    assert.match(svg, /fill:#ff3b30/);
  } finally {
    fs.rmSync(tempDir, { recursive: true, force: true });
  }
});

test("cli rejects partial custom color input", () => {
  const result = spawnSync(process.execPath, [cliPath, "https://example.com", "--fg", "FFFFFF"], {
    encoding: "utf8",
  });

  assert.notEqual(result.status, 0);
  assert.match(result.stderr, /specify both --fg and --bg/);
});

test("cli accepts 8-digit hex colors", () => {
  const result = spawnSync(
    process.execPath,
    [cliPath, "https://example.com", "--fg", "FFFFFF80", "--bg", "00000000", "--type", "nfc"],
    { encoding: "utf8" },
  );

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /stroke:#ffffff80/);
  assert.match(result.stdout, /fill:#00000000/);
});

test("cli lists templates", () => {
  const result = spawnSync(process.execPath, [cliPath, "templates"], {
    encoding: "utf8",
  });

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /index=0/);
  assert.match(result.stdout, /index=17/);
});
