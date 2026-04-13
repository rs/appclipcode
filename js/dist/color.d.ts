export declare class Color {
    readonly r: number;
    readonly g: number;
    readonly b: number;
    constructor(r: number, g: number, b: number);
    hex(): string;
}
export interface Palette {
    foreground: Color;
    background: Color;
    third: Color;
}
export interface Template extends Palette {
    index: number;
}
export declare function parseHexColor(value: string): Color;
export declare function midpointColor(a: Color, b: Color): Color;
export declare function templates(): Template[];
export declare function templateByIndex(index: number): Palette;
export declare function findThirdColor(foreground: Color, background: Color): Color;
