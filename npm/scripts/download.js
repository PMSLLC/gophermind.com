#!/usr/bin/env node
'use strict';

// Postinstall: download the gophermind binary for this platform/arch from the
// matching GitHub Release and extract it into vendor/. The release asset names
// are produced by GoReleaser (see .goreleaser.yaml archives.name_template).
//
// Env:
//   GOPHERMIND_SKIP_DOWNLOAD=1   skip (e.g. CI that doesn't need the binary)
//   GOPHERMIND_DOWNLOAD_BASE=url override the release base URL (testing/mirrors)

const fs = require('fs');
const os = require('os');
const path = require('path');
const https = require('https');
const { execFileSync } = require('child_process');

const REPO = 'jbrahy/gophermind.com';
const version = require('../package.json').version;

if (process.env.GOPHERMIND_SKIP_DOWNLOAD) {
  console.log('gophermind: GOPHERMIND_SKIP_DOWNLOAD set — skipping binary download.');
  process.exit(0);
}

function assetName() {
  const v = version;
  const arch = process.arch === 'arm64' ? 'arm64' : process.arch === 'x64' ? 'amd64' : null;
  switch (process.platform) {
    case 'darwin':
      return `gophermind_${v}_darwin_all.tar.gz`; // universal (arm64 + x64)
    case 'linux':
      return arch && `gophermind_${v}_linux_${arch}.tar.gz`;
    case 'win32':
      return arch && `gophermind_${v}_windows_${arch}.zip`;
    default:
      return null;
  }
}

function fail(msg) {
  console.error('gophermind: ' + msg);
  process.exit(1);
}

function download(url, dest, redirects) {
  redirects = redirects || 0;
  if (redirects > 10) return fail('too many redirects fetching ' + url);
  return new Promise((resolve, reject) => {
    https
      .get(url, { headers: { 'User-Agent': 'gophermind-npm' } }, (res) => {
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          res.resume();
          return resolve(download(res.headers.location, dest, redirects + 1));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error('HTTP ' + res.statusCode + ' for ' + url));
        }
        const out = fs.createWriteStream(dest);
        res.pipe(out);
        out.on('finish', () => out.close(resolve));
        out.on('error', reject);
      })
      .on('error', reject);
  });
}

async function main() {
  const asset = assetName();
  if (!asset) {
    fail(`unsupported platform/arch: ${process.platform}/${process.arch}`);
  }
  const base =
    process.env.GOPHERMIND_DOWNLOAD_BASE ||
    `https://github.com/${REPO}/releases/download/v${version}`;
  const url = `${base}/${asset}`;

  const vendor = path.join(__dirname, '..', 'vendor');
  fs.mkdirSync(vendor, { recursive: true });
  const tmp = path.join(os.tmpdir(), asset);

  console.log(`gophermind: downloading ${asset} ...`);
  try {
    await download(url, tmp);
  } catch (e) {
    fail(`download failed (${e.message}).\n  URL: ${url}`);
  }

  // `tar` extracts both .tar.gz (macOS/Linux) and .zip (bsdtar on Windows 10+).
  try {
    execFileSync('tar', ['-xf', tmp, '-C', vendor], { stdio: 'inherit' });
  } catch (e) {
    fail('extract failed: ' + e.message);
  } finally {
    try { fs.unlinkSync(tmp); } catch (_) {}
  }

  const binName = process.platform === 'win32' ? 'gophermind.exe' : 'gophermind';
  const bin = path.join(vendor, binName);
  if (!fs.existsSync(bin)) {
    fail('binary not found in archive after extraction: ' + binName);
  }
  if (process.platform !== 'win32') {
    fs.chmodSync(bin, 0o755);
  }
  console.log('gophermind: installed ' + binName);
}

main();
