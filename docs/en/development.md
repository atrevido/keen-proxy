# Documentation development

## Build locally

Use Python 3.12+ and Node 22.12+:

```sh
python3 -m venv .venv-docs
.venv-docs/bin/pip install -r requirements-docs.txt
DOCS_SITE_URL=http://localhost:8097/ .venv-docs/bin/mkdocs build --strict
cd web
npm ci
npm test
npm run build:demo
cd ..
mkdir -p site/demo
cp -R web/dist-demo/. site/demo/
python3 -m http.server 8097 --directory site
```

Open `http://localhost:8097/` for English, `/ru/` for Russian and `/demo/` for the independent demo. The server serves static files only. `npm run build` still builds the normal router UI in `web/dist`; `build:demo` writes to `web/dist-demo`.

## Edit and translate

Maintain matching Markdown filenames under `docs/en/` and `docs/ru/`. Navigation is configured in `mkdocs.yml`; the i18n plugin creates language switching and localized search/navigation. Add screenshots under `docs/assets/screenshots/` in both UI languages. Capture only synthetic sessions and update related captions when screens change.

Demo mode replaces imports of the API module with `web/src/demo/api.ts` through a Vite alias. Its `satisfies` contract catches differences from the normal API. Add behavior tests when extending the adapter; do not add backend networking to simulate a feature.

## GitHub Pages

The **Documentation** workflow builds and checks both outputs on relevant PRs and pushes. It uploads only `site/`, containing generated documentation and the demo. Internal reports, local memories, application source maps, credentials, and Go runtime state are not part of that directory.

Enable GitHub Pages with **GitHub Actions** as its source. Changes to the docs or demo on `main` are published after the **Documentation** build succeeds. To republish on demand, run **Documentation** on `main` with **Deploy to GitHub Pages** enabled. Pull request runs build a preview artifact. The default site URL is `https://ordo-systems.github.io/keen-proxy/`. A private repository requires a supporting GitHub plan; publishing Pages can make the generated site publicly accessible. Do not change repository visibility merely to enable Pages.

The release workflow and Pages deployment are independent. Follow [CONTRIBUTING](https://github.com/ordo-systems/keen-proxy/blob/main/CONTRIBUTING.md) for versioning and [RELEASING](https://github.com/ordo-systems/keen-proxy/blob/main/RELEASING.md) for binary releases.
