#!/usr/bin/env node
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const platform = `${process.platform}-${process.arch}`;
const packageName = `@nodryl/${platform}`;
let binary;
try {
  binary = require.resolve(`${packageName}/bin/nodryl`);
} catch {
  const local = path.resolve(__dirname, '..', 'dist', 'nodryl');
  if (fs.existsSync(local)) binary = local;
}
if (!binary) {
  console.error(`Nodryl has no binary for ${platform}. Supported: darwin/linux on x64/arm64.`);
  process.exit(1);
}

const child = spawnSync(binary, process.argv.slice(2), {
  stdio: 'inherit',
  env: { ...process.env, NODRYL_HELPER_PATH: path.join(__dirname, 'preload.cjs') },
});
if (child.error) {
  console.error(`Nodryl could not start: ${child.error.message}`);
  process.exit(1);
}
if (child.signal) {
  process.kill(process.pid, child.signal);
} else {
  process.exit(child.status === null ? 1 : child.status);
}
