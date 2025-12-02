import { describe, expect, it } from 'vitest';
import { getBrowserDocument, getBrowserWindow, hasBrowserWindow } from './browser';

describe('browser utils', () => {
  it('returns window/document when present', () => {
    const originalWindow = globalThis.window;
    const originalDocument = globalThis.document;
    const fakeWindow = {
      document: { title: 'test' },
    } as unknown as Window;
    (globalThis as Record<string, unknown>).window = fakeWindow;
    (globalThis as Record<string, unknown>).document = fakeWindow.document;

    try {
      expect(getBrowserWindow()).toBe(fakeWindow);
      expect(getBrowserDocument()).toBe(fakeWindow.document);
      expect(hasBrowserWindow()).toBe(true);
    } finally {
      (globalThis as Record<string, unknown>).window = originalWindow;
      (globalThis as Record<string, unknown>).document = originalDocument;
    }
  });

  it('handles non-browser environments gracefully', () => {
    const originalWindow = globalThis.window;
    const originalDocument = globalThis.document;
    delete (globalThis as Record<string, unknown>).window;
    delete (globalThis as Record<string, unknown>).document;

    try {
      expect(getBrowserWindow()).toBeUndefined();
      expect(getBrowserDocument()).toBeUndefined();
      expect(hasBrowserWindow()).toBe(false);
    } finally {
      (globalThis as Record<string, unknown>).window = originalWindow;
      (globalThis as Record<string, unknown>).document = originalDocument;
    }
  });
});
