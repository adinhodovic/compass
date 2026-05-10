// build-assets.mjs — single source of truth for the asset pipeline.
//
// Replaces the previous chain of cp commands so adding a vendor lib is
// one line and missing files are hard errors instead of silent half-builds.
//
// Run via `npm run build:js`. Tailwind CSS is still built separately via
// the @tailwindcss/cli (see package.json build:css).

import { copyFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const out = resolve(root, "internal/server/static");
mkdirSync(out, { recursive: true });

// Vendor libs lifted straight from node_modules. Add new entries here when
// pulling in another runtime dependency; the source path is relative to
// node_modules and the destination is just the bare filename.
const vendor = [
  ["alpinejs/dist/cdn.min.js", "alpine.min.js"],
  ["htmx.org/dist/htmx.min.js", "htmx.min.js"],
  ["fuse.js/dist/fuse.min.mjs", "fuse.min.mjs"],
  ["iconify-icon/dist/iconify-icon.min.js", "iconify-icon.min.js"],
  ["mermaid/dist/mermaid.min.js", "mermaid.min.js"],
];

const brand = [
  ["docs/assets/images/compass-logo.svg", "brand/compass-logo.svg"],
  ["docs/assets/images/compass-logo-square.png", "brand/compass-logo-square.png"],
  ["docs/assets/images/favicon.svg", "brand/favicon.svg"],
];

for (const [src, dst] of vendor) {
  const from = resolve(root, "node_modules", src);
  if (!existsSync(from)) {
    console.error(`build-assets: missing ${src}; run \`npm ci\` first`);
    process.exit(1);
  }
  const to = resolve(out, dst);
  mkdirSync(dirname(to), { recursive: true });
  copyFileSync(from, to);
}

for (const [src, dst] of brand) {
  const from = resolve(root, src);
  if (!existsSync(from)) {
    console.error(`build-assets: missing ${src}`);
    process.exit(1);
  }
  const to = resolve(out, dst);
  mkdirSync(dirname(to), { recursive: true });
  copyFileSync(from, to);
}

// app.js is split into focused modules under assets/ that import each
// other via ES modules. esbuild bundles them into a single IIFE so the
// browser still loads one /static/app.js. The entry attaches Alpine
// factories to `window` so `x-data="..."` directives keep working with
// the IIFE-scoped output.
const appJsSrc = resolve(root, "assets/app.js");
const appJsDst = resolve(out, "app.js");
const esbuild = await import("esbuild");
await esbuild.build({
  entryPoints: [appJsSrc],
  bundle: true,
  format: "iife",
  minify: true,
  sourcemap: "external",
  target: "es2022",
  outfile: appJsDst,
  legalComments: "none",
});
