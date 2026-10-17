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
