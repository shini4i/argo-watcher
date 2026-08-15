type GroupList = ReadonlyArray<string>;

let cachedPrivilegedGroups: GroupList | null = null;
let cachedPrivilegedSet: ReadonlySet<string> | null = null;

const getPrivilegedSet = (groups: GroupList): ReadonlySet<string> => {
  if (cachedPrivilegedGroups === groups && cachedPrivilegedSet) {
    return cachedPrivilegedSet;
  }

  cachedPrivilegedGroups = groups;
  cachedPrivilegedSet = new Set(groups);
  return cachedPrivilegedSet;
};

/** False unless `userGroups` is an array sharing a member with the privileged set. */
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
