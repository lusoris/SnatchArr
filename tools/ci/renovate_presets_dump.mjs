// SPDX-FileCopyrightText: 2026 lusoris <lusoris@pm.me>
// SPDX-License-Identifier: EUPL-1.2
//
// Offline Renovate dist probe run inside the digest-pinned Renovate container image:
//   docker run --rm --network none --read-only -v .../renovate_presets_dump.mjs:/probe/dump.mjs:ro \
//     --entrypoint node <RENOVATE_IMAGE> /probe/dump.mjs
//
// Reads built-in preset groups, Docker-capable built-in manager names and dist files emitting
// non-Docker depTypes ("github-runner", "php2shell-runtime"), then writes one JSON object to
// stdout. Iterative bounded walk (no recursion), no network, no writes.

import fs from 'node:fs';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const DEFAULT_RENOVATE_ROOT = '/usr/local/renovate';
const MAX_DIST_ENTRIES = 20000;
const MAX_FILE_BYTES = 8 * 1024 * 1024;
const TRACKED_DEP_TYPES = ['github-runner', 'php2shell-runtime'];
const DOCKER_IMPORT_RE =
  /from "(?:\.\.\/)+(?:datasource\/docker\/[^"]*|manager\/dockerfile\/extract\.js|dockerfile\/extract\.js)"/;

/** Read a UTF-8 file after checking that its size does not exceed MAX_FILE_BYTES. */
function readBoundedText(filePath) {
  const st = fs.lstatSync(filePath);
  if (!st.isFile()) {
    throw new Error(`${filePath}: expected regular file`);
  }
  if (st.size > MAX_FILE_BYTES) {
    throw new Error(`${filePath}: ${st.size} bytes exceeds cap ${MAX_FILE_BYTES}`);
  }
  return fs.readFileSync(filePath, 'utf8');
}

/** Iteratively list every .js file under distRoot as sorted relative POSIX paths. */
function listDistJsFiles(distRoot) {
  const stack = [''];
  const jsFiles = [];
  let seenEntries = 0;

  while (stack.length > 0) {
    const relDir = stack.pop();
    const absDir = relDir === '' ? distRoot : path.join(distRoot, relDir);
    const entries = fs.readdirSync(absDir, { withFileTypes: true });
    entries.sort((a, b) => (a.name < b.name ? 1 : a.name > b.name ? -1 : 0));

    for (const entry of entries) {
      seenEntries += 1;
      if (seenEntries > MAX_DIST_ENTRIES) {
        throw new Error(`dist walk exceeded ${MAX_DIST_ENTRIES} entries`);
      }
      const childRel = relDir === '' ? entry.name : `${relDir}/${entry.name}`;
      if (entry.isDirectory()) {
        stack.push(childRel);
      } else if (entry.isFile() && entry.name.endsWith('.js')) {
        jsFiles.push(childRel);
      }
    }
  }

  jsFiles.sort();
  return jsFiles;
}

/** Scan dist .js files for Docker-importing managers and tracked depType string literals. */
function scanDistSources(distRoot, jsFiles) {
  const importingManagers = new Set();
  const depTypeEmitters = {
    'github-runner': [],
    'php2shell-runtime': [],
  };

  for (const rel of jsFiles) {
    const text = readBoundedText(path.join(distRoot, rel));
    for (const depType of TRACKED_DEP_TYPES) {
      if (text.includes(`"${depType}"`)) {
        depTypeEmitters[depType].push(rel);
      }
    }
    if (rel.startsWith('modules/manager/')) {
      const rest = rel.slice('modules/manager/'.length);
      const slash = rest.indexOf('/');
      if (slash > 0 && DOCKER_IMPORT_RE.test(text)) {
        importingManagers.add(rest.slice(0, slash));
      }
    }
  }
  return { importingManagers, depTypeEmitters };
}

/** Collect sorted built-in manager names that support Docker or import Docker/Dockerfile code. */
function collectDockerManagers(apiMap, importingManagers) {
  if (!(apiMap instanceof Map) || apiMap.size === 0) {
    throw new Error('dist/modules/manager/api.js default export is not a non-empty Map');
  }
  const names = [];
  for (const [name, mod] of apiMap.entries()) {
    if (typeof name !== 'string' || name.length === 0) {
      throw new Error('manager api Map contains a non-string key');
    }
    const datasources = mod?.supportedDatasources;
    const hasDockerDatasource = Array.isArray(datasources) && datasources.includes('docker');
    if (hasDockerDatasource || importingManagers.has(name)) {
      names.push(name);
    }
  }
  names.sort();
  return names;
}

/** Load Renovate dist metadata and print the single JSON payload to stdout. */
async function main() {
  const root = process.env.RENOVATE_ROOT || DEFAULT_RENOVATE_ROOT;
  const pkg = JSON.parse(readBoundedText(path.join(root, 'package.json')));
  if (typeof pkg?.version !== 'string' || pkg.version.length === 0) {
    throw new Error('package.json lacks a string version');
  }

  const distRoot = path.join(root, 'dist');
  const presetsUrl = pathToFileURL(path.join(distRoot, 'config/presets/internal/index.js')).href;
  const managerApiUrl = pathToFileURL(path.join(distRoot, 'modules/manager/api.js')).href;
  const presetsMod = await import(presetsUrl);
  const managerApiMod = await import(managerApiUrl);

  const groups = presetsMod?.groups;
  if (typeof groups !== 'object' || groups === null || Array.isArray(groups)) {
    throw new Error('config/presets/internal/index.js lacks a groups object');
  }

  const jsFiles = listDistJsFiles(distRoot);
  const { importingManagers, depTypeEmitters } = scanDistSources(distRoot, jsFiles);
  const dockerManagers = collectDockerManagers(managerApiMod?.default, importingManagers);

  const payload = {
    renovateVersion: pkg.version,
    node: process.version,
    groups,
    dockerManagers,
    depTypeEmitters,
  };
  process.stdout.write(`${JSON.stringify(payload)}\n`);
}

main().catch((err) => {
  const msg = err instanceof Error ? err.message : String(err);
  process.stderr.write(`renovate_presets_dump: error: ${msg}\n`);
  process.exit(1);
});
