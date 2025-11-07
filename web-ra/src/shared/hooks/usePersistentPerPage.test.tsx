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

describe('per-page persistence utilities', () => {
  beforeEach(() => {
    window.localStorage.clear();
    (useListPaginationContext as unknown as vi.Mock).mockReturnValue({ perPage: 50 });
  });

  it('readPersistentPerPage returns stored value when present', () => {
    window.localStorage.setItem('key', '100');
    expect(readPersistentPerPage('key', 25)).toBe(100);
  });

  it('readPersistentPerPage falls back when storage value is invalid', () => {
    window.localStorage.setItem('key', 'oops');
    expect(readPersistentPerPage('key', 25)).toBe(25);
  });

  it('readPersistentPerPage falls back when storage missing', () => {
    expect(readPersistentPerPage('missing', 40)).toBe(40);
  });

  it('PerPagePersistence writes current perPage into storage', () => {
    render(<PerPagePersistence storageKey="key" />);
    expect(window.localStorage.getItem('key')).toBe('50');
  });

  it('PerPagePersistence does nothing when perPage is undefined', () => {
    (useListPaginationContext as unknown as vi.Mock).mockReturnValue({ perPage: undefined });
    render(<PerPagePersistence storageKey="key" />);
    expect(window.localStorage.getItem('key')).toBeNull();
  });
});
