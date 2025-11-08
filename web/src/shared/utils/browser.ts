/**
 * Returns the browser Window instance when running in DOM-capable environments.
 * Falls back to undefined when rendered on the server or during non-DOM tests.
 */
export const getBrowserWindow = (): Window | undefined => globalThis.window ?? undefined;

/**
 * Returns the browser Document instance when running with DOM access.
 * Useful for SSR-safe consumers that need to reference document conditionally.
 */
export const getBrowserDocument = (): Document | undefined => globalThis.document ?? undefined;

/** Indicates whether the current runtime exposes a Window object. */
export const hasBrowserWindow = (): boolean => getBrowserWindow() !== undefined;
