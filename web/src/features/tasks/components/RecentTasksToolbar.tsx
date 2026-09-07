import { useCallback, useEffect, useMemo, useRef } from 'react';
import { Stack } from '@mui/material';
import { useGetIdentity, useListContext, useRefresh } from 'react-admin';
import {
  ApplicationFilter,
  normalizeApplicationFilterValue,
} from './ApplicationFilter';
import type { Task } from '../../../data/types';
import { getBrowserWindow } from '../../../shared/utils';
import { useFilterState, type FilterStateSchema } from '../../../shared/hooks/useFilterState';
import { useKeyboardShortcuts } from '../../../shared/hooks/useKeyboardShortcuts';
import { ActiveFilterBar, type FilterChipDescriptor } from './ActiveFilterBar';
import { ListToolbar } from './ListToolbar';
import { RefreshControl } from './RefreshControl';
import { ScopeTabs, type TaskScope } from './ScopeTabs';
import { SearchInput } from './SearchInput';
import { StatusTabs } from './StatusTabs';
import { useTaskListContext } from './TaskListContext';

interface RecentFiltersValues extends Record<string, unknown> {
  app: string;
  status: string | null;
  search: string;
  scope: TaskScope;
}

const DEFAULTS: RecentFiltersValues = { app: '', status: null, search: '', scope: 'everyone' };

/**
 * @description Builds the filter schema, closing over the signed-in address so
 * the scope choice projects onto the backend's `author` filter. useFilterState
 * is then the single writer of filterValues — a second writer would replace the
 * object it just set and silently drop the app/status/search filters.
 * @param identityEmail signed-in address, or '' when anonymous
 */
const buildSchema = (identityEmail: string): FilterStateSchema<RecentFiltersValues> => ({
  app: {
    fromUrl: raw => normalizeApplicationFilterValue(raw),
    toUrl: value => value || null,
    storage: true,
  },
  status: {
    fromUrl: raw => raw ?? null,
    toUrl: value => value || null,
    storage: false,
  },
  // Not persisted: a stored search would silently hide rows on the next visit.
  search: {
    fromUrl: raw => raw?.trim() ?? '',
    toUrl: value => value.trim() || null,
    storage: false,
  },
  // Stored as the choice; the address is derived here on every apply, so a
  // scope restored from a previous session follows whoever is signed in now.
  scope: {
    fromUrl: raw => (raw === 'mine' ? 'mine' : 'everyone'),
    toUrl: value => value,
    filterKey: 'author',
    toFilter: value => (value === 'mine' && identityEmail ? identityEmail : undefined),
    storage: true,
  },
});

const SCOPE_URL_KEY = 'scope';

/** Whether the reader has an explicit scope choice, as opposed to the default. */
const readExplicitScope = (storageKey: string): TaskScope | null => {
  const fromUrl = new URLSearchParams(globalThis.location?.search ?? '').get(SCOPE_URL_KEY);
  if (fromUrl === 'mine' || fromUrl === 'everyone') {
    return fromUrl;
  }
  const stored = getBrowserWindow()?.localStorage?.getItem(`${storageKey}.${SCOPE_URL_KEY}`);
  return stored === 'mine' || stored === 'everyone' ? stored : null;
};

/**
 * @description Filter bar for the recent list. The scope pills default to
 * "Mine" for a signed-in user, since a developer's usual question is whether
 * their own deployment landed; anonymous mode has no identity, so the control
 * is hidden and the scope stays "Everyone".
 */
