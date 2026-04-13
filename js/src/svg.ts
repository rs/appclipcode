import type { Palette } from "./color.js";
import {
  BG_RADIUS,
  CAMERA_LOGO_PATHS,
  CENTER_X,
  CENTER_Y,
  DEG2RAD,
  PHONE_OUTER_PATH,
  PHONE_SCREEN_PATH,
  RING_BIT_COUNTS,
  RING_GAP_ANGLES,
  RING_RADII,
  RING_ROTATIONS,
  STROKE_WIDTH,
} from "./svg-data.js";

export type CodeType = "cam" | "nfc";

export function renderSVG(bits: boolean[], palette: Palette, url: string, codeType: CodeType): string {
  const lines: string[] = [];
  lines.push('<?xml version="1.0" encoding="utf-8"?>');
  lines.push(`<svg data-design="Fingerprint" data-payload="${escapeXML(url)}" viewBox="0 0 800 800" xmlns="http://www.w3.org/2000/svg">`);
  lines.push("    <title>App Clip Code</title>");
  lines.push(
    `    <circle cx="${formatFloat(CENTER_X)}" cy="${formatFloat(CENTER_Y)}" id="Background" r="${formatFloat(BG_RADIUS)}" style="fill:${palette.background.hex()}"/>`,
  );
  lines.push('    <g id="Markers">');

  const gapBits = bits.slice(0, 128);
  const colorStream = bits.slice(128);
  let gapOffset = 0;
  let colorIndex = 0;

  for (let ring = 0; ring < 5; ring += 1) {
    const count = RING_BIT_COUNTS[ring];
    const ringGap = gapBits.slice(gapOffset, gapOffset + count);
    gapOffset += count;

    const posState = new Array<number>(count).fill(-1);
    for (let i = 0; i < count; i += 1) {
      if (!ringGap[i]) {
        posState[i] = colorIndex < colorStream.length && colorStream[colorIndex] ? 1 : 0;
        colorIndex += 1;
      }
    }

    lines.push(
      `        <g name="ring-${ring + 1}" transform="rotate(${RING_ROTATIONS[ring]} ${CENTER_X.toFixed(0)} ${CENTER_Y.toFixed(0)})">`,
    );
    writeRingArcsFromState(lines, ring, posState, palette);
    lines.push("        </g>");
  }

  lines.push("    </g>");
  writeLogo(lines, codeType, palette);
  lines.push("</svg>");
  return `${lines.join("\n")}\n`;
}

function writeRingArcsFromState(lines: string[], ringIndex: number, posState: number[], palette: Palette): void {
  const count = RING_BIT_COUNTS[ringIndex];
  const radius = RING_RADII[ringIndex];
  const bitAngle = 360.0 / count;
  const gapAngle = RING_GAP_ANGLES[ringIndex];

  const arcs: Array<{ dataColor: number; startBit: number; span: number }> = [];

  for (let i = 0; i < count; i += 1) {
    if (posState[i] === -1) {
      continue;
    }

    let span = 1;
    while (i + span < count && posState[i + span] === -1) {
      span += 1;
    }
    if (i + span === count) {
      for (let j = 0; j < count && posState[j] === -1; j += 1) {
        span += 1;
      }
    }

    arcs.push({ dataColor: posState[i], startBit: i, span });
  }

  for (const arc of arcs) {
    const startAngle = arc.startBit * bitAngle + gapAngle;
    const endAngle = (arc.startBit + arc.span) * bitAngle - gapAngle;

    const sx = CENTER_X + radius * Math.cos(startAngle * DEG2RAD);
    const sy = CENTER_Y + radius * Math.sin(startAngle * DEG2RAD);
    const ex = CENTER_X + radius * Math.cos(endAngle * DEG2RAD);
    const ey = CENTER_Y + radius * Math.sin(endAngle * DEG2RAD);

    let arcSpan = endAngle - startAngle;
    if (arcSpan < 0) {
      arcSpan += 360.0;
    }
    const largeArc = arcSpan > 180.0 ? 1 : 0;
    const strokeColor = arc.dataColor === 1 ? palette.third : palette.foreground;

    lines.push(
      `            <path d="M ${formatFloat(ex)} ${formatFloat(ey)} A ${formatFloat(radius)} ${formatFloat(radius)} 0 ${largeArc} 0 ${formatFloat(sx)} ${formatFloat(sy)}" data-color="${arc.dataColor}" style="fill:none;stroke:${strokeColor.hex()};stroke-linecap:round;stroke-miterlimit:10;stroke-width:${formatFloat(STROKE_WIDTH)}px"/>`,
    );
  }
}

function writeLogo(lines: string[], codeType: CodeType, palette: Palette): void {
  if (codeType === "nfc") {
    lines.push('    <g id="Logo" data-logo-type="phone" transform="translate(293.400000 293.400000) scale(1.980000 1.980000)">');
    lines.push(`        <path id="outer_circle" d="${PHONE_OUTER_PATH}" style="fill:${palette.foreground.hex()}"/>`);
    lines.push(`        <path id="phone_screen" d="${PHONE_SCREEN_PATH}" style="fill:${palette.third.hex()};isolation:isolate"/>`);
    lines.push("    </g>");
    return;
  }

  lines.push('    <g id="Logo" data-logo-type="Camera" transform="translate(293.275699 293.275699) scale(1.874000 1.874000)">');
  for (const path of CAMERA_LOGO_PATHS) {
    lines.push(`        <path d="${path}" style="fill:${palette.foreground.hex()}"/>`);
  }
  lines.push("    </g>");
}

function formatFloat(value: number): string {
  return value.toFixed(6);
}

function escapeXML(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}
