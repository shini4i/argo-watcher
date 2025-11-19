import type { TaskStatus } from '../../src/data/types';
import { e2eEnv } from '../config/env';

type HttpMethod = 'GET' | 'POST' | 'DELETE';

interface RequestOptions {
  method?: HttpMethod;
  body?: unknown;
  headers?: Record<string, string>;
}

const request = async <T>(path: string, options: RequestOptions = {}): Promise<T> => {
  const url = new URL(path, e2eEnv.apiBaseUrl);
  const headers: Record<string, string> = {
    Accept: 'application/json',
    'Content-Type': 'application/json',
    ...options.headers,
  };

  const response = await fetch(url, {
    method: options.method ?? 'GET',
    headers,
    body: options.body ? JSON.stringify(options.body) : undefined,
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(`API ${response.status} for ${url}: ${text}`);
  }

  const contentType = response.headers.get('content-type');
  if (contentType?.includes('application/json')) {
    return (await response.json()) as T;
  }

  return undefined as T;
};

interface CreateTaskPayload {
  app: string;
  author: string;
  project: string;
  images: Array<{ image: string; tag: string }>;
}

export const createTask = async (payload: CreateTaskPayload) => {
  const data = await request<TaskStatus>('/api/v1/tasks', {
    method: 'POST',
    body: payload,
  });
  if (!data?.id) {
    throw new Error('Task creation did not return an id');
  }
  return data;
};

export const getConfig = async () => request<Record<string, unknown>>('/api/v1/config');

export const setDeployLock = async (locked: boolean) => {
  if (locked) {
    await request('/api/v1/deploy-lock', { method: 'POST' });
  } else {
    await request('/api/v1/deploy-lock', { method: 'DELETE' });
  }
};
