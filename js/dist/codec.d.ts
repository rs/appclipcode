export declare class GaloisField {
    readonly primitive: number;
    readonly size: number;
    readonly genBase: number;
    readonly expTbl: number[];
    readonly logTbl: number[];
    constructor(primitive: number, size: number, genBase: number);
    exp(a: number): number;
    log(a: number): number;
    multiply(a: number, b: number): number;
    inverse(a: number): number;
}
export declare class RSEncoder {
    readonly gf: GaloisField;
    readonly numParity: number;
    private genPoly;
    constructor(gf: GaloisField, numParity: number);
    encode(data: number[]): number[];
    private buildGenerator;
}
export declare function newGF(primitive: number, size: number, genBase: number): GaloisField;
export declare function newRSEncoder(gf: GaloisField, numParity: number): RSEncoder;
export declare function compressURL(rawURL: string): Uint8Array;
export declare function encodePayload(payloadInput: Uint8Array | number[]): boolean[];
