export class Color {
    r;
    g;
    b;
    constructor(r, g, b) {
        this.r = r;
        this.g = g;
        this.b = b;
    }
    hex() {
        return `#${toHex(this.r)}${toHex(this.g)}${toHex(this.b)}`;
    }
}
function toHex(n) {
    return n.toString(16).padStart(2, "0");
}
export function parseHexColor(value) {
    const normalized = String(value).replace(/^#/, "");
    if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
        throw new Error(`color must be 6 hex digits, got ${JSON.stringify(normalized)}`);
    }
    return new Color(Number.parseInt(normalized.slice(0, 2), 16), Number.parseInt(normalized.slice(2, 4), 16), Number.parseInt(normalized.slice(4, 6), 16));
}
export function midpointColor(a, b) {
    return new Color(Math.floor((a.r + b.r) / 2), Math.floor((a.g + b.g) / 2), Math.floor((a.b + b.b) / 2));
}
const basePalettes = [
    { foreground: new Color(0x00, 0x00, 0x00), background: new Color(0xff, 0xff, 0xff), third: new Color(0x88, 0x88, 0x88) },
    { foreground: new Color(0x77, 0x77, 0x77), background: new Color(0xff, 0xff, 0xff), third: new Color(0xaa, 0xaa, 0xaa) },
    { foreground: new Color(0xff, 0x3b, 0x30), background: new Color(0xff, 0xff, 0xff), third: new Color(0xff, 0x99, 0x99) },
    { foreground: new Color(0xee, 0x77, 0x33), background: new Color(0xff, 0xff, 0xff), third: new Color(0xee, 0xbb, 0x88) },
    { foreground: new Color(0x33, 0xaa, 0x22), background: new Color(0xff, 0xff, 0xff), third: new Color(0x99, 0xdd, 0x99) },
    { foreground: new Color(0x00, 0xa6, 0xa1), background: new Color(0xff, 0xff, 0xff), third: new Color(0x88, 0xdd, 0xcc) },
    { foreground: new Color(0x00, 0x7a, 0xff), background: new Color(0xff, 0xff, 0xff), third: new Color(0x77, 0xbb, 0xff) },
    { foreground: new Color(0x58, 0x56, 0xd6), background: new Color(0xff, 0xff, 0xff), third: new Color(0xbb, 0xbb, 0xee) },
    { foreground: new Color(0xcc, 0x73, 0xe1), background: new Color(0xff, 0xff, 0xff), third: new Color(0xee, 0xbb, 0xee) },
];
export function templates() {
    const result = [];
    for (let i = 0; i < basePalettes.length; i += 1) {
        const palette = basePalettes[i];
        result.push({
            index: i * 2,
            foreground: new Color(0xff, 0xff, 0xff),
            background: palette.foreground,
            third: palette.third,
        });
        result.push({
            index: i * 2 + 1,
            foreground: palette.foreground,
            background: palette.background,
            third: palette.third,
        });
    }
    return result;
}
export function templateByIndex(index) {
    const all = templates();
    if (index < 0 || index >= all.length) {
        throw new Error(`template index must be 0-17, got ${index}`);
    }
    const template = all[index];
    return {
        foreground: template.foreground,
        background: template.background,
        third: template.third,
    };
}
export function findThirdColor(foreground, background) {
    for (const palette of basePalettes) {
        if (sameColor(palette.foreground, foreground) && sameColor(palette.background, background)) {
            return palette.third;
        }
        if (sameColor(palette.foreground, background) && sameColor(palette.background, foreground)) {
            return palette.third;
        }
    }
    return midpointColor(foreground, background);
}
function sameColor(a, b) {
    return a.r === b.r && a.g === b.g && a.b === b.b;
}
