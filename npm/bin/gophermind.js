#!/usr/bin/env node
'use strict';

// Thin launcher: exec the platform binary that postinstall downloaded into
// vendor/. Forwards argv, stdio, and exit code so `npx gophermind` / a globally
// installed `gophermind` behave exactly like the native binary.
const path = require('path');
const { spawnSync } = require('child_process');

const binName = process.platform === 'win32' ? 'gophermind.exe' : 'gophermind';
const bin = path.join(__dirname, '..', 'vendor', binName);

const res = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });

if (res.error) {
  if (res.error.code === 'ENOENT') {
    console.error(
      'gophermind: binary not found — the postinstall download may have failed.\n' +
        'Reinstall with:  npm rebuild gophermind   (or reinstall the package)'
    );
  } else {
    console.error('gophermind: ' + res.error.message);
  }
  process.exit(1);
}

process.exit(res.status === null ? 1 : res.status);
