import { installStorageShim } from './storageShim';

// Importing this module runs the shim; importing `./storageShim` does not.
// Keep it the first import in main.tsx — ESM hoists imports, so calling the
// function from a module body would run after react-admin had evaluated. A
// `sideEffects: false` in package.json would tree-shake this away silently.
installStorageShim();
