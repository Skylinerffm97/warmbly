# site

Warmbly's public marketing site. Astro 5 + Tailwind v4, separate from the
in-product dashboard at `web/` and the admin app at `admin/`.

## Run it locally

```sh
pnpm install
pnpm dev          # boots on http://localhost:4321
pnpm build        # static output into ./dist
pnpm preview      # serve the production build locally
```

From the repo root you can also use `make site`, which is a shortcut for
`cd site && pnpm dev`. `make app` does **not** start this site — it lives
outside the docker compose stack and ships on its own cadence.

## The installer

`public/install.sh` is the one-command self-host installer, served verbatim at
`https://warmbly.com/install.sh`. It is a static asset, so what ships is exactly
what a `curl | sh` executes.

Two things the host has to get right:

- serve it as `text/plain` (or `application/x-sh`), never `text/html`, so
  reading it in a browser works and nothing tries to render it
- serve `public/install.sh.sha256` alongside it, unmodified

Editing it means regenerating the checksum, and CI fails when the two disagree:

```sh
make installer-sha      # regenerate site/public/install.sh.sha256
make installer-check    # everything CI runs: POSIX parse, shellcheck, dry runs
make installer-demo     # walk the wizard; installs nothing, needs no Docker
```

`make installer-demo` is the fastest way to see a UI change: it runs the real
questions and the real review with the pull, the container creation and the
health wait played, and writes nothing anywhere. It takes about half a minute;
`WARMBLY_DEMO_FAST=1 make installer-demo` cuts that to under ten seconds while
you iterate.

## Layout

```
site/
├── astro.config.mjs   # sitemap integration + Tailwind v4 Vite plugin
├── public/            # static assets served as-is
├── scripts/           # build-time helpers
└── src/               # pages, layouts, components, content
```
