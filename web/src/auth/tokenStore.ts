let accessToken: string | null = null;

let accessToken: string | null = null;

/** Stores the current Keycloak access token in memory for API calls. */
export const setAccessToken = (token: string | null): void => {
  accessToken = token ?? null;
};

/** Returns the in-memory access token, if any. */
export const getAccessToken = (): string | null => accessToken;

/** Clears the cached token, effectively logging the user out locally. */
export const clearAccessToken = (): void => {
  accessToken = null;
};

/** Indicates whether a token is currently cached for authenticated requests. */
export const isAuthenticated = (): boolean => accessToken !== null;
