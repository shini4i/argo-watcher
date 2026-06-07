# Documentation Style Guide

## Bullets

Use `-` for all bullet points. Do not use `--`.

```markdown
- Item one
- Item two
  - Nested item
```

## Admonitions

Use the correct admonition type for the context:

- `!!! warning` — For actual warnings: data loss, breaking changes, security issues, or irreversible actions.
- `!!! tip` — For optional optimizations, best practices, or helpful hints.
- `!!! note` — For asides, additional context, or clarifications that don't fit the main flow.

## Terminology

Keep terminology consistent across all docs:

- **Argo Watcher** — The product name. Always hyphenated and capitalized.
- **Argo CD** — Two words, no hyphen, both capitalized.
- **deploy token** — Lowercase when used as a prose noun. `DEPLOY_TOKEN` when referring to an environment variable name.
- **GitOps** — Capital G and O, no hyphen.
- **Web UI** — Always "Web UI", not "web interface" or "UI".

## Tables

- **String columns** — Left-align.
- **Numeric columns** — Right-align.
- **Avoid centre alignment.**

Use the `right` attribute in markdown table syntax when needed:

```markdown
| Name | Count |
|------|------:|
| Item | 42    |
```

## Code and Commands

- Inline code: use backticks for variables, file paths, and command names.
- Code blocks: specify the language for syntax highlighting.
- Commands: use code formatting for environment variables and flags.

## Headings

- Use `#` for page titles (H1).
- Use `##` for major sections.
- Use `###` for subsections.
- Keep heading text short and descriptive.

## Cross-references

Use relative links: `[text](../other-page.md)` or `[text](../section/page.md)`.

## Images

Include a `<figcaption>` with every image to provide context:

```markdown
<figure markdown="span">
  ![Alt text](path/to/image.png)
  <figcaption>Descriptive caption</figcaption>
</figure>
```
