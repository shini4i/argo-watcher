import { getAccessToken } from '../../auth/tokenStore';
import { buildQueryString } from '../../data/httpClient';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

/** Supported formats for history export requests. */
export type HistoryExportFormat = 'json' | 'csv';

/** Filters that mirror the backend query parameters for historical exports. */
export interface HistoryExportFilters {
  start?: number;
  end?: number;
  app?: string;
}

export interface HistoryExportRequest {
  format: HistoryExportFormat;
  anonymize: boolean;
  filters: HistoryExportFilters;
}

/** Builds the absolute API URL, falling back to a relative path when no base URL is configured. */
const buildExportUrl = (path: string): string => {
  if (!API_BASE_URL) {
    return path;
  }

  const normalizedBase = API_BASE_URL.endsWith('/') ? API_BASE_URL.slice(0, -1) : API_BASE_URL;
  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  return `${normalizedBase}${normalizedPath}`;
};

/** Extracts the most descriptive error message from a failed export response. */
const extractErrorMessage = async (response: Response): Promise<string> => {
  try {
    const payload = await response.clone().json();
    if (payload && typeof payload.status === 'string') {
      return payload.status;
    }
    if (payload && typeof payload.error === 'string') {
      return payload.error;
    }
  } catch {
    // Ignore JSON parsing errors and fall back to the default message.
  }
  return response.statusText || 'Export failed';
};

/** Requests the backend to generate the export and returns the resulting Blob for download. */
export const requestHistoryExport = async ({ format, anonymize, filters }: HistoryExportRequest): Promise<Blob> => {
  const query = buildQueryString({
    format,
    anonymize,
    from_timestamp: filters.start,
    to_timestamp: filters.end,
    app: filters.app,
  });

  const token = getAccessToken();
  const headers: Record<string, string> = {};
  if (token) {
    const bearerToken = `Bearer ${token}`;
    headers['Keycloak-Authorization'] = bearerToken;
    headers.Authorization = bearerToken;
  }

  const response = await fetch(buildExportUrl(`/api/v1/tasks/export${query}`), {
    headers,
  });

  if (!response.ok) {
    throw new Error(await extractErrorMessage(response));
  }

  return response.blob();
};
