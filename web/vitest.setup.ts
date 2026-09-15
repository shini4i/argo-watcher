import '@testing-library/jest-dom/vitest';

// React 19 tightened act() detection; set the flag authoritatively for the whole
// run (before any component test loads React) so async state updates flush
// reliably and the "not configured to support act(...)" warnings stay silent.
declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}

globalThis.IS_REACT_ACT_ENVIRONMENT = true;
