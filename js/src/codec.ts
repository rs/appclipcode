import {
  CPQ_SYMBOLS,
  FIXED_TLD_INDEX,
  GAPS_BITS_ORDER_LUT,
  HOST_SYMBOLS,
  HUFFMAN_TLDS,
  KNOWN_WORD_INDEX,
  SPQ_SYMBOLS,
} from "./codec-data.js";
import {
  CPQ_TRIE_PACKED_DEFLATE_BASE64,
  HOST_TRIE_PACKED_DEFLATE_BASE64,
  SPQ_TRIE_PACKED_DEFLATE_BASE64,
} from "./trie-data.generated.js";
import { inflateSync } from "fflate";

interface FormatParams {
  gapsDataCount: number;
  gapsParityCount: number;
  arcsDataCount: number;
  arcsParityCount: number;
}

interface CompressionURL {
  host: string;
  path: string;
  query: string;
  fragment: string;
}

type UrlComponentKind = "path" | "query" | "fragment";

const FORMATS: Record<0 | 1, FormatParams> = {
  0: { gapsDataCount: 9, gapsParityCount: 4, arcsDataCount: 5, arcsParityCount: 2 },
  1: { gapsDataCount: 11, gapsParityCount: 2, arcsDataCount: 5, arcsParityCount: 2 },
};

const TEMPLATE_BITS = [false, true, false, true, false, true, false, false];
const FIXED6_ALPHABET = ".0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz|";
const FIXED6_INDEX = new Map<string, number>(
  Array.from(FIXED6_ALPHABET, (char, index) => [char, index]),
);

let hostCoder: MultiContextHuffmanCoder | undefined;
let cpqCoder: MultiContextHuffmanCoder | undefined;
let spqCoder: MultiContextHuffmanCoder | undefined;
let tldCoderCache: HuffmanCoder | undefined;
let initError: Error | undefined;

interface SymbolCoder {
  encode(symbolIndex: number): string;
  canEncode(symbolIndex: number): boolean;
}

export class GaloisField {
  readonly expTbl: number[];
  readonly logTbl: number[];

  constructor(
    public readonly primitive: number,
    public readonly size: number,
    public readonly genBase: number,
  ) {
    this.expTbl = new Array<number>(size * 2).fill(0);
    this.logTbl = new Array<number>(size).fill(0);

    let x = 1;
    for (let i = 0; i < size; i += 1) {
      this.expTbl[i] = x;
      this.logTbl[x] = i;
      x <<= 1;
      if (x >= size) {
        x ^= primitive;
        x &= size - 1;
      }
    }

    for (let i = size; i < size * 2; i += 1) {
      this.expTbl[i] = this.expTbl[i - size + 1];
    }
  }

  exp(a: number): number {
    return this.expTbl[a];
  }

  log(a: number): number {
    return this.logTbl[a];
  }

  multiply(a: number, b: number): number {
    if (a === 0 || b === 0) {
      return 0;
    }
    return this.expTbl[this.logTbl[a] + this.logTbl[b]];
  }

  inverse(a: number): number {
    return this.expTbl[this.size - 1 - this.logTbl[a]];
  }
}

export class RSEncoder {
  private genPoly: number[] = [1];

  constructor(
    public readonly gf: GaloisField,
    public readonly numParity: number,
  ) {
    this.buildGenerator();
  }

  encode(data: number[]): number[] {
    const result = new Array<number>(data.length + this.numParity).fill(0);
    for (let i = 0; i < data.length; i += 1) {
      result[i] = data[i];
    }

    for (let i = 0; i < data.length; i += 1) {
      const coef = result[i];
      if (coef !== 0) {
        for (let j = 1; j <= this.numParity; j += 1) {
          result[i + j] ^= this.gf.multiply(this.genPoly[j], coef);
        }
      }
    }

    for (let i = 0; i < data.length; i += 1) {
      result[i] = data[i];
    }
    return result;
  }

  private buildGenerator(): void {
    let generator = [1];

    for (let i = 0; i < this.numParity; i += 1) {
      const root = this.gf.exp(this.gf.genBase + i);
      const next = new Array<number>(generator.length + 1).fill(0);
      for (let j = 0; j < generator.length; j += 1) {
        next[j] = generator[j];
      }
      for (let j = 0; j < generator.length; j += 1) {
        next[j + 1] ^= this.gf.multiply(generator[j], root);
      }
      generator = next;
    }

    this.genPoly = generator;
  }
}

const GF16 = new GaloisField(0x13, 16, 0);
const GF256 = new GaloisField(0x11d, 256, 1);

class PackedHuffmanTrie {
  readonly numSymbols: number;
  readonly maxDepth = 2;
  readonly symbolIndexBits: number;
  readonly shapeBitsPerNode: number;
  readonly leafBitsPerNode: number;
  readonly shapeBytes: number;
  readonly leafBitOffset: number;