export const RecentTasksToolbar = ({ storageKey = 'recentTasks' }: { storageKey?: string }) => {
  const { data } = useListContext<Task>();
  const records = useMemo(() => (Array.isArray(data) ? data : []), [data]);
  // Refresh every active query, not just the list: the status pills are backed
  // by their own useGetList, so the list's `refetch` would leave their counts
  // frozen at whatever the first load saw.
  const handleRefresh = useRefresh();
  const { data: identity } = useGetIdentity();
  const identityEmail = identity?.email ?? '';
  const scopeAvailable = Boolean(identityEmail);
  const searchFocusRef = useRef<(() => void) | null>(null);

  // Read once: whether the reader ever chose a scope, as opposed to inheriting
  // the default. The identity may not have resolved yet on a cold load, so the
  // default cannot be baked into the frozen `initial` below.
  const explicitScopeRef = useRef<TaskScope | null>(readExplicitScope(storageKey));

  const defaults = useMemo<RecentFiltersValues>(
    () => ({ ...DEFAULTS, scope: explicitScopeRef.current ?? (scopeAvailable ? 'mine' : 'everyone') }),
    [scopeAvailable],
  );

  const schema = useMemo(() => buildSchema(identityEmail), [identityEmail]);

  const { values, applied, apply } = useFilterState<RecentFiltersValues>({
    storageKey,
    schema,
    defaults,
  });

  const { registerClearAll } = useTaskListContext();

  const effectiveScope: TaskScope = scopeAvailable ? applied.scope : 'everyone';

  // The identity arrives after mount on a cold load, so re-apply once it lands:
  // the author projection needs it, and an unchosen scope defaults to Mine.
  const syncedIdentityRef = useRef(identityEmail);
  useEffect(() => {
    if (syncedIdentityRef.current === identityEmail) {
      return;
    }
    syncedIdentityRef.current = identityEmail;
    const scope = explicitScopeRef.current ?? (identityEmail ? 'mine' : 'everyone');
    apply({ ...values, scope });
    // `apply` and `values` re-identify every render; keying on the identity is
    // what makes this fire exactly once per identity change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [identityEmail]);

  const handleApplicationChange = useCallback(
    (next: string) => {
      apply({ ...values, app: normalizeApplicationFilterValue(next) });
    },
    [apply, values],
  );

  const handleStatusChange = useCallback(
    (next: string | null) => {
      apply({ ...values, status: next });
    },
    [apply, values],
  );

  const handleSearchChange = useCallback(
    (next: string) => {
      apply({ ...values, search: next });
    },
    [apply, values],
  );

  const handleScopeChange = useCallback(
    (next: TaskScope) => {
      // Recording the choice stops a later identity change reverting it.
      explicitScopeRef.current = next;
      apply({ ...values, scope: next });
    },
    [apply, values],
  );

  useKeyboardShortcuts(
    useMemo(
      () => ({
        '/': () => searchFocusRef.current?.(),
        a: () => handleStatusChange(null),
        i: () => handleStatusChange('in progress'),
        // `f` toggles rather than selects, since it is the tab an on-call
        // reader flips in and out of; `a` is how you get back from the others.
        f: () => handleStatusChange(applied.status === 'failed' ? null : 'failed'),
        ...(scopeAvailable
          ? { m: () => handleScopeChange(effectiveScope === 'mine' ? 'everyone' : 'mine') }
          : {}),
      }),
      [applied.status, effectiveScope, handleScopeChange, handleStatusChange, scopeAvailable],
    ),
  );

  const chips: FilterChipDescriptor[] = [];
  if (applied.app) {
    chips.push({
      key: 'app',
      labelPrefix: 'app',
      labelValue: applied.app,
      onRemove: () => apply({ ...values, app: '' }),
    });
  }
  if (applied.search) {
    chips.push({
      key: 'search',
      labelPrefix: 'search',
      labelValue: applied.search,
      onRemove: () => apply({ ...values, search: '' }),
    });
  }
  if (scopeAvailable && effectiveScope === 'mine') {
    chips.push({
      key: 'scope',
      labelPrefix: 'author',
      labelValue: identityEmail,
      onRemove: () => handleScopeChange('everyone'),
    });
  }

  const handleClearAll = useCallback(() => {
    // Clearing every filter is itself a choice of Everyone, so record it —
    // otherwise the identity effect would restore Mine on the next mount.
    explicitScopeRef.current = 'everyone';
    apply({ app: '', status: null, search: '', scope: 'everyone' });
  }, [apply]);

  // `apply` re-identifies on every searchParams/filterValues change, so
  // re-registering the handler each render would thrash the context ref and
  // briefly leave Datagrid's "Clear filters" CTA pointing at null. Park the
  // latest handler in a ref and register a stable indirector exactly once.
  const clearAllHandlerRef = useRef(handleClearAll);
  useEffect(() => {
    clearAllHandlerRef.current = handleClearAll;
  });
  useEffect(
    () => registerClearAll(() => clearAllHandlerRef.current()),
    [registerClearAll],
  );

  return (
    <Stack spacing={0.5} sx={{ width: '100%' }}>
      <ListToolbar
        left={
          <>
            <StatusTabs value={applied.status} onChange={handleStatusChange} />
            {scopeAvailable && (
              <ScopeTabs
                value={effectiveScope}
                onChange={handleScopeChange}
                identityEmail={identityEmail}
              />
            )}
          </>
        }
        right={
          <>
            <ApplicationFilter
              storageKey={`${storageKey}.app`}
              records={records}
              value={applied.app}
              onChange={handleApplicationChange}
            />
            <SearchInput
              value={applied.search}
              onChange={handleSearchChange}
              placeholder="Search app, author, image…"
              focusRef={searchFocusRef}
            />
            <RefreshControl onRefresh={handleRefresh} />
          </>
        }
      />
      <ActiveFilterBar chips={chips} onClearAll={chips.length > 0 ? handleClearAll : undefined} />
    </Stack>
  );
};
