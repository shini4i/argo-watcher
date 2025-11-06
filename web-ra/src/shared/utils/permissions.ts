export const hasPrivilegedAccess = (
  userGroups?: ReadonlyArray<string> | null,
  privilegedGroups?: ReadonlyArray<string> | null,
): boolean => {
  if (!Array.isArray(userGroups) || !Array.isArray(privilegedGroups)) {
    return false;
  }

  const privilegedSet = new Set(privilegedGroups);
  return userGroups.some(group => privilegedSet.has(group));
};