  constructor(
    private readonly data: Uint8Array,
    readonly symbols: string[],
    filename: string,
  ) {
    this.numSymbols = symbols.length;
    this.symbolIndexBits = Math.ceil(Math.log2(this.numSymbols));
    this.shapeBitsPerNode = this.numSymbols * 2 - 1;
    const expectedNodes = 1 + this.numSymbols + this.numSymbols * this.numSymbols;
    this.leafBitsPerNode = this.numSymbols * this.symbolIndexBits;
    this.shapeBytes = Math.ceil((expectedNodes * this.shapeBitsPerNode) / 8);
    this.leafBitOffset = this.shapeBytes * 8;
    const expectedSize = this.shapeBytes + Math.ceil((expectedNodes * this.leafBitsPerNode) / 8);
    if (data.length !== expectedSize) {
      throw new Error(`trie ${filename}: expected ${expectedSize} bytes, got ${data.length}`);
    }
  }

  buildCoder(nodeOffset: number): StaticHuffmanCoder {
    const codes = new Array<string>(this.numSymbols).fill("");
    let shapeBitOffset = nodeOffset * this.shapeBitsPerNode;
    let leafBitOffset = this.leafBitOffset + nodeOffset * this.leafBitsPerNode;

    const walk = (prefix: string): void => {
      const isLeaf = this.readBit(shapeBitOffset);
      shapeBitOffset += 1;

      if (isLeaf) {
        const symbolIndex = this.readBits(leafBitOffset, this.symbolIndexBits);
        leafBitOffset += this.symbolIndexBits;
        codes[symbolIndex] = prefix === "" ? "0" : prefix;
        return;
      }

      walk(`${prefix}0`);
      walk(`${prefix}1`);
    };

    walk("");

    if (shapeBitOffset !== (nodeOffset + 1) * this.shapeBitsPerNode) {
      throw new Error(`trie node ${nodeOffset}: malformed shape bitstream`);
    }
    if (leafBitOffset !== this.leafBitOffset + (nodeOffset + 1) * this.leafBitsPerNode) {
      throw new Error(`trie node ${nodeOffset}: malformed leaf bitstream`);
    }

    return new StaticHuffmanCoder(codes);
  }

  childOffset(parentOffset: number, symbolIndex: number): number {
    return this.numSymbols * parentOffset + 1 + symbolIndex;
  }

  private readBit(bitOffset: number): boolean {
    return ((this.data[bitOffset >> 3] >> (7 - (bitOffset & 7))) & 1) === 1;
  }

  private readBits(bitOffset: number, count: number): number {
    let value = 0;
    for (let i = 0; i < count; i += 1) {
      value = (value << 1) | (((this.data[(bitOffset + i) >> 3] >> (7 - ((bitOffset + i) & 7))) & 1) === 1 ? 1 : 0);
    }
    return value;
  }
}

class StaticHuffmanCoder implements SymbolCoder {
  constructor(private readonly codes: string[]) {}

  encode(symbolIndex: number): string {
    return this.codes[symbolIndex] ?? "";
  }

  canEncode(symbolIndex: number): boolean {
    return symbolIndex >= 0 && symbolIndex < this.codes.length && this.codes[symbolIndex] !== "";
  }
}

class HuffmanCoder implements SymbolCoder {
  readonly codes: string[];

  constructor(freqs: number[], symbols: string[]) {
    this.codes = new Array<string>(freqs.length).fill("");
    const leaves = freqs
      .map((freq, index) => ({
        freq,
        symbolIndex: index,
        symbol: symbols[index] ?? "",
        left: undefined as HuffmanNode | undefined,
        right: undefined as HuffmanNode | undefined,
        leftmost: symbols[index] ?? "",
      }))
      .filter((node) => node.freq > 0);

    if (leaves.length === 0) {
      return;
    }
    if (leaves.length === 1) {
      this.codes[leaves[0].symbolIndex] = "0";
      return;
    }

    const nodes: HuffmanNode[] = leaves;
    while (nodes.length > 1) {
      nodes.sort(compareNodes);
      const left = nodes.shift()!;
      const right = nodes.shift()!;
      nodes.push({
        freq: left.freq + right.freq,
        symbolIndex: -1,
        symbol: "",
        left,
        right,
        leftmost: left.leftmost,
      });
    }

    this.buildCodes(nodes[0], "");
  }

  encode(symbolIndex: number): string {
    return this.codes[symbolIndex] ?? "";
  }

  canEncode(symbolIndex: number): boolean {
    return symbolIndex >= 0 && symbolIndex < this.codes.length && this.codes[symbolIndex] !== "";
  }

  private buildCodes(node: HuffmanNode | undefined, prefix: string): void {
    if (!node) {
      return;
    }
    if (!node.left && !node.right) {
      this.codes[node.symbolIndex] = prefix === "" ? "0" : prefix;
      return;
    }
    this.buildCodes(node.left, `${prefix}0`);
    this.buildCodes(node.right, `${prefix}1`);
  }
}

