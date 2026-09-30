# Vendored assets

Everything in this directory is embedded into the `benchbook` binary with `go:embed`.
Nothing is fetched at runtime: the generated documentation site works offline, on an
air-gapped machine and on GitHub Pages without a CDN, and a CDN cannot break it by
changing a URL or a version.

The files below are the exact artefacts published by the upstream projects. Run
`scripts/vendor.sh` to re-download them and verify the checksums; the script fails
loudly when a checksum does not match.

| Path | Package | Pinned version | SHA-256 |
| --- | --- | --- | --- |
| `vendor/marked.min.js` | [marked](https://github.com/markedjs/marked) | `15.0.7` | `934e3e36e9e2da0afb1a6e75075bb0f09af05293a844e84a7477ef40911c349a` |
| `vendor/highlight.min.js` | [highlight.js](https://github.com/highlightjs/highlight.js) (common bundle) | `11.11.1` | `c4a399dd6f488bc97a3546e3476747b3e714c99c57b9473154c6fb8d259b9381` |
| `vendor/languages/go.min.js` | highlight.js language pack (Go) | `11.11.1` | `8d9f9a7dcd604438ac3530ad85597db9329923488b8a7ccc711df551f6f37799` |
| `vendor/styles/github.min.css` | highlight.js theme (light) | `11.11.1` | `3a9a5def8b9c311e5ae43abde85c63133185eed4f0d9f67fea4b00a8308cf066` |
| `vendor/styles/github-dark.min.css` | highlight.js theme (dark) | `11.11.1` | `9f208d022102b1d0c7aebfecd8e42ca7997d5de636649d2b31ea63093d809019` |

## Why the "common" bundle

`vendor/highlight.min.js` is the upstream `build/highlight.min.js` bundle, which
already registers the common grammars: `go`, `bash`/`shell`, `json`, `toml` (through
`ini`), `diff`, `yaml`, `sql` and about twenty more. `vendor/languages/go.min.js` is
vendored explicitly as well, because Go is the language this tool exists for and a
future bundle change should never be able to remove it silently.

Extra languages can be added through `[site].highlight_langs`. The shell requests
`assets/benchbook/vendor/languages/<lang>.min.js` for every configured language and
ignores a missing file, so adding a grammar is a matter of dropping the pinned file
into `vendor/languages/` and re-running `scripts/vendor.sh --verify`. Languages that
are part of the highlighter core need no file at all.

At the time of vendoring, highlight.js `11.11.1` publishes no standalone
`languages/toml.min.js`: TOML support lives in the `ini` grammar inside the core
bundle, so no file is vendored for it.

## Licence

Both projects are used unmodified and keep their own licences, reproduced by the
banner comment at the top of each file: marked is MIT, highlight.js is BSD-3-Clause.
