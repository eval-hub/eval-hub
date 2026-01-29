#!/usr/bin/env node
/**
 * Builds index-standalone.html by inlining the OpenAPI spec into index.html.
 * Use the standalone file when opening docs via file:// to avoid CORS (browsers
 * block fetching openapi.yaml from a different origin than the HTML).
 *
 * Requires: openapi.json in this directory (e.g. from redocly bundle --ext json).
 * Run from repo root: make generate-standalone-docs (or generate-public-docs).
 */

const fs = require('fs');
const path = require('path');

const args = process.argv;
const isInternal = args.includes('--internal');
const docsDir = __dirname;
if (isInternal) {
  specPath = path.join(docsDir, 'openapi-internal.json');
  templatePath = path.join(docsDir, 'templates/index.html');
  outPath = path.join(docsDir, 'index-internal.html');
} else {
  specPath = path.join(docsDir, 'openapi.json');
  templatePath = path.join(docsDir, 'templates/index.html');
  outPath = path.join(docsDir, 'index.html');
}

if (!fs.existsSync(specPath)) {
  console.error('docs/build-standalone-html.js: openapi json not found.');
  process.exit(1);
}

const spec = JSON.parse(fs.readFileSync(specPath, 'utf8'));
let html = fs.readFileSync(templatePath, 'utf8');

// Replace url-based loading with inlined spec so file:// works without CORS
const specJson = JSON.stringify(spec);
html = html.replace(
  /url: 'openapi\.yaml',/,
  `spec: ${specJson},`
);

fs.writeFileSync(outPath, html);
console.log('Wrote', outPath);
