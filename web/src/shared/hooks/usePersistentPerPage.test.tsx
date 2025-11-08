import React from 'react';
import { render } from '@testing-library/react';
import { vi, describe, it, expect, beforeEach } from 'vitest';

vi.mock('react-admin', async () => {
  const actual = await vi.importActual<Record<string, unknown>>('react-admin');
  return {
    ...actual,
    useListPaginationContext: vi.fn(() => ({
      perPage: 50,
    })),
  };
});

import { useListPaginationContext } from 'react-admin';
import { PerPagePersistence, readPersistentPerPage } from './usePersistentPerPage';

/** Provides the browser window shim used by the tests, throwing if unavailable. */
const getBrowserWindow = (): Window => {
  const browserWindow = globalThis.window;
  if (!browserWindow) {
    throw new Error('Browser window is required for per-page persistence tests.');
  }
  return browserWindow;
};

describe('per-page persistence utilities', () => {
  beforeEach(() => {
    getBrowserWindow().localStorage.clear();
    (useListPaginationContext as unknown as vi.Mock).mockReturnValue({ perPage: 50 });
  });

  it('readPersistentPerPage returns stored value when present', () => {
    getBrowserWindow().localStorage.setItem('key', '100');
    expect(readPersistentPerPage('key', 25)).toBe(100);
  });

  it('readPersistentPerPage falls back when storage value is invalid', () => {
    getBrowserWindow().localStorage.setItem('key', 'oops');
    expect(readPersistentPerPage('key', 25)).toBe(25);
  });

  it('readPersistentPerPage falls back when storage missing', () => {
    expect(readPersistentPerPage('missing', 40)).toBe(40);
  });

  it('PerPagePersistence writes current perPage into storage', () => {
    render(<PerPagePersistence storageKey="key" />);
    expect(getBrowserWindow().localStorage.getItem('key')).toBe('50');
  });

  it('PerPagePersistence does nothing when perPage is undefined', () => {
    (useListPaginationContext as unknown as vi.Mock).mockReturnValue({ perPage: undefined });
    render(<PerPagePersistence storageKey="key" />);
    expect(getBrowserWindow().localStorage.getItem('key')).toBeNull();
  });
});
