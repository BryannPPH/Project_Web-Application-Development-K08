// Builds every Senja rig output from two sources: senja-rig.svg (the drawing) and
// scripts/senja-motion.js (the animations).
//   1. parts/<layer>.svg   one standalone, transparent SVG per movable layer, all in the
//                          master's coordinate space (stack them with the same viewBox).
//   2. hyperframes/assets/parts/   a copy of parts/ (imported by the "layers" scene).
//   3. hyperframes/compositions/<anim>.html   one reel scene per animation (rig + motion inlined).
//   4. hyperframes/loops/<anim>.html   standalone 1080x1080 transparent roots, one loop each,
//      for exporting: npx hyperframes render -c loops/wave.html --format webm
//   5. hyperframes/index.html   the preview reel: neutral, layers, then every animation.
//   6. web/senja-data.ts   the rig SVG + every animation recorded as keyframe data, played on the
//      website by src/components/senja-player.ts (same motion as HyperFrames, no GSAP needed).
//   Hand-written scenes keep the rig between <!-- senja-rig:start prefix="x" --> ... end markers.
//   Every inlined id is prefixed so several scenes can each hold a rig without id collisions.
// Usage: node scripts/build-rig.mjs   (no dependencies)
import { readFileSync, writeFileSync, readdirSync, mkdirSync, copyFileSync } from "node:fs";
import { dirname, join } from "node:path";
import vm from "node:vm";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const hf = join(root, "hyperframes");
const master = readFileSync(join(root, "senja-rig.svg"), "utf8");
const motion = readFileSync(join(root, "scripts", "senja-motion.js"), "utf8").trim();
const viewBox = master.match(/viewBox="([^"]+)"/)[1];
const [, , vbW, vbH] = viewBox.split(/\s+/).map(Number);

// ---------- parts ----------

// Locate <g id="..."> ... matching </g> by counting nested groups.
function findGroup(src, id) {
  const start = src.indexOf(`<g id="${id}"`);
  if (start < 0) throw new Error(`group #${id} not found`);
  const re = /<g\b[^>]*?(\/?)>|<\/g>/g;
  re.lastIndex = start;
  let depth = 0;
  for (let m; (m = re.exec(src)); ) {
    if (m[0] === "</g>") depth--;
    else if (!m[1]) depth++;
    if (depth === 0) return { start, end: m.index + m[0].length };
  }
  throw new Error(`unclosed group #${id}`);
}
const group = (src, id) => {
  const { start, end } = findGroup(src, id);
  return src.slice(start, end);
};
const without = (src, ids) =>
  ids.reduce((s, id) => {
    const { start, end } = findGroup(s, id);
    return s.slice(0, start) + s.slice(end);
  }, src);

// Layer -> child layers exported as their own files (removed from this one).
const PARTS = {
  tail: [],
  "leg-l": [],
  "leg-r": [],
  body: ["outfit-robe"],
  "scarf-ends": [],
  "arm-l": ["paw-l", "sleeve-l"],
  "paw-l": [],
  "arm-r": ["paw-r", "sleeve-r"],
  "paw-r": ["prop-lantern", "prop-iced-coffee"],
  "prop-lantern": [],
  scarf: [],
  "ear-l": [],
  "ear-r": [],
  head: ["ear-l", "ear-r", "eye-l", "eye-r", "brows", "mouth", "outfit-hat", "outfit-nightcap"],
  // Outfits per time of day (data-outfit="pagi|siang|sore|malam"; sore = the lantern).
  "prop-mug": [],
  "outfit-hat": [],
  "prop-iced-coffee": [],
  "outfit-nightcap": [],
  "outfit-robe": [],
  "sleeve-l": [],
  "sleeve-r": [],
  "prop-pillow": [],
  "eye-l": [],
  "eye-r": [],
  brows: [],
  mouth: [],
};

const defs = master.match(/<defs>[\s\S]*?<\/defs>/)[0];
const clipDefs = Object.fromEntries(
  [...defs.matchAll(/<clipPath id="([^"]+)">[\s\S]*?<\/clipPath>/g)].map((m) => [m[1], m[0]]),
);

