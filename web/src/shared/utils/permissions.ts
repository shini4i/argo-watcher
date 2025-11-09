/** Determines whether the user belongs to any of the privileged groups. */
type GroupList = ReadonlyArray<string>;

let cachedPrivilegedGroups: GroupList | null = null;
let cachedPrivilegedSet: ReadonlySet<string> | null = null;

/** Returns a memoized Set for the provided privileged group array to avoid repeated allocations. */
const getPrivilegedSet = (groups: GroupList): ReadonlySet<string> => {
  if (cachedPrivilegedGroups === groups && cachedPrivilegedSet) {
    return cachedPrivilegedSet;
  }

  cachedPrivilegedGroups = groups;
  cachedPrivilegedSet = new Set(groups);
  return cachedPrivilegedSet;
};

/**
 * Determines whether the user belongs to any of the privileged groups.
 * Accepts an optional precomputed Set for call-sites that already maintain one.
 */
export const hasPrivilegedAccess = (
  userGroups?: GroupList | null,
  privilegedGroups?: GroupList | null,
  privilegedSetOverride?: ReadonlySet<string>,
): boolean => {
  if (!Array.isArray(userGroups)) {
    return false;
  }

  let privilegedSet: ReadonlySet<string> | null = privilegedSetOverride ?? null;

  if (!privilegedSet && Array.isArray(privilegedGroups)) {
    privilegedSet = getPrivilegedSet(privilegedGroups);
  }

  if (!privilegedSet) {
    return false;
  }

  return userGroups.some(group => privilegedSet.has(group));
};
