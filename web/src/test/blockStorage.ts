/**
 * @description Replaces `window.localStorage` with a getter that throws, the
 * shape Safari private mode and a cookie-blocked Firefox or Chrome take: the
 * property access fails, not just the method call.
 * @returns a function putting the real property back
 */
export const blockStorageAccess = (): (() => void) => {
  const descriptor = Object.getOwnPropertyDescriptor(window, 'localStorage');

  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    get() {
      throw new DOMException('blocked', 'SecurityError');
    },
  });

  return () => {
    if (descriptor) {
      Object.defineProperty(window, 'localStorage', descriptor);
    }
  };
};
