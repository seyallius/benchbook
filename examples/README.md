# Example project layout

A copy-ready skeleton of a repository that uses `benchbook`. Nothing here is required by the tool — `benchbook init`
writes the config for you — but this is the layout the documentation assumes, and the one the FAQ keeps referring to.

```console
$ cp examples/benchbook.toml ~/code/myproject/benchbook.toml
$ mkdir -p ~/code/myproject/docs/content
$ cp examples/docs/content/home.md ~/code/myproject/docs/content/home.md
$ cd ~/code/myproject && benchbook run && benchbook milestone -m "baseline"
```

## What each file is

| Path                   | Who owns it | Notes                                                        |
|------------------------|-------------|--------------------------------------------------------------|
| `benchbook.toml`       | you         | the only configuration file; every key is documented inline  |
| `docs/content/home.md` | you         | an authored page; `benchbook` reads it and never rewrites it |
| `justfile`             | you         | the recipes from the `init` snippet, ready to keep           |

After the first `run`, `benchbook` creates and owns these (commit all of it except
`raw/`):

```
benchbook/
  current.json                  # the working run: overwritten by every `run`
  history/m001-baseline.json    # milestones: immutable, never rewritten
  raw/                          # untouched go test / pprof output (gitignored)

docs/
  index.html, manifest.json     # the shell, embedded in the binary
  assets/benchbook/             # css, app.js, compare.js, vendored libraries
  content/                      # generated pages (DO NOT EDIT) + your authored pages
  data/                         # the exported JSON the compare page reads
```

## The full loop

```console
$ benchbook init --name myproject     # or copy benchbook.toml from here
$ benchbook run                       # record current.json, rebuild the site
$ benchbook profile                   # cpu + heap tables, attached to that run
$ benchbook milestone -m "baseline"   # m001 — the point everything is compared with
# ... change code ...
$ benchbook run                       # record again
$ benchbook status                    # what changed, and whether it is real
$ benchbook profile                   # profile the run you are about to promote
$ benchbook milestone -m "what I did" # promote it, with a label that means something
$ git add benchbook docs && git commit -m "benchmarks: what I did"
```

Publish the site by pointing GitHub Pages at the `docs/` folder on `main`.
