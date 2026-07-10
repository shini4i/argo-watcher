import '@testing-library/jest-dom';

// React 19 tightened act() detection; set the flag authoritatively for the whole
// run (before any component test loads React) so async state updates flush
// reliably and the "not configured to support act(...)" warnings stay silent.
globalThis.IS_REACT_ACT_ENVIRONMENT = true;