interface HuffmanNode {
  freq: number;
  symbolIndex: number;
  symbol: string;
  left?: HuffmanNode;
  right?: HuffmanNode;
  leftmost: string;
}

function compareNodes(a: HuffmanNode, b: HuffmanNode): number {
  if (a.freq !== b.freq) {
    return a.freq - b.freq;
  }
  if (a.leftmost < b.leftmost) {
    return -1;
  }
  if (a.leftmost > b.leftmost) {
    return 1;
  }
  return 0;
}

class MultiContextHuffmanCoder {
  private readonly symbolIndexByValue = new Map<string, number>();
  private readonly cache = new Map<number, SymbolCoder>();

  constructor(readonly trie: PackedHuffmanTrie) {
    trie.symbols.forEach((symbol, index) => this.symbolIndexByValue.set(symbol, index));
  }

  coderForNode(nodeOffset: number): SymbolCoder {
    const cached = this.cache.get(nodeOffset);
    if (cached) {
      return cached;
    }
    const coder = this.trie.buildCoder(nodeOffset);
    this.cache.set(nodeOffset, coder);
    return coder;
  }

  symbolIndex(symbol: string): number {
    return this.symbolIndexByValue.get(symbol) ?? -1;
  }

  encode(symbols: string[]): string {
    return this.encodeWithStartContext(symbols, "");
  }

  encodeWithStartContext(symbols: string[], startContext: string): string {
    let nodeOffset = 0;
    let depth = 0;

    for (const char of Array.from(startContext)) {
      const index = this.symbolIndex(char);
      if (index < 0) {
        throw new Error(`unknown start context symbol: ${JSON.stringify(char)}`);
      }
      [nodeOffset, depth] = this.advanceContext(nodeOffset, depth, index);
    }

    let bits = "";
    for (const symbol of symbols) {
      const index = this.symbolIndex(symbol);
      if (index < 0) {
        throw new Error(`unknown symbol: ${JSON.stringify(symbol)}`);
      }
      const coder = this.coderForNode(nodeOffset);
      if (!coder.canEncode(index)) {
        throw new Error(`cannot encode symbol ${JSON.stringify(symbol)} at context node ${nodeOffset}`);
      }
      bits += coder.encode(index);
      [nodeOffset, depth] = this.advanceContext(nodeOffset, depth, index);
    }
    return bits;
  }

  advanceContext(nodeOffset: number, depth: number, symbolIndex: number): [number, number] {
    if (depth < this.trie.maxDepth) {
      return [this.trie.childOffset(nodeOffset, symbolIndex), depth + 1];
    }
    const prevSymbolIndex = (nodeOffset - 1) % this.trie.numSymbols;
    return [this.trie.childOffset(1 + prevSymbolIndex, symbolIndex), depth];
  }
}

function ensureInit(): void {
  if (initError) {
    throw initError;
  }
  if (hostCoder && cpqCoder && spqCoder) {
    return;
  }

  try {
    hostCoder = new MultiContextHuffmanCoder(loadTrie(HOST_TRIE_PACKED_DEFLATE_BASE64, "h.data", HOST_SYMBOLS));
    cpqCoder = new MultiContextHuffmanCoder(loadTrie(CPQ_TRIE_PACKED_DEFLATE_BASE64, "cpq.data", CPQ_SYMBOLS));
    spqCoder = new MultiContextHuffmanCoder(loadTrie(SPQ_TRIE_PACKED_DEFLATE_BASE64, "spq.data", SPQ_SYMBOLS));
  } catch (error) {
    initError = error instanceof Error ? error : new Error(String(error));
    throw initError;
  }
}

function loadTrie(base64: string, filename: string, symbols: string[]): PackedHuffmanTrie {
  const data = inflateSync(decodeBase64(base64));
  return new PackedHuffmanTrie(data, symbols, filename);
}

function decodeBase64(base64: string): Uint8Array {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
  const lookup = new Uint8Array(256);
  lookup.fill(255);

  for (let i = 0; i < alphabet.length; i += 1) {
    lookup[alphabet.charCodeAt(i)] = i;
  }

  const sanitized = base64.replace(/\s+/g, "");
  const padding =
    (sanitized.endsWith("==") ? 2 : 0) ||
    (sanitized.endsWith("=") ? 1 : 0);
  const outputLength = Math.floor((sanitized.length * 3) / 4) - padding;
  const output = new Uint8Array(outputLength);

  let outIndex = 0;

  for (let i = 0; i < sanitized.length; i += 4) {
    const a = lookup[sanitized.charCodeAt(i)];
    const b = lookup[sanitized.charCodeAt(i + 1)];
    const cChar = sanitized[i + 2];
    const dChar = sanitized[i + 3];
    const c = cChar === "=" ? 0 : lookup[sanitized.charCodeAt(i + 2)];
    const d = dChar === "=" ? 0 : lookup[sanitized.charCodeAt(i + 3)];

    const value = (a << 18) | (b << 12) | (c << 6) | d;

    output[outIndex] = (value >> 16) & 0xff;
    outIndex += 1;

    if (cChar !== "=" && outIndex < output.length) {
      output[outIndex] = (value >> 8) & 0xff;
      outIndex += 1;
    }

    if (dChar !== "=" && outIndex < output.length) {
      output[outIndex] = value & 0xff;
      outIndex += 1;
    }
  }

  return output;
}

