let accessToken: string | null = null;

/** Stores the current Keycloak access token in memory for API calls. */
export const setAccessToken = (token: string | null) => {
  accessToken = token ?? null;
};

/** Returns the in-memory access token, if any. */
export const getAccessToken = () => accessToken;

/** Clears the cached token, effectively logging the user out locally. */
export const clearAccessToken = () => {
  accessToken = null;
};

/** Indicates whether a token is currently cached for authenticated requests. */
export const isAuthenticated = () => accessToken !== null;