const partsDir = join(root, "parts");
const hfPartsDir = join(hf, "assets", "parts");
mkdirSync(partsDir, { recursive: true });
mkdirSync(hfPartsDir, { recursive: true });

for (const [id, exclude] of Object.entries(PARTS)) {
  const body = without(group(master, id), exclude)
    .replace(/\n\s*\n/g, "\n")
    .replace(/^(<g id="[^"]+"[^>]*?) opacity="0"/, "$1"); // a part file shows its own layer (only its root)
  const used = [...body.matchAll(/url\(#([^)]+)\)/g)].map((m) => m[1]);
  const partDefs = used.length
    ? `  <defs>\n    ${[...new Set(used)].map((c) => clipDefs[c]).join("\n    ")}\n  </defs>\n`
    : "";
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${viewBox}" width="${vbW}" height="${vbH}">\n` +
    `  <!-- Senja rig layer "${id}". Generated from senja-rig.svg by scripts/build-rig.mjs; edit the master. -->\n` +
    partDefs +
    `  ${body.trim()}\n</svg>\n`;
  writeFileSync(join(partsDir, `${id}.svg`), svg);
  writeFileSync(join(hfPartsDir, `${id}.svg`), svg);
}
copyFileSync(join(root, "senja-rig.svg"), join(hf, "assets", "senja-rig.svg"));

// ---------- inline rig ----------

const inlineRig = (prefix) =>
  master
    .replace(/<!--[\s\S]*?-->\s*/g, "")
    .replace(/\bid="([^"]+)"/g, `id="${prefix}-$1"`)
    .replace(/url\(#([^)]+)\)/g, `url(#${prefix}-$1)`)
    .replace(/ width="\d+" height="\d+"/, "")
    .trim();

const STAGE_CSS = (id, h) => {
  const w = +((h * vbW) / vbH).toFixed(2);
  return `#${id} {
      position: absolute;
      left: 50%;
      top: 50%;
      width: ${w}px;
      height: ${h}px;
      margin: ${-h / 2}px 0 0 ${-w / 2}px;
    }
    #${id} svg {
      width: 100%;
      height: 100%;
      overflow: visible;
    }`;
};

// ---------- animations ----------

// Animation -> cycles shown in the reel, cycles in the exported loop, outfit worn in the preview.
// Lengths live in senja-motion.js.
const ANIMS = {
  idle: { reel: 2, loop: 1, outfit: "sore" },
  wave: { reel: 2, loop: 1, outfit: "siang" },
  thinking: { reel: 2, loop: 1, outfit: "sore" },
  happy: { reel: 3, loop: 1, outfit: "sore" },
  walk: { reel: 4, loop: 2, outfit: "sore" },
  tail: { reel: 2, loop: 1, outfit: "sore" },
  stretch: { reel: 2, loop: 1, outfit: "pagi" },
  sleepy: { reel: 2, loop: 1, outfit: "malam" },
};
// Only the chosen outfit's layers show (the website does the same in senja-rig.css).
const OUTFIT_CSS = (scope, outfit) =>
  `${scope} [data-outfit] { display: none; }
    ${scope} [data-outfit="${outfit}"] { display: inline; }`;
const LENGTH = Object.fromEntries(
  motion
    .match(/LENGTH = \{([^}]+)\}/)[1]
    .split(",")
    .map((kv) => kv.split(":").map((x) => x.trim()))
    .map(([k, v]) => [k, Number(v)]),
);

mkdirSync(join(hf, "loops"), { recursive: true });
for (const [name, { reel, loop, outfit }] of Object.entries(ANIMS)) {
  const scene = `<template>
  <!-- Generated by scripts/build-rig.mjs: edit senja-rig.svg / scripts/senja-motion.js, then rebuild. -->
  <style>
    #root {
      position: absolute;
      inset: 0;
      background: #f7f1ea;
    }
    ${STAGE_CSS(`${name}-stage`, 860)}
    ${OUTFIT_CSS(`#${name}-stage`, outfit)}
  </style>
  <div id="root" data-composition-id="${name}" data-width="1920" data-height="1080">
    <div id="${name}-stage">
${inlineRig(name)}
    </div>
  </div>
  <script>
${motion}
    window.__timelines["${name}"] = SenjaMotion.build("${name}", "${name}", ${reel});
  </script>
</template>
`;
  writeFileSync(join(hf, "compositions", `${name}.html`), scene);

  const dur = +(LENGTH[name] * loop).toFixed(3);
  const standalone = `<!doctype html>
<!-- Generated by scripts/build-rig.mjs. Transparent, seamless loop of Senja "${name}" (${dur} s).
     Export: npx hyperframes render -c loops/${name}.html --format webm -o ../renders/senja-${name}.webm -->
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=1080, height=1080" />
    <script src="https://cdn.jsdelivr.net/npm/gsap@3.15.0/dist/gsap.min.js"></script>
    <style>
      * { margin: 0; padding: 0; box-sizing: border-box; }
      html, body { width: 1080px; height: 1080px; overflow: hidden; background: transparent; }
      #root { position: relative; width: 100%; height: 100%; }
      ${STAGE_CSS("stage", 860)}
      ${OUTFIT_CSS("#stage", outfit)}
    </style>
  </head>
  <body>
    <div id="root" data-composition-id="senja-${name}" data-start="0" data-duration="${dur}" data-width="1080" data-height="1080">
      <div id="stage">
${inlineRig(name)}
      </div>
    </div>
    <script>
${motion}
      window.__timelines["senja-${name}"] = SenjaMotion.build("${name}", "${name}", ${loop});
    </script>
  </body>
</html>
`;
  writeFileSync(join(hf, "loops", `${name}.html`), standalone);
}

// ---------- preview reel ----------

const scenes = [["neutral", 3], ["layers", 6], ...Object.entries(ANIMS).map(([n, a]) => [n, +(LENGTH[n] * a.reel).toFixed(3)])];
let at = 0;
const hosts = scenes
  .map(([id, d]) => {
    const host = `      <div
        id="${id}"
        data-composition-id="${id}"
        data-composition-src="compositions/${id}.html"
        data-start="${+at.toFixed(3)}"
        data-duration="${d}"
        data-track-index="0"
        data-track-kind="graphics"
        data-width="1920"
        data-height="1080"
      ></div>`;
    at += d;
    return host;
  })
  .join("\n");
const indexPath = join(hf, "index.html");
writeFileSync(
  indexPath,
  readFileSync(indexPath, "utf8")
    .replace(/(data-composition-id="main"[\s\S]*?data-duration=")[^"]+"/, `$1${+at.toFixed(3)}"`)
    .replace(/(data-height="1080"\r?\n    >\r?\n)[\s\S]*?(\r?\n    <\/div>\r?\n    <script>)/, `$1${hosts}$2`),
);

// ---------- website data ----------

// Run senja-motion.js against a recording timeline (no browser, no GSAP) and keep every
// tween as [start, duration, layerId, props, ease]. Origins come from data-pivot / svgOrigin.
const pivotOf = Object.fromEntries(
  [...master.matchAll(/<g id="([^"]+)"[^>]*?data-pivot="([^"]+)"/g)].map((m) => [m[1], m[2]]),
);
const origins = Object.fromEntries(Object.entries(pivotOf).map(([id, p]) => [id, p.split(/\s+/).map(Number)]));

function record(name) {
  const P = "rec";
  const strip = (id) => id.trim().replace(/^#?rec-/, "");
  const el = (id) => ({ id: `${P}-${id}`, dataset: pivotOf[id] ? { pivot: pivotOf[id] } : {} });
  const ids = (targets) =>
    (Array.isArray(targets) ? targets : [targets]).flatMap((x) =>
      typeof x === "string" ? x.split(",").map(strip) : x && x.id ? [strip(x.id)] : [],
    );
  const ops = [];
  const push = (kind, targets, vars, pos) => {
    const { duration = 0, ease = "none", svgOrigin, ...props } = vars;
    for (const id of ids(targets)) {
      if (svgOrigin && !origins[id]) origins[id] = String(svgOrigin).split(/\s+/).map(Number);
      if (Object.keys(props).length === 0) continue;
      for (const v of Object.values(props)) {
        if (typeof v !== "number") throw new Error(`${name}: non-numeric value on #${id}`);
      }
      ops.push([+pos.toFixed(4), kind === "set" ? 0 : +duration.toFixed(4), id, props, kind === "set" ? "none" : ease]);
    }
  };
  const tl = {
    to: (t, v, pos) => (push("to", t, v, pos), tl),
    set: (t, v, pos) => (push("set", t, v, pos), tl),
  };
  const sandbox = {
    window: {},
    document: { querySelector: (sel) => el(strip(sel)), querySelectorAll: () => Object.keys(pivotOf).map(el) },
    gsap: { set() {}, timeline: () => tl },
  };
  vm.runInNewContext(motion, sandbox);
  sandbox.window.SenjaMotion.build(name, P, 1);
  return { length: LENGTH[name], ops };
}

const clips = Object.fromEntries(Object.keys(ANIMS).map((n) => [n, record(n)]));
const webSvg = master
  .replace(/<!--[\s\S]*?-->\s*/g, "")
  .replace(/\bid="([^"]+)"/g, 'id="__SENJA__-$1"')
  .replace(/url\(#([^)]+)\)/g, "url(#__SENJA__-$1)")
  .replace(/ width="\d+" height="\d+"/, "")
  .replace(/>\s+</g, "><")
  .trim();
const clipNames = Object.keys(ANIMS).map((n) => `'${n}'`).join(" | ");
mkdirSync(join(root, "web"), { recursive: true });
writeFileSync(
  join(root, "web", "senja-data.ts"),
  `// GENERATED by assets/brand/senja-rig/scripts/build-rig.mjs. Jangan diedit manual:
// ubah senja-rig.svg atau scripts/senja-motion.js, lalu jalankan ulang build-rig.mjs.
// Isi: SVG rig Senja (id diawali __SENJA__-) dan setiap animasi sebagai data keyframe
// yang diputar oleh src/components/senja-player.ts.

export type SenjaClipName = ${clipNames}

/** [mulai (detik), durasi (detik), id layer, properti tujuan, easing] */
export type SenjaOp = readonly [number, number, string, Readonly<Record<string, number>>, string]

export interface SenjaClip {
  readonly length: number
  readonly ops: readonly SenjaOp[]
}

export const SENJA_ORIGINS: Readonly<Record<string, readonly [number, number]>> = ${JSON.stringify(origins)}

export const SENJA_CLIPS: Readonly<Record<SenjaClipName, SenjaClip>> = ${JSON.stringify(clips)}

export const SENJA_SVG = ${JSON.stringify(webSvg)}
`,
);

// ---------- hand-written scenes ----------

const compDir = join(hf, "compositions");
const marker = /<!-- senja-rig:start prefix="([^"]+)" -->[\s\S]*?<!-- senja-rig:end -->/g;
let injected = 0;
for (const file of readdirSync(compDir).filter((f) => f.endsWith(".html"))) {
  const path = join(compDir, file);
  const html = readFileSync(path, "utf8");
  const next = html.replace(marker, (_, prefix) => {
    injected++;
    return `<!-- senja-rig:start prefix="${prefix}" -->\n${inlineRig(prefix)}\n<!-- senja-rig:end -->`;
  });
  if (next !== html) writeFileSync(path, next);
}
console.log(
  `parts: ${Object.keys(PARTS).length}, hand-written rigs: ${injected}, animations: ${Object.keys(ANIMS).join(", ")}, reel: ${+at.toFixed(3)} s`,
);
