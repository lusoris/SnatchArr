# sv

Everything you need to build a Svelte project, powered by [`sv`](https://github.com/sveltejs/cli).

## Creating a project

If you're seeing this, you've probably already done this step. Congrats!

```sh
# create a new project
npx sv create my-app
```

To recreate this project with the same configuration:

```sh
# recreate this project
pnpm dlx sv@0.17.1 create --template minimal --types ts --add prettier eslint vitest="usages:unit" tailwindcss="plugins:none" playwright --install pnpm web
```

## Developing

Once you've created a project and installed dependencies with `npm install` (or `pnpm install` or `yarn`), start a development server:

```sh
npm run dev

# or start the server and open the app in a new browser tab
npm run dev -- --open
```

## Building

To create a production version of your app:

```sh
npm run build
```

You can preview the production build with `npm run preview`.

> To deploy your app, you may need to install an [adapter](https://svelte.dev/docs/kit/adapters) for your target environment.

## Visual baseline

`make web-visual` compares `/login` and `/setup` (en and de, light and dark) with the PNGs in
`e2e/visual`. It runs `e2e/visual.test.ts` inside the Playwright image pinned by version and
digest in `e2e/visual-image.env`, so the pixels never depend on the host's fonts, browsers or
GPU; it needs Docker. Two planted visual changes run first and must fail the comparison.

After an intended change to these pages, run `make web-visual-update`, look at the changed PNGs
in the diff, and commit them with the change. The image version must equal `@playwright/test`
in `pnpm-lock.yaml`; Renovate moves both in one pull request (group `playwright`).

## Dependency overrides and patches

[pnpm-workspace.yaml](pnpm-workspace.yaml) overrides `minimatch@<11.0.1` with `11.0.3`
for GHSA-g3hv-75wg-m249 (Dependabot alert #5). The dependency chain in
[pnpm-lock.yaml](pnpm-lock.yaml) is `openapi-typescript@7.13.0` →
`@redocly/openapi-core@1.34.20`; that Redocly release pins minimatch to `5.1.9` exactly.
The [compatibility patch](patches/@redocly__openapi-core@1.34.20.patch) changes
`lib/utils.js` from `require("minimatch")` to `const { minimatch } = require("minimatch")`
because minimatch 11 has no CommonJS default export.

[redocly-minimatch.test.ts](src/lib/redocly-minimatch.test.ts), run by `make web-verify`
([Makefile](../Makefile)), exercises Redocly's URL header matching and fails with
`minimatch is not a function` without the patch. `make web-gen` alone does not exercise
that path because it reads a local schema. If the patch stops applying, `pnpm install` fails:
`--frozen-lockfile` with `ERR_PNPM_LOCKFILE_CONFIG_MISMATCH`, an unfrozen install with
`ERR_PNPM_PATCH_FAILED`.

Remove this workaround when a `@redocly/openapi-core` release supports minimatch 10 or later:

1. Upgrade the dependency chain to use that release.
2. Remove the patch entry and the minimatch override from `pnpm-workspace.yaml`, delete the
   patch file and its annotation in [REUSE.toml](../REUSE.toml), and refresh `pnpm-lock.yaml`
   with `pnpm install` from `web/`.
3. From the repository root, run `make web-gen` (the generated diff must stay empty), then
   `make web-verify`, including the existing regression test.