function tldHuffmanCoder(): HuffmanCoder {
  if (!tldCoderCache) {
    tldCoderCache = new HuffmanCoder(
      HUFFMAN_TLDS.map((entry) => entry.freq),
      HUFFMAN_TLDS.map((entry) => entry.tld),
    );
  }
  return tldCoderCache;
}

export function newGF(primitive: number, size: number, genBase: number): GaloisField {
  return new GaloisField(primitive, size, genBase);
}

export function newRSEncoder(gf: GaloisField, numParity: number): RSEncoder {
  return new RSEncoder(gf, numParity);
}

export function compressURL(rawURL: string): Uint8Array {
  try {
    ensureInit();
  } catch (error) {
    throw new Error(`init coders: ${(error as Error).message}`);
  }

  let parsed: CompressionURL;
  try {
    parsed = parseCompressionURL(rawURL);
  } catch (error) {
    throw new Error(`invalid URL: ${(error as Error).message}`);
  }

  let host = parsed.host;
  let subdomainType = 0;
  if (host.startsWith("appclip.")) {
    subdomainType = 1;
    host = host.slice("appclip.".length);
  }

  const hasPathOrQuery = parsed.path !== "" || parsed.query !== "" || parsed.fragment !== "";

  let templateType = 0;
  let pathQueryBits = "";
  if (hasPathOrQuery) {
    try {
      const encoded = choosePathQueryEncoding(parsed.path, parsed.query, parsed.fragment);
      pathQueryBits = encoded.bits;
      templateType = encoded.templateType;
    } catch (error) {
      throw new Error(`encode path/query: ${(error as Error).message}`);
    }
  }

  let hostBits = "";
  let hostFormat = 0;
  try {
    const encodedHost = encodeHost(host, hasPathOrQuery);
    hostBits = encodedHost.bits;
    hostFormat = encodedHost.hostFormat;
  } catch (error) {
    throw new Error(`encode host: ${(error as Error).message}`);
  }

  let bits = "1";
  bits += templateType === 1 ? "1" : "0";
  bits += subdomainType === 1 ? "1" : "0";

  switch (hostFormat) {
    case 0:
      bits += "0";
      break;
    case 1:
      bits += "10";
      break;
    case 2:
      bits += "11";
      break;
    default:
      throw new Error(`unsupported host format ${hostFormat}`);
  }

  bits += hostBits;
  bits += pathQueryBits;
  return rawBitsToBytes(bits);
}

export function encodePayload(payloadInput: Uint8Array | number[]): boolean[] {
  const payload = toUint8Array(payloadInput);
  let firstNonZero = 0;
  while (firstNonZero < payload.length && payload[firstNonZero] === 0) {
    firstNonZero += 1;
  }
  const trimmed = payload.slice(firstNonZero);
  const version: 0 | 1 = trimmed.length > 14 ? 1 : 0;
  const format = FORMATS[version];
  const totalData = format.gapsDataCount + format.arcsDataCount;

  const padded = new Uint8Array(totalData);
  const rightAligned = trimmed.length <= totalData ? trimmed : trimmed.slice(trimmed.length - totalData);
  padded.set(rightAligned, totalData - rightAligned.length);

  const scrambled = new Uint8Array(totalData);
  for (let i = 0; i < totalData; i += 1) {
    scrambled[i] = padded[totalData - 1 - i] ^ 0xa5;
  }

  const gapsData = scrambled.slice(0, format.gapsDataCount);
  const arcsData = scrambled.slice(totalData - format.arcsDataCount);

  const gapsEncoded = new RSEncoder(GF256, format.gapsParityCount).encode(Array.from(gapsData));
  const gapsBits = blocksToBits(gapsEncoded, 8);

  let gapZeros = 0;
  for (const bit of gapsBits) {
    if (!bit) {
      gapZeros += 1;
    }
  }

  let inverted = false;
  if (gapZeros <= 51) {
    inverted = true;
    for (let i = 0; i < gapsBits.length; i += 1) {
      gapsBits[i] = !gapsBits[i];
    }
  }

  const metaData = [version >> 3, ((version & 7) << 1) | (inverted ? 1 : 0)];
  const metaBits = blocksToBits(new RSEncoder(GF16, 2).encode(metaData), 4);
  const arcsBits = blocksToBits(new RSEncoder(GF256, format.arcsParityCount).encode(Array.from(arcsData)), 8);

  const prePerm = new Array<boolean>(128).fill(false);
  for (let i = 0; i < metaBits.length; i += 1) {
    prePerm[i] = metaBits[i];
  }
  for (let i = 0; i < gapsBits.length; i += 1) {
    prePerm[16 + i] = gapsBits[i];
  }
  for (let i = 0; i < TEMPLATE_BITS.length; i += 1) {
    prePerm[120 + i] = TEMPLATE_BITS[i];
  }

  let zeroCount128 = 0;
  for (const bit of prePerm) {
    if (!bit) {
      zeroCount128 += 1;
    }
  }

  const output = new Array<boolean>(129 + zeroCount128).fill(false);
  for (let i = 0; i < 128; i += 1) {
    output[GAPS_BITS_ORDER_LUT[i]] = prePerm[i];
  }

  let pos = 128;
  output[pos] = false;
  pos += 1;

  for (let i = 0; i < arcsBits.length; i += 1) {
    output[pos + i] = arcsBits[i];
  }
  pos += arcsBits.length;

  const extraCount = zeroCount128 - arcsBits.length;
  if (extraCount > 0) {
    for (let i = 0; i < extraCount; i += 1) {
      output[pos + i] = gapsBits[i];
    }
  }

  return output;
}

