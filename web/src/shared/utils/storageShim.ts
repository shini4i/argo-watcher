/**
 * @description Builds a Storage backed by a Map, for a browser that has none.
 * Values are not own properties, so `storage.foo`, `storage[0]` and
 * `Object.keys` do not see them the way native Storage allows — enough for
 * getItem/setItem callers, which is all this app and ra-core's store need.
 * @returns an in-memory Storage; values live until the page is left
 */
const createMemoryStorage = (): Storage => {
  const values = new Map<string, string>();

  return {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key: string) => values.get(key) ?? null,
    key: (index: number) => Array.from(values.keys())[index] ?? null,
    removeItem: (key: string) => {
      values.delete(key);
    },
    setItem: (key: string, value: string) => {
      values.set(key, String(value));
    },
  } as Storage;
};

/**
 * @description Swaps an unreadable `window.localStorage` for an in-memory one.
 * Chrome and Firefox with site data blocked make the property itself throw
 * SecurityError, and react-admin reads it while its module evaluates, outside
 * its own try/catch — so the app would die on import before rendering.
 * @returns true when a replacement was installed
 */
export const installStorageShim = (): boolean => {
  const browserWindow = globalThis.window;
  if (!browserWindow) {
    return false;
  }

  try {
    if (browserWindow.localStorage != null) {
      return false;
    }
  } catch {
    // Falls through to the replacement below.
  }

  try {
    Object.defineProperty(browserWindow, 'localStorage', {
      configurable: true,
      value: createMemoryStorage(),
    });
    return true;
  } catch {
    // A non-configurable property cannot be replaced; the safe* helpers still
    // keep each individual access from propagating.
    return false;
  }
};
