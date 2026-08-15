let accessToken: string | null = null;

export const setAccessToken = (token: string | null) => {
  accessToken = token ?? null;
};

export const getAccessToken = () => accessToken;

/** Ends the session locally only: the token is not revoked at the provider. */
export const clearAccessToken = () => {
  accessToken = null;
};

export const isAuthenticated = () => accessToken !== null;
