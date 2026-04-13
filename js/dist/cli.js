#!/usr/bin/env node
import fs from "node:fs";
import process from "node:process";
import { CodeTypeCamera, CodeTypeNFC, generate, generateWithTemplate, templates, } from "./index.js";
function main(argv) {
    let parsed;
    try {
        parsed = parseArgs(argv);
    }
    catch (error) {
        printError(error.message);
        printUsage();
        return 1;
    }
    if (parsed.help) {
        printUsage();
        return 0;
    }
    if (parsed.positionals.length === 0) {
        printUsage();
        return 1;
    }
    if (parsed.positionals[0] === "templates") {
        if (parsed.positionals.length > 1) {
            printError("templates does not accept positional arguments");
            return 1;
        }
        printTemplates();
        return 0;
    }
    if (parsed.positionals.length > 1) {
        printError(`unexpected extra arguments: ${parsed.positionals.slice(1).join(" ")}`);
        printUsage();
        return 1;
    }
    const codeType = normalizeCodeType(parsed.type);
    if (!codeType) {
        printError(`invalid --type value ${JSON.stringify(parsed.type)}; expected "cam" or "nfc"`);
        return 1;
    }
    const url = parsed.positionals[0];
    let svg;
    try {
        svg = generateSvg(url, parsed, codeType);
    }
    catch (error) {
        printError(error.message);
        return 1;
    }
    try {
        if (parsed.output !== "") {
            fs.writeFileSync(parsed.output, svg, "utf8");
        }
        else {
            process.stdout.write(svg);
        }
    }
    catch (error) {
        printError(`failed to write output: ${error.message}`);
        return 1;
    }
    return 0;
}
function generateSvg(url, parsed, codeType) {
    const options = { type: codeType };
    const hasCustomColors = parsed.foreground !== "" || parsed.background !== "";
    const hasIndex = parsed.index !== "";
    if (!hasCustomColors) {
        const index = hasIndex ? parseTemplateIndex(parsed.index) : 0;
        return generateWithTemplate(url, index, options);
    }
    if (parsed.foreground === "" || parsed.background === "") {
        throw new Error("specify both --fg and --bg");
    }
    if (hasIndex) {
        throw new Error("specify either --index or --fg/--bg, not both");
    }
    return generate(url, parsed.foreground, parsed.background, options);
}
function parseTemplateIndex(value) {
    if (!/^-?\d+$/.test(value)) {
        throw new Error(`invalid --index value ${JSON.stringify(value)}`);
    }
    const parsed = Number.parseInt(value, 10);
    if (!Number.isSafeInteger(parsed)) {
        throw new Error(`invalid --index value ${JSON.stringify(value)}`);
    }
    return parsed;
}
function normalizeCodeType(value) {
    if (value === CodeTypeCamera || value === CodeTypeNFC) {
        return value;
    }
    return null;
}
function parseArgs(argv) {
    const parsed = {
        background: "",
        foreground: "",
        help: false,
        index: "",
        output: "",
        positionals: [],
        type: CodeTypeCamera,
    };
    for (let i = 0; i < argv.length; i += 1) {
        const arg = argv[i];
        switch (arg) {
            case "-h":
            case "--help":
            case "help":
                parsed.help = true;
                continue;
            case "-o":
            case "--output":
                i += 1;
                parsed.output = requireValue(arg, argv[i]);
                continue;
            case "--fg":
                i += 1;
                parsed.foreground = requireValue(arg, argv[i]);
                continue;
            case "--bg":
                i += 1;
                parsed.background = requireValue(arg, argv[i]);
                continue;
            case "--index":
                i += 1;
                parsed.index = requireValue(arg, argv[i]);
                continue;
            case "--type":
                i += 1;
                parsed.type = requireValue(arg, argv[i]);
                continue;
            default:
                if (arg.startsWith("--output=")) {
                    parsed.output = arg.slice("--output=".length);
                    continue;
                }
                if (arg.startsWith("--fg=")) {
                    parsed.foreground = arg.slice("--fg=".length);
                    continue;
                }
                if (arg.startsWith("--bg=")) {
                    parsed.background = arg.slice("--bg=".length);
                    continue;
                }
                if (arg.startsWith("--index=")) {
                    parsed.index = arg.slice("--index=".length);
                    continue;
                }
                if (arg.startsWith("--type=")) {
                    parsed.type = arg.slice("--type=".length);
                    continue;
                }
                if (arg.startsWith("-")) {
                    throw new Error(`unknown option: ${arg}`);
                }
                parsed.positionals.push(arg);
        }
    }
    return parsed;
}
function requireValue(flag, value) {
    if (value === undefined || value === "") {
        throw new Error(`missing value for ${flag}`);
    }
    return value;
}
function printTemplates() {
    for (const template of templates()) {
        process.stdout.write(`index=${template.index} foreground=${template.foreground.hex()} background=${template.background.hex()} third=${template.third.hex()}\n`);
    }
}
function printUsage() {
    process.stderr.write(`Usage:
  appclipcode <url> [--index N] [--type cam|nfc] [-o FILE]
  appclipcode <url> --fg HEX --bg HEX [--type cam|nfc] [-o FILE]
  appclipcode templates

Examples:
  appclipcode https://example.com > code.svg
  appclipcode https://example.com --index 4 -o code.svg
  appclipcode https://example.com --fg FFFFFF --bg 000000 --type nfc -o code.svg
  appclipcode templates

Options:
  --index N       Built-in template index (0-17). Defaults to 0.
  --fg HEX        Foreground color as 6-digit hex.
  --bg HEX        Background color as 6-digit hex.
  --type TYPE     Code type: cam (default) or nfc.
  -o, --output    Output file path. Defaults to stdout.
  -h, --help      Show this help text.
`);
}
function printError(message) {
    process.stderr.write(`error: ${message}\n`);
}
process.exitCode = main(process.argv.slice(2));
