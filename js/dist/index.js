import { Color, findThirdColor, parseHexColor, templateByIndex, templates } from "./color.js";
import { compressURL, encodePayload, GaloisField, newGF, newRSEncoder, RSEncoder } from "./codec.js";
import { renderSVG } from "./svg.js";
export const CodeTypeCamera = "cam";
export const CodeTypeNFC = "nfc";
export function generate(rawURL, foreground, background, options) {
    const fg = parseHexColor(foreground);
    const bg = parseHexColor(background);
    return generateWithPalette(rawURL, {
        foreground: fg,
        background: bg,
        third: findThirdColor(fg, bg),
    }, options);
}
export function generateWithTemplate(rawURL, templateIndex, options) {
    return generateWithPalette(rawURL, templateByIndex(templateIndex), options);
}
export function renderSvg(bits, palette, url, codeType = CodeTypeCamera) {
    return renderSVG(bits, palette, url, codeType);
}
function generateWithPalette(rawURL, palette, options) {
    const type = options?.type ?? CodeTypeCamera;
    const compressed = compressURL(rawURL);
    const bits = encodePayload(compressed);
    return renderSVG(bits, palette, rawURL, type);
}
export { Color, GaloisField, RSEncoder, compressURL, encodePayload, newGF, newRSEncoder, parseHexColor, templateByIndex, templates, };
