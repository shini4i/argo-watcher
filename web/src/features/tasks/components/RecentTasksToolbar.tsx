import { useCallback, useEffect, useMemo, useRef } from 'react';
import { Stack } from '@mui/material';
import { useGetIdentity, useRefresh } from 'react-admin';
import { normalizeApplicationFilterValue } from './ApplicationFilter';
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
  // Not persisted: Recent has no application picker, so an app filter only ever
  // arrives from a link and remembering it would hide the rest of the estate on
  // the next visit. History persists its own, because there it is a picked value.
  app: {
    fromUrl: raw => normalizeApplicationFilterValue(raw),
    toUrl: value => value || null,
    storage: false,
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
  // Only Mine is written, to URL and storage alike: absence means the Everyone
  // default, so nothing pins a scope the reader never picked — and no shared
  // link pins Everyone over their own Mine. The field is fresh because releases
  // up to 1.3.0 auto-wrote `scope=mine` for readers who never chose it.
  scope: {
    fromUrl: raw => (raw === 'mine' ? 'mine' : 'everyone'),
    toUrl: value => (value === 'mine' ? 'mine' : null),
    filterKey: 'author',
    toFilter: value => (value === 'mine' && identityEmail ? identityEmail : undefined),
    storage: true,
    storageField: 'scopeChoice',
  },
});

/**
 * @description Filter bar for the recent list. The scope pills default to
 * "Everyone" — the readers of this list are on-call, and their usual question
 * is what the whole estate is doing, not what they personally deployed. A
 * chosen scope is remembered; anonymous mode has no identity to scope by, so
 * the control is hidden.
 */
export const RecentTasksToolbar = ({ storageKey = 'recentTasks' }: { storageKey?: string }) => {
  // Refresh every active query, not just the list: the status pills are backed
  // by their own useGetList, so the list's `refetch` would leave their counts
  // frozen at whatever the first load saw.
  const handleRefresh = useRefresh();
  const { data: identity } = useGetIdentity();
  const identityEmail = identity?.email ?? '';
  const scopeAvailable = Boolean(identityEmail);
  const searchFocusRef = useRef<(() => void) | null>(null);

  const schema = useMemo(() => buildSchema(identityEmail), [identityEmail]);

  const { values, applied, apply } = useFilterState<RecentFiltersValues>({
    storageKey,
    schema,
    defaults: DEFAULTS,
  });

  const { registerClearAll } = useTaskListContext();

  const effectiveScope: TaskScope = scopeAvailable ? applied.scope : 'everyone';

  // The identity arrives after mount on a cold load, so re-apply once it lands:
  // a scope of Mine has no address to project onto until then.
  const syncedIdentityRef = useRef(identityEmail);
  useEffect(() => {
    if (syncedIdentityRef.current === identityEmail) {
      return;
    }
    syncedIdentityRef.current = identityEmail;
    apply(values);
    // `apply` and `values` re-identify every render; keying on the identity is
    // what makes this fire exactly once per identity change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [identityEmail]);

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
    apply({ ...DEFAULTS });
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
