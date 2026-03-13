#!/usr/bin/env node
// Updates the integrity hash in ui/package-lock.json for @gocodealone/workflow-editor
// after a fresh local build, so that npm ci can verify the tarball.
// Usage (from any directory): node .github/scripts/update-workflow-editor-hash.js

const fs = require('fs');
const crypto = require('crypto');
const path = require('path');

// Resolve paths relative to this script's location (repo root .github/scripts/)
const repoRoot = path.resolve(__dirname, '../..');
const tgzPath = path.join(repoRoot, '..', 'workflow-editor', 'gocodealone-workflow-editor-0.1.0.tgz');
const lockPath = path.join(repoRoot, 'ui', 'package-lock.json');

if (!fs.existsSync(tgzPath)) {
  console.error(`ERROR: tgz not found at ${tgzPath}`);
  process.exit(1);
}

const tgz = fs.readFileSync(tgzPath);
const hash = 'sha512-' + crypto.createHash('sha512').update(tgz).digest('base64');
const lock = JSON.parse(fs.readFileSync(lockPath, 'utf8'));
const pkg = lock.packages['node_modules/@gocodealone/workflow-editor'];
if (!pkg) {
  console.error('ERROR: @gocodealone/workflow-editor not found in package-lock.json');
  process.exit(1);
}
const oldHash = pkg.integrity;
pkg.integrity = hash;
fs.writeFileSync(lockPath, JSON.stringify(lock, null, 2) + '\n');
console.log(`Updated @gocodealone/workflow-editor integrity: ${oldHash} -> ${hash}`);
