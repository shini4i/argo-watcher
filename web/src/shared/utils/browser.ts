export const getBrowserWindow = (): Window | undefined => globalThis.window ?? undefined;

export const getBrowserDocument = (): Document | undefined => globalThis.document ?? undefined;

export const hasBrowserWindow = (): boolean => getBrowserWindow() !== undefined;
