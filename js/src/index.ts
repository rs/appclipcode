import { Color, findThirdColor, parseHexColor, templateByIndex, templates } from "./color.js";
import { compressURL, encodePayload, GaloisField, newGF, newRSEncoder, RSEncoder } from "./codec.js";
import { renderSVG, type CodeType } from "./svg.js";
import type { Palette, Template } from "./color.js";

export const CodeTypeCamera: CodeType = "cam";
export const CodeTypeNFC: CodeType = "nfc";

export interface Options {
  type?: CodeType;
}

export function generate(rawURL: string, foreground: string, background: string, options?: Options): string {
  const fg = parseHexColor(foreground);
  const bg = parseHexColor(background);
  return generateWithPalette(rawURL, {
    foreground: fg,
    background: bg,
    third: findThirdColor(fg, bg),
  }, options);
}

export function generateWithTemplate(rawURL: string, templateIndex: number, options?: Options): string {
  return generateWithPalette(rawURL, templateByIndex(templateIndex), options);
}

export function renderSvg(bits: boolean[], palette: Palette, url: string, codeType: CodeType = CodeTypeCamera): string {
  return renderSVG(bits, palette, url, codeType);
}

function generateWithPalette(rawURL: string, palette: Palette, options?: Options): string {
  const type = options?.type ?? CodeTypeCamera;
  const compressed = compressURL(rawURL);
  const bits = encodePayload(compressed);
  return renderSVG(bits, palette, rawURL, type);
}

export {
  Color,
  GaloisField,
  RSEncoder,
  compressURL,
  encodePayload,
  newGF,
  newRSEncoder,
  parseHexColor,
  templateByIndex,
  templates,
};

export type {
  CodeType,
  Palette,
  Template,
};
