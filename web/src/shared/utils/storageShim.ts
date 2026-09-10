/**
 * @description Builds a Storage for a browser that has none. Keys are own
 * enumerable properties of the returned object, as on a native Storage, so
 * callers that enumerate it see the values — ra-core's store walks
 * `Object.keys` in reset and `Object.entries` in listItems.
 * @returns an in-memory Storage; values live until the page is left
 */
const createMemoryStorage = (): Storage => {
  const storage = {} as Storage;
  const values = storage as unknown as Record<string, unknown>;

  const define = (name: string, member: PropertyDescriptor) => {
    Object.defineProperty(storage, name, { configurable: true, ...member });
  };

  define('length', { get: () => Object.keys(storage).length });
  define('clear', {
    value: () => {
      for (const key of Object.keys(storage)) {
        delete values[key];
      }
    },
  });
  define('getItem', {
    value: (key: string) => {
      // Only a stored key is an own enumerable property, so this reads past
      // neither this object's own members nor a polluted Object.prototype.
      const stored = Object.getOwnPropertyDescriptor(storage, key);
      return stored?.enumerable === true ? (stored.value as string) : null;
    },
  });
  define('key', { value: (index: number) => Object.keys(storage)[index] ?? null });
  define('removeItem', {
    value: (key: string) => {
      delete values[key];
    },
  });
  define('setItem', {
    value: (key: string, value: string) => {
      Object.defineProperty(storage, key, {
        configurable: true,
        enumerable: true,
        value: String(value),
        writable: true,
      });
    },
  });

  return storage;
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
