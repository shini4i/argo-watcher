let accessToken: string | null = null;

export const setAccessToken = (token: string | null) => {
  accessToken = token ?? null;
};

export const getAccessToken = () => accessToken;

export const clearAccessToken = () => {
  accessToken = null;
};

export const isAuthenticated = () => accessToken !== null;