function choosePathQueryEncoding(pathValue: string, query: string, fragment: string): { bits: string; templateType: number } {
  const candidates: Array<{ bits: string; templateType: number }> = [];

  try {
    candidates.push({ bits: encodeTemplatePathQuery(pathValue, query, fragment), templateType: 1 });
  } catch {}

  try {
    candidates.push({ bits: encodeNonTemplatePathQuery(pathValue, query, fragment), templateType: 0 });
  } catch {}

  if (candidates.length === 0) {
    throw new Error("cannot encode path/query");
  }

  let best = candidates[0];
  for (const candidate of candidates.slice(1)) {
    if (candidate.bits.length < best.bits.length) {
      best = candidate;
      continue;
    }
    if (candidate.bits.length === best.bits.length && best.templateType === 1 && candidate.templateType === 0) {
      best = candidate;
    }
  }

  return best;
}

function encodeTemplatePathQuery(pathValue: string, query: string, fragment: string): string {
  if (fragment !== "") {
    throw new Error("template mode does not support fragments");
  }

  const match = matchAutoQueryTemplate(pathValue, query);
  if (!match.ok) {
    throw new Error("path/query do not match template auto-query format");
  }

  let bits = "";
  if (match.pathWord !== "") {
    const index = KNOWN_WORD_INDEX[match.pathWord];
    if (index > 0xff) {
      throw new Error(`template path word ${JSON.stringify(match.pathWord)} exceeds 8-bit auto-query range`);
    }
    bits += "0";
    bits += intToBits(index, 8);
  }

  if (match.params.length > 0) {
    bits += "1";
    for (let i = 0; i < match.params.length; i += 1) {
      bits += encodeAutoQueryTemplateQueryComponent(match.params[i], i + 1 < match.params.length);
    }
  }

  if (bits === "") {
    throw new Error("template mode requires a path word or auto-query parameters");
  }

  return bits;
}

function matchAutoQueryTemplate(pathValue: string, query: string): { pathWord: string; params: string[]; ok: boolean } {
  if (pathValue.length >= 2 && pathValue.endsWith("/")) {
    return { pathWord: "", params: [], ok: false };
  }
  if (query.endsWith("&")) {
    return { pathWord: "", params: [], ok: false };
  }

  const pathParts = splitNonEmpty(pathValue, "/");
  if (pathParts.length > 1) {
    return { pathWord: "", params: [], ok: false };
  }

  let pathWord = "";
  if (pathParts.length === 1) {
    const index = KNOWN_WORD_INDEX[pathParts[0]];
    if (index === undefined || index > 0xff) {
      return { pathWord: "", params: [], ok: false };
    }
    pathWord = pathParts[0];
  }

  const params = splitNonEmpty(query, "&");
  if (params.length === 0) {
    return { pathWord, params: [], ok: pathWord !== "" || pathValue === "/" };
  }

  for (let i = 0; i < params.length; i += 1) {
    const separator = params[i].indexOf("=");
    if (separator < 0) {
      return { pathWord: "", params: [], ok: false };
    }
    const key = params[i].slice(0, separator);
    const expected = i === 0 ? "p" : `p${i}`;
    if (key !== expected) {
      return { pathWord: "", params: [], ok: false };
    }
  }

  return { pathWord, params, ok: true };
}

function splitNonEmpty(value: string, separator: string): string[] {
  if (value === "") {
    return [];
  }
  return value.split(separator).filter((part) => part !== "");
}

