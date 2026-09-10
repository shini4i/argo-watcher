import { useCallback, useEffect, useMemo, useRef } from 'react';
import { Stack } from '@mui/material';
import { useGetIdentity, useRefresh } from 'react-admin';
import { normalizeApplicationFilterValue } from './ApplicationFilter';
import { safeGetItem, safeRemoveItem, safeSetItem } from '../../../shared/utils';
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

const scopeChoicePath = (storageKey: string) => `${storageKey}.scopeChoice`;

/**
 * @description Reads the scope the reader last picked.
 * @param storageKey namespace this toolbar stores under
 * @returns the remembered scope, or the Everyone default when none is stored
 */
const readScopeChoice = (storageKey: string): TaskScope =>
  safeGetItem(scopeChoicePath(storageKey)) === 'mine' ? 'mine' : 'everyone';

/**
 * @description Records a scope the reader picked, or forgets it when they go
 * back to Everyone. Only a pick reaches here, which is what keeps a scope that
 * arrived from a shared link out of the reader's own default.
 * @param storageKey namespace this toolbar stores under
 * @param scope the scope just picked
 */
const writeScopeChoice = (storageKey: string, scope: TaskScope): void => {
  if (scope === 'mine') {
    safeSetItem(scopeChoicePath(storageKey), 'mine');
  } else {
    safeRemoveItem(scopeChoicePath(storageKey));
  }
};

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
  // Only Mine reaches the URL: absence means the Everyone default, so no shared
  // link pins Everyone over the reader's own Mine. Storage is not the hook's —
  // any apply mirrors every stored field, which would turn a link's scope into
  // the reader's default; readScopeChoice/writeScopeChoice own it instead.
  scope: {
    fromUrl: raw => (raw === 'mine' ? 'mine' : 'everyone'),
    toUrl: value => (value === 'mine' ? 'mine' : null),
    filterKey: 'author',
    toFilter: value => (value === 'mine' && identityEmail ? identityEmail : undefined),
    storage: false,
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

  // The remembered pick is the mount default, so the hook still prefers a scope
  // named in the URL over it without ever writing one back.
  const defaults = useMemo(
    () => ({ ...DEFAULTS, scope: readScopeChoice(storageKey) }),
    [storageKey],
  );

  const { values, applied, apply } = useFilterState<RecentFiltersValues>({
    storageKey,
    schema,
    defaults,
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
      // Storage last, so a browser that refuses the write still switches the list.
      apply({ ...values, scope: next });
      writeScopeChoice(storageKey, next);
    },
    [apply, storageKey, values],
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
    writeScopeChoice(storageKey, DEFAULTS.scope);
  }, [apply, storageKey]);

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
