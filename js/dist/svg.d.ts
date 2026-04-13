import type { Palette } from "./color.js";
export type CodeType = "cam" | "nfc";
export declare function renderSVG(bits: boolean[], palette: Palette, url: string, codeType: CodeType): string;