function encodeAutoQueryTemplateQueryComponent(param: string, hasMore: boolean): string {
  const separator = param.indexOf("=");
  if (separator < 0) {
    throw new Error(`template query parameter ${JSON.stringify(param)} missing '='`);
  }
  const value = param.slice(separator + 1);

  let bestBits = "";
  try {
    bestBits = `00${encodeSPQValue("=", value, hasMore)}`;
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `01${encodeULEB128Value(value)}`);
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `10${encodeFixed6Value(value, hasMore)}`);
  } catch {}

  if (bestBits === "") {
    throw new Error(`cannot encode template query value from ${JSON.stringify(param)}`);
  }
  return bestBits;
}

function encodeHost(host: string, hasPathOrQuery: boolean): { bits: string; hostFormat: number } {
  const lastDot = host.lastIndexOf(".");
  if (lastDot < 0) {
    throw new Error(`host has no TLD: ${JSON.stringify(host)}`);
  }

  const tld = host.slice(lastDot);
  let domain = host.slice(0, lastDot);
  if (hasPathOrQuery) {
    domain += "|";
  }

  const currentHostCoder = hostCoder!;
  const domainBits = currentHostCoder.encode(toCharSlice(domain));
  const tldIndex = HUFFMAN_TLDS.findIndex((entry) => entry.tld === tld);
  const tldCoder = tldHuffmanCoder();

  if (tldIndex >= 0 && tldCoder.canEncode(tldIndex)) {
    return { bits: tldCoder.encode(tldIndex) + domainBits, hostFormat: 0 };
  }

  if (FIXED_TLD_INDEX[tld] !== undefined) {
    return { bits: intToBits(FIXED_TLD_INDEX[tld], 8) + domainBits, hostFormat: 1 };
  }

  let fullHost = host;
  if (hasPathOrQuery) {
    fullHost += "|";
  }
  return { bits: currentHostCoder.encode(toCharSlice(fullHost)), hostFormat: 2 };
}

function encodeNonTemplatePathQuery(pathValue: string, query: string, fragment: string): string {
  let combinedBits = "";
  let combinedError: Error | undefined;
  try {
    combinedBits = encodeCombinedPathQuery(pathValue, query, fragment);
  } catch (error) {
    combinedError = error as Error;
  }

  let segmentedBits = "";
  let segmentedError: Error | undefined;
  try {
    segmentedBits = encodeSegmentedPathQuery(pathValue, query, fragment);
  } catch (error) {
    segmentedError = error as Error;
  }

  if (!combinedError && !segmentedError) {
    return combinedBits.length <= segmentedBits.length ? `0${combinedBits}` : `1${segmentedBits}`;
  }
  if (!combinedError) {
    return `0${combinedBits}`;
  }
  if (!segmentedError) {
    return `1${segmentedBits}`;
  }

  throw new Error(`cannot encode path/query: combined: ${combinedError.message}, segmented: ${segmentedError.message}`);
}

function encodeCombinedPathQuery(pathValue: string, query: string, fragment: string): string {
  let combined = pathValue;
  if (query !== "") {
    combined += `?${query}`;
  }
  if (fragment !== "") {
    combined += `#${fragment}`;
  }
  if (combined.startsWith("/") && (combined.length === 1 || combined[1] !== "#")) {
    combined = combined.slice(1);
  }
  if (combined === "") {
    throw new Error("combined path/query is empty");
  }
  return cpqCoder!.encode(toCharSlice(combined));
}

function encodeSegmentedPathQuery(pathValue: string, query: string, fragment: string): string {
  if (fragment !== "") {
    throw new Error("segmented mode does not support fragments");
  }

  const items = buildSegmentedPathItems(pathValue);
  let bits = "";

  for (let i = 0; i < items.length; i += 1) {
    const item = items[i];
    if (item === "/") {
      bits += "10";
      continue;
    }
    bits += "0";
    bits += encodeSegmentedPathComponent(item, i + 1 < items.length || query !== "");
  }

  if (query !== "") {
    const params = query.split("&");
    if (params.length === 0) {
      throw new Error("invalid segmented query");
    }
    bits += "11";
    for (let i = 0; i < params.length; i += 1) {
      bits += encodeSegmentedQueryComponent(params[i], i + 1 < params.length);
    }
  }

  if (bits === "") {
    throw new Error("segmented path/query is empty");
  }
  return bits;
}

function buildSegmentedPathItems(pathValue: string): string[] {
  if (pathValue === "") {
    return [];
  }

  const items = pathValue
    .replace(/^\//, "")
    .split("/")
    .filter((part) => part !== "");

  if (items.length === 0 || pathValue.endsWith("/")) {
    items.push("/");
  }

  return items;
}

function encodeSegmentedPathComponent(component: string, needsTerminator: boolean): string {
  if (component === "") {
    throw new Error("cannot encode empty path component");
  }

  let bestBits = "";
  try {
    bestBits = `00${encodeSPQValue("", component, needsTerminator)}`;
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `01${encodeULEB128Value(component)}`);
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `10${encodeFixed6Value(component, needsTerminator)}`);
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `11${encodeKnownWordValue(component)}`);
  } catch {}

  if (bestBits === "") {
    throw new Error(`cannot encode segmented path component ${JSON.stringify(component)}`);
  }
  return bestBits;
}

