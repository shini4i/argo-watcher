import { HttpError } from 'react-admin';
import { getAccessToken } from '../auth/tokenStore';

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';

const DEFAULT_HEADERS: Record<string, string> = {
  Accept: 'application/json',
};

type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

export interface HttpClientOptions {
  method?: HttpMethod;
  headers?: HeadersInit;
  body?: unknown;
}

export interface HttpResponse<T> {
  /** Parsed JSON payload or undefined when the response has no body. */
  data: T | undefined;
  status: number;
  headers: Headers;
}

/** Combines the API base URL with a relative path while cleaning duplicate slashes. */
const joinUrl = (base: string, path: string): string => {
  const normalizedBase = base.endsWith('/') ? base.slice(0, -1) : base;
  const normalizedPath = path.startsWith('/') ? path : `/${path}`;
  return `${normalizedBase}${normalizedPath}`;
};

/** Converts various header shapes (Headers, arrays, objects) into a plain object. */
const normalizeHeaders = (headers?: HeadersInit): Record<string, string> => {
  if (!headers) {
    return {};
  }

  if (headers instanceof Headers) {
    return Object.fromEntries(headers.entries());
  }

  if (Array.isArray(headers)) {
    return Object.fromEntries(headers);
  }

  return { ...headers };
};

/** Builds a fetch RequestInit including auth headers and serialized bodies. */
const buildRequestInit = (options: HttpClientOptions): RequestInit => {
  const method = options.method ?? 'GET';
  const headers = { ...DEFAULT_HEADERS, ...normalizeHeaders(options.headers) };
  const token = getAccessToken();
  if (token && !headers.Authorization) {
    headers.Authorization = `Bearer ${token}`;
    if (!headers['Keycloak-Authorization']) {
      headers['Keycloak-Authorization'] = `Bearer ${token}`;
    }
  } else if (token && headers.Authorization && !headers['Keycloak-Authorization']) {
    headers['Keycloak-Authorization'] = headers.Authorization;
  }

  const init: RequestInit = { method, headers };

  if (options.body !== undefined && method !== 'GET') {
    headers['Content-Type'] = headers['Content-Type'] ?? 'application/json';
    init.body = typeof options.body === 'string' ? options.body : JSON.stringify(options.body);
  }

  return init;
};

/** Parses JSON bodies defensively, returning undefined for non-json responses. */
const parseJson = async <T>(response: Response): Promise<T | undefined> => {
  const contentType = response.headers.get('Content-Type') ?? '';
  if (!contentType.includes('application/json')) {
    return undefined;
  }

  const text = await response.text();
  if (!text) {
    return undefined;
  }

  try {
    return JSON.parse(text) as T;
  } catch (error) {
    throw new HttpError('Failed to parse server response', response.status, { cause: error });
  }
};

/** Creates a HttpError with the most descriptive message available from the payload. */
const buildHttpError = (status: number, body: unknown, fallbackMessage: string): HttpError => {
  if (body && typeof body === 'object' && 'status' in body && typeof body.status === 'string') {
    return new HttpError(body.status, status, body);
  }

  if (body && typeof body === 'object' && 'error' in body && typeof body.error === 'string') {
    return new HttpError(body.error, status, body);
  }

  if (fallbackMessage) {
    return new HttpError(fallbackMessage, status, body);
  }

  return new HttpError('Request failed', status, body);
};

/** Wrapper around fetch that injects auth headers and normalizes error handling. */
export const httpClient = async <T>(path: string, options: HttpClientOptions = {}): Promise<HttpResponse<T>> => {
  const url = joinUrl(API_BASE_URL, path);
  const requestInit = buildRequestInit(options);

  let response: Response;
  try {
    response = await fetch(url, requestInit);
  } catch (error) {
    if (error instanceof HttpError) {
      throw error;
    }

    throw new HttpError('Network error', 0, { cause: error });
  }

  const data = await parseJson<T>(response);

  if (!response.ok) {
    throw buildHttpError(response.status, data, response.statusText);
  }

  if (data && typeof data === 'object' && 'error' in data && data.error) {
    throw buildHttpError(response.status, data, response.statusText);
  }

  return {
    data,
    status: response.status,
    headers: response.headers,
  };
};

/** Serializes REST query parameters, omitting empty values. */
export const buildQueryString = (params: Record<string, string | number | boolean | undefined>) => {
  const searchParams = new URLSearchParams();

  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') {
      continue;
    }

    searchParams.append(key, String(value));
  }

  const query = searchParams.toString();
  return query ? `?${query}` : '';
};
