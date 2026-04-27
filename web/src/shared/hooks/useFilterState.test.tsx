import { act, renderHook } from '@testing-library/react';
import type { Location } from 'react-router-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { ListContextProvider } from 'react-admin';
import type { ListContextValue } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useFilterState, type FilterStateSchema } from './useFilterState';

interface HistoryFilters extends Record<string, unknown> {
  app: string;
  start: number | null;
  end: number | null;
}

const schema: FilterStateSchema<HistoryFilters> = {
  app: {
    fromUrl: raw => raw ?? '',
    toUrl: value => (value ? value : null),
    storage: true,
  },
  start: {
    fromUrl: raw => (raw ? Number(raw) : null),
    toUrl: value => (value === null ? null : String(value)),
    urlKey: 'startDate',
    storage: true,
  },
  end: {
    fromUrl: raw => (raw ? Number(raw) : null),
    toUrl: value => (value === null ? null : String(value)),
    urlKey: 'endDate',
    storage: true,
  },
};

const defaults: HistoryFilters = { app: '', start: null, end: null };

interface HarnessOptions {
  initialEntry?: string;
  setFilters?: ReturnType<typeof vi.fn>;
  filterValues?: Record<string, unknown>;
}

let lastLocation: Location | undefined;

const LocationProbe = () => {
  lastLocation = useLocation();
  return null;
};

const wrapperFactory = ({ initialEntry = '/', setFilters, filterValues = {} }: HarnessOptions) => {
  const ctx = {
    data: [],
    filterValues,
    setFilters: setFilters ?? vi.fn(),
  } as unknown as ListContextValue;

  const Wrapper = ({ children }: { children: React.ReactNode }) => (
    <MemoryRouter initialEntries={[initialEntry]}>
      <LocationProbe />
      <ListContextProvider value={ctx}>{children}</ListContextProvider>
    </MemoryRouter>
  );
  Wrapper.displayName = 'TestWrapper';
  return Wrapper;
};

describe('useFilterState', () => {
  beforeEach(() => {
    lastLocation = undefined;
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it('reads URL params on mount and projects them into filterValues', () => {
    const setFilters = vi.fn();
    const wrapper = wrapperFactory({
      initialEntry: '/?startDate=1700000000&endDate=1700100000&app=demo',
      setFilters,
    });

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    expect(result.current.values).toEqual({ app: 'demo', start: 1700000000, end: 1700100000 });
    expect(setFilters).toHaveBeenCalledWith(
      { app: 'demo', start: 1700000000, end: 1700100000 },
      {},
      false,
    );
  });

  it('falls back to localStorage when URL is empty', () => {
    localStorage.setItem('history.app', 'beta');
    localStorage.setItem('history.start', '1700000000');
    const wrapper = wrapperFactory({});

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    expect(result.current.values.app).toBe('beta');
    expect(result.current.values.start).toBe(1700000000);
    expect(result.current.values.end).toBeNull();
  });

  it('tracks dirty state after setValue and clears it on apply', () => {
    const setFilters = vi.fn();
    const wrapper = wrapperFactory({ setFilters });

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    expect(result.current.isDirty).toBe(false);
    act(() => result.current.setValue('app', 'demo'));
    expect(result.current.isDirty).toBe(true);

    act(() => result.current.apply());
    expect(result.current.isDirty).toBe(false);
    expect(result.current.applied.app).toBe('demo');
  });

  it('writes URL params, localStorage, and filterValues on apply', () => {
    const setFilters = vi.fn();
    const wrapper = wrapperFactory({ setFilters });

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    act(() =>
      result.current.setMany({ app: 'demo', start: 1700000000, end: 1700100000 }),
    );
    act(() => result.current.apply());

    expect(setFilters).toHaveBeenLastCalledWith(
      { app: 'demo', start: 1700000000, end: 1700100000 },
      {},
      false,
    );
    expect(localStorage.getItem('history.app')).toBe('demo');
    expect(localStorage.getItem('history.start')).toBe('1700000000');
    expect(localStorage.getItem('history.end')).toBe('1700100000');

    const params = new URLSearchParams(lastLocation?.search ?? '');
    expect(params.get('app')).toBe('demo');
    expect(params.get('startDate')).toBe('1700000000');
    expect(params.get('endDate')).toBe('1700100000');
  });

  it('removes URL params and storage entries when values are cleared', () => {
    localStorage.setItem('history.app', 'demo');
    const wrapper = wrapperFactory({ initialEntry: '/?app=demo' });

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    act(() => result.current.setValue('app', ''));
    act(() => result.current.apply());

    expect(localStorage.getItem('history.app')).toBeNull();
    const params = new URLSearchParams(lastLocation?.search ?? '');
    expect(params.has('app')).toBe(false);
  });

  it('reset returns values to defaults but keeps applied stable until apply', () => {
    const wrapper = wrapperFactory({ initialEntry: '/?app=demo' });

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    expect(result.current.applied.app).toBe('demo');
    act(() => result.current.reset());
    expect(result.current.values.app).toBe('');
    expect(result.current.applied.app).toBe('demo');
    expect(result.current.isDirty).toBe(true);
  });

  it('URL takes precedence over localStorage on mount', () => {
    localStorage.setItem('history.app', 'fromStorage');
    const wrapper = wrapperFactory({ initialEntry: '/?app=fromUrl' });

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    expect(result.current.values.app).toBe('fromUrl');
    expect(result.current.applied.app).toBe('fromUrl');
  });

  it('falls back to defaults when neither URL nor storage have values', () => {
    const wrapper = wrapperFactory({});

    const { result } = renderHook(() => useFilterState({ storageKey: 'history', schema, defaults }), {
      wrapper,
    });

    expect(result.current.values).toEqual(defaults);
    expect(result.current.applied).toEqual(defaults);
    expect(result.current.isDirty).toBe(false);
  });

  it('does not write to localStorage when schema field opts out (storage: false)', () => {
    interface EphemeralFilters extends Record<string, unknown> {
      app: string;
      query: string;
    }

    const ephemeralSchema: FilterStateSchema<EphemeralFilters> = {
      app: {
        fromUrl: raw => raw ?? '',
        toUrl: value => (value ? value : null),
        storage: true,
      },
      query: {
        fromUrl: raw => raw ?? '',
        toUrl: value => (value ? value : null),
        storage: false,
      },
    };
    const ephemeralDefaults: EphemeralFilters = { app: '', query: '' };

    const wrapper = wrapperFactory({});
    const { result } = renderHook(
      () =>
        useFilterState({
          storageKey: 'recent',
          schema: ephemeralSchema,
          defaults: ephemeralDefaults,
        }),
      { wrapper },
    );

    act(() => result.current.setMany({ app: 'demo', query: 'transient' }));
    act(() => result.current.apply());

    expect(localStorage.getItem('recent.app')).toBe('demo');
    expect(localStorage.getItem('recent.query')).toBeNull();
  });
});
