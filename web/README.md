# React-admin Workspace Scaffold

This directory hosts the new React-admin frontend for Argo Watcher. It is intentionally minimal and will be extended across the migration phases.

## Commands

```bash
npm install     # install dependencies
npm run dev     # start Vite dev server on http://localhost:5173 with API proxy
npm run build   # produce production assets in dist/
npm run lint    # run eslint against src
npm run test    # execute Vitest unit test suite
```

## Next Steps

- Implement real authentication and data providers (Phase 2).
- Port task list/detail screens using React-admin resources (Phase 3+).
- Extend theming to include runtime light/dark switching and shared utilities.
