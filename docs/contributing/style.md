# Documentation Style Guide

Conventions for the pages under `docs/`. The goal is a page a reader can act on, not a page that covers everything.

## Writing

- Lead with what the reader needs. Background comes after, or not at all.
- Cut anything that survives deletion without changing the meaning.
- Prefer a table or a code block over a paragraph describing the same thing.
- Explain a *why* only when it is not obvious and still true — a security trade-off, an upstream bug, a counter-intuitive constraint. Never document what the code used to do.
- Address the reader as "you"; call the software "Argo Watcher".

## Terminology

- **Argo Watcher** — the product. Two words, both capitalized, never hyphenated. `argo-watcher` only when naming the binary, image, or Helm release.
- **Argo CD** — two words, no hyphen.
- **GitOps** — capital G and O, no hyphen.
- **Web UI** — not "web interface" or "the frontend".
- **deploy token** — lowercase in prose; `ARGO_WATCHER_DEPLOY_TOKEN` when naming the variable.

## Formatting

- **Bullets** use `-`.
- **Dashes** in prose are em dashes (`—`), never `--`.
- **Headings**: `#` for the page title, `##` for sections, `###` below that. Short and descriptive — headings are anchors other pages link to, so renaming one means updating those links.
- **Tables**: left-align text, right-align numbers (`|---:|`), skip centre alignment.
- **Code**: always tag the language on a fenced block. Backtick variables, paths, headers and commands inline.

## Admonitions

Use the type that matches the stakes, and keep them rare — a page of admonitions reads as a page of noise.

- `!!! warning` — data loss, a security consequence, or something irreversible.
- `!!! note` — a caveat or clarification that would interrupt the main flow.
- `!!! tip` — an optional improvement.

## Links

Use relative paths between pages: `[Installation](../guides/install.md)`. `mkdocs build --strict` fails on a broken one, so it is worth running before opening a PR.

## Images

Include alt text describing what the image shows. Wrap it in a `<figure>` when a caption adds something the surrounding prose does not:

```markdown
<figure markdown="span">
  ![Deployment lock toggle](path/to/image.png)
  <figcaption>The lock toggle appears only for privileged users.</figcaption>
</figure>
```