function encodeSegmentedQueryComponent(param: string, hasMore: boolean): string {
  const separator = param.indexOf("=");
  if (separator < 0) {
    throw new Error(`cannot encode segmented query parameter ${JSON.stringify(param)}`);
  }

  const key = param.slice(0, separator);
  const value = param.slice(separator + 1);
  const keyWithTerminator = encodeSPQValue("?", key, true);
  const keyWithoutTerminator = encodeSPQValue("?", key, hasMore);

  let bestBits = "";
  try {
    bestBits = `00${keyWithTerminator}${encodeSPQValue("=", value, hasMore)}`;
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `01${encodeULEB128Value(value)}${keyWithoutTerminator}`);
  } catch {}
  try {
    bestBits = shorterBits(bestBits, `10${keyWithTerminator}${encodeFixed6Value(value, hasMore)}`);
  } catch {}

  if (bestBits === "") {
    throw new Error(`cannot encode segmented query parameter ${JSON.stringify(param)}`);
  }
  return bestBits;
}

function encodeSPQValue(startContext: string, value: string, needsTerminator: boolean): string {
  const text = needsTerminator ? `${value}|` : value;
  return spqCoder!.encodeWithStartContext(toCharSlice(text), startContext);
}

function encodeFixed6Value(value: string, needsTerminator: boolean): string {
  return encodeFixed6(needsTerminator ? `${value}|` : value);
}

function encodeKnownWordValue(value: string): string {
  const index = KNOWN_WORD_INDEX[value];
  if (index === undefined || index > 0xff) {
    throw new Error(`unknown word ${JSON.stringify(value)}`);
  }
  return intToBits(index, 8);
}

function encodeULEB128Value(value: string): string {
  if (value === "") {
    throw new Error("empty numeric value");
  }
  if (!/^[0-9]+$/.test(value)) {
    throw new Error(`non-decimal digit in ${JSON.stringify(value)}`);
  }

  let current: bigint;
  try {
    current = BigInt(value);
  } catch {
    throw new Error(`invalid integer ${JSON.stringify(value)}`);
  }

  const bytes: number[] = [];
  if (current === 0n) {
    bytes.push(0);
  } else {
    while (current > 0n) {
      let chunk = Number(current & 0x7fn);
      current >>= 7n;
      if (current > 0n) {
        chunk |= 0x80;
      }
      bytes.push(chunk);
    }
  }

  return bytes.map((byte) => intToBits(byte, 8)).join("");
}

function encodeFixed6(value: string): string {
  let bits = "";
  for (const char of Array.from(value)) {
    const index = FIXED6_INDEX.get(char);
    if (index === undefined) {
      throw new Error(`symbol ${JSON.stringify(char)} not encodable by fixed6`);
    }
    bits += intToBits(index, 6);
  }
  return bits;
}

function parseCompressionURL(rawURL: string): CompressionURL {
  const scheme = "https://";
  if (rawURL.length < scheme.length || rawURL.slice(0, scheme.length).toLowerCase() !== scheme) {
    throw new Error("URL scheme must be https");
  }

  const rest = rawURL.slice(scheme.length);
  const authorityEnd = indexOfAny(rest, "/?#");
  const authority = authorityEnd >= 0 ? rest.slice(0, authorityEnd) : rest;
  let suffix = authorityEnd >= 0 ? rest.slice(authorityEnd) : "";

  if (authority === "") {
    throw new Error("URL must have a host");
  }
  if (authority.includes("@")) {
    throw new Error("URL must not have user info");
  }
  if (authority.includes(":")) {
    throw new Error("URL must not have a port");
  }

  const result: CompressionURL = {
    host: canonicalizeHost(authority),
    path: "",
    query: "",
    fragment: "",
  };

  if (suffix.startsWith("/")) {
    let pathEnd = suffix.length;
    const next = indexOfAny(suffix, "?#");
    if (next >= 0) {
      pathEnd = next;
    }
    result.path = canonicalizeURLComponent(suffix.slice(0, pathEnd), "path");
    suffix = suffix.slice(pathEnd);
  }

  if (suffix.startsWith("?")) {
    suffix = suffix.slice(1);
    let queryEnd = suffix.length;
    const fragmentIndex = suffix.indexOf("#");
    if (fragmentIndex >= 0) {
      queryEnd = fragmentIndex;
    }
    result.query = canonicalizeURLComponent(suffix.slice(0, queryEnd), "query");
    suffix = suffix.slice(queryEnd);
  }

  if (suffix.startsWith("#")) {
    result.fragment = canonicalizeURLComponent(suffix.slice(1), "fragment");
  }

  return result;
}

