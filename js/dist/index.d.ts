import { Color, parseHexColor, templateByIndex, templates } from "./color.js";
import { compressURL, encodePayload, GaloisField, newGF, newRSEncoder, RSEncoder } from "./codec.js";
import { type CodeType } from "./svg.js";
import type { Palette, Template } from "./color.js";
export declare const CodeTypeCamera: CodeType;
export declare const CodeTypeNFC: CodeType;
export interface Options {
    type?: CodeType;
}
export declare function generate(rawURL: string, foreground: string, background: string, options?: Options): string;
export declare function generateWithTemplate(rawURL: string, templateIndex: number, options?: Options): string;
export declare function renderSvg(bits: boolean[], palette: Palette, url: string, codeType?: CodeType): string;
export { Color, GaloisField, RSEncoder, compressURL, encodePayload, newGF, newRSEncoder, parseHexColor, templateByIndex, templates, };
export type { CodeType, Palette, Template, };