function canonicalizeHost(authority: string): string {
  const lower = authority.toLowerCase();
  for (let i = 0; i < lower.length; i += 1) {
    const code = lower.charCodeAt(i);
    const char = lower[i];
    if (code >= 0x80) {
      throw new Error("URL contains unsupported host characters");
    }
    if (
      (char >= "a" && char <= "z") ||
      (char >= "0" && char <= "9") ||
      char === "." ||
      char === "-"
    ) {
      continue;
    }
    throw new Error("URL contains unsupported host characters");
  }

  for (const label of lower.split(".")) {
    if (label.startsWith("xn--")) {
      throw new Error("URL contains unsupported host characters");
    }
  }

  return lower;
}

function canonicalizeURLComponent(value: string, kind: UrlComponentKind): string {
  let output = "";
  for (let i = 0; i < value.length; ) {
    const code = value.charCodeAt(i);
    const char = value[i];

    if (char === "%") {
      if (i + 2 < value.length && isHexDigit(value[i + 1]) && isHexDigit(value[i + 2])) {
        output += value.slice(i, i + 3);
        i += 3;
        continue;
      }
      throw new Error("URL contains invalid percent escape");
    }

    if (code < 0x20 || code === 0x7f || code >= 0x80) {
      throw new Error("URL contains unsupported characters");
    }
    if (rejectsRawURLComponentByte(char, kind)) {
      throw new Error("URL contains unsupported characters");
    }
    output += isAllowedURLComponentByte(char, kind) ? char : writePercentEncodedByte(code);
    i += 1;
  }
  return output;
}

function rejectsRawURLComponentByte(char: string, kind: UrlComponentKind): boolean {
  switch (char) {
    case " ":
    case '"':
    case "%":
    case "<":
    case ">":
    case "\\":
    case "^":
    case "`":
    case "{":
    case "|":
    case "}":
      return true;
    case "#":
      return kind === "fragment";
    default:
      return false;
  }
}

function isAllowedURLComponentByte(char: string, kind: UrlComponentKind): boolean {
  if (isASCIIAlphaNum(char)) {
    return true;
  }

  switch (char) {
    case "-":
    case ".":
    case "_":
    case "~":
    case "!":
    case "$":
    case "&":
    case "'":
    case "(":
    case ")":
    case "*":
    case "+":
    case ",":
    case ";":
    case "=":
    case ":":
    case "@":
    case "/":
      return true;
    case "?":
      return kind !== "path";
    case "#":
      return false;
    default:
      return false;
  }
}

function isASCIIAlphaNum(char: string): boolean {
  return /^[0-9A-Za-z]$/.test(char);
}

function isHexDigit(char: string): boolean {
  return /^[0-9A-Fa-f]$/.test(char);
}

function writePercentEncodedByte(code: number): string {
  return `%${code.toString(16).toUpperCase().padStart(2, "0")}`;
}

function intToBits(value: number, bitCount: number): string {
  let bits = "";
  for (let i = bitCount - 1; i >= 0; i -= 1) {
    bits += (value >> i) & 1 ? "1" : "0";
  }
  return bits;
}

function rawBitsToBytes(bits: string): Uint8Array {
  if (bits.length > 128) {
    throw new Error(`compressed URL too large: ${bits.length} bits (max 128)`);
  }

  const padded = `${"0".repeat(128 - bits.length)}${bits}`;
  const result = new Uint8Array(16);
  for (let i = 0; i < 16; i += 1) {
    let value = 0;
    for (let j = 0; j < 8; j += 1) {
      if (padded[i * 8 + j] === "1") {
        value |= 1 << (7 - j);
      }
    }
    result[i] = value;
  }
  return result;
}

function blocksToBits(symbols: number[], bitsPerSymbol: number): boolean[] {
  const bits = new Array<boolean>(symbols.length * bitsPerSymbol).fill(false);
  for (let i = 0; i < symbols.length; i += 1) {
    for (let j = bitsPerSymbol - 1; j >= 0; j -= 1) {
      bits[i * bitsPerSymbol + (bitsPerSymbol - 1 - j)] = ((symbols[i] >> j) & 1) === 1;
    }
  }
  return bits;
}

function toCharSlice(value: string): string[] {
  return Array.from(value);
}

function shorterBits(current: string, candidate: string): string {
  if (current === "" || candidate.length < current.length) {
    return candidate;
  }
  return current;
}

function indexOfAny(value: string, chars: string): number {
  let best = -1;
  for (const char of Array.from(chars)) {
    const index = value.indexOf(char);
    if (index >= 0 && (best < 0 || index < best)) {
      best = index;
    }
  }
  return best;
}

function toUint8Array(value: Uint8Array | number[]): Uint8Array {
  return value instanceof Uint8Array ? value : Uint8Array.from(value);
}
