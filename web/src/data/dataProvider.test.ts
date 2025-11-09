import { HttpError } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { dataProvider } from './dataProvider';
import { clearAccessToken, setAccessToken } from '../auth/tokenStore';

const mockFetch = () => vi.spyOn(globalThis, 'fetch');

const jsonResponse = (body: unknown, init?: ResponseInit) =>
  new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
    ...init,
  });

const createListParams = () => ({
  pagination: { page: 1, perPage: 10 },
  sort: { field: 'created', order: 'DESC' as const },
  filter: {},
});

const getQueryParams = (call: string) => new URL(call, 'https://example.test').searchParams;

describe('dataProvider', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2025-01-01T00:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('infers total from pagination context when backend omits total', async () => {
    const fetch = mockFetch().mockResolvedValue(
      jsonResponse({
        tasks: [{ id: '1', created: 1, updated: 1, app: 'demo', author: 'alice', project: 'proj', images: [] }],
      }),
    );

    const result = await dataProvider.getList('tasks', {
      ...createListParams(),
      pagination: { page: 2, perPage: 10 },
    });

    const url = fetch.mock.calls[0][0] as string;
    expect(url).toContain('limit=10');
    expect(url).toContain('offset=10');
    expect(result.total).toBe(11);
    expect(result.data).toHaveLength(1);
  });

  it('trusts backend totals when provided', async () => {
    mockFetch().mockResolvedValue(
      jsonResponse({
        tasks: [
          { id: '1', created: 1, updated: 1, app: 'demo', author: 'alice', project: 'proj', images: [] },
          { id: '2', created: 2, updated: 2, app: 'demo', author: 'bob', project: 'proj', images: [] },
        ],
        total: 42,
      }),
    );

    const result = await dataProvider.getList('tasks', {
      ...createListParams(),
      pagination: { page: 1, perPage: 25 },
    });

    expect(result.total).toBe(42);
    expect(result.data).toHaveLength(2);
  });

  it('fetches task list with default timeframe and returns paginated data', async () => {
    const tasks = Array.from({ length: 10 }, (_, index) => ({
      id: String(index + 1),
      created: index + 1,
      updated: index + 1,
      app: 'demo',
      author: `author-${index + 1}`,
      project: 'proj',
      images: [],
    }));
    const fetch = mockFetch().mockResolvedValue(jsonResponse({ tasks, total: 50 }));

    const result = await dataProvider.getList('tasks', createListParams());

    expect(fetch).toHaveBeenCalledTimes(1);
    const url = fetch.mock.calls[0][0] as string;
    expect(url).toContain('/api/v1/tasks?');
    expect(url).toMatch(/from_timestamp=/);
    expect(url).toContain('limit=10');
    expect(url).toContain('offset=0');
    expect(result.total).toBe(50);
    expect(result.data).toHaveLength(10);
    expect(result.data[0].id).toBe('1');
  });

  it('attaches bearer token when available', async () => {
    setAccessToken('token-123');
    const fetch = mockFetch().mockResolvedValue(jsonResponse({ tasks: [] }));

    await dataProvider.getList('tasks', createListParams());

    const init = fetch.mock.calls[0][1] as RequestInit;
    expect(init.headers).toMatchObject({ Authorization: 'Bearer token-123' });

    clearAccessToken();
  });

  it('supports filtering by explicit start and end timestamps', async () => {
    const fetch = mockFetch().mockResolvedValue(
      jsonResponse({
        tasks: [],
      }),
    );

    await dataProvider.getList('tasks', {
      ...createListParams(),
      filter: {
        start: 1000,
        end: 2000,
      },
    });

    const url = fetch.mock.calls[0][0] as string;
    expect(url).toContain('from_timestamp=1000');
    expect(url).toContain('to_timestamp=2000');
  });

  it('converts Date and ISO filter inputs to unix timestamps', async () => {
    const fetch = mockFetch().mockResolvedValue(jsonResponse({ tasks: [] }));
    await dataProvider.getList('tasks', {
      ...createListParams(),
      filter: {
        from: new Date('2024-12-31T12:00:00Z'),
        to: '2025-01-02T03:04:05Z',
      },
    });

    const params = getQueryParams(fetch.mock.calls[0][0] as string);
    expect(params.get('from_timestamp')).toBe(String(Math.floor(Date.parse('2024-12-31T12:00:00Z') / 1000)));
    expect(params.get('to_timestamp')).toBe(String(Math.floor(Date.parse('2025-01-02T03:04:05Z') / 1000)));
  });

  it('falls back to default timeframe when filters are invalid', async () => {
    const fetch = mockFetch().mockResolvedValue(jsonResponse({ tasks: [] }));
    await dataProvider.getList('tasks', {
      ...createListParams(),
      filter: { from: 'invalid', to: 'invalid' },
    });
    const nowSeconds = Math.floor(Date.now() / 1000);
    const params = getQueryParams(fetch.mock.calls[0][0] as string);
    expect(params.get('from_timestamp')).toBe(String(nowSeconds - 24 * 60 * 60));
    expect(params.get('to_timestamp')).toBe(String(nowSeconds));
  });

  it('throws HttpError when backend responds with error payload', async () => {
    mockFetch().mockResolvedValue(
      jsonResponse({
        tasks: [],
        error: 'argocd is unavailable',
      }),
    );

    await expect(dataProvider.getList('tasks', createListParams())).rejects.toThrow(HttpError);
  });

  it('retrieves a single task status', async () => {
    mockFetch().mockResolvedValue(
      jsonResponse({
        id: '123',
        app: 'demo',
        status: 'in progress',
      }),
    );

    const result = await dataProvider.getOne('tasks', { id: '123' });
    expect(result.data.id).toBe('123');
    expect(result.data.status).toBe('in progress');
  });

  it('throws HttpError when task detail contains error', async () => {
    mockFetch().mockResolvedValue(
      jsonResponse({
        id: 'missing',
        error: 'task not found',
      }),
    );

    await expect(dataProvider.getOne('tasks', { id: 'missing' })).rejects.toThrow(HttpError);
  });

  it('creates a task and returns accepted status', async () => {
    const payload = {
      app: 'demo',
      author: 'alice',
      project: 'proj',
      images: [{ image: 'img', tag: 'latest' }],
    };

    const fetch = mockFetch().mockResolvedValue(
      jsonResponse(
        {
          id: 'new-id',
          status: 'accepted',
        },
        { status: 202 },
      ),
    );

    const result = await dataProvider.create('tasks', { data: payload, previousData: undefined });
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining('/api/v1/tasks'),
      expect.objectContaining({ method: 'POST' }),
    );
    expect(result.data.id).toBe('new-id');
  });

  it('propagates server validation errors during creation', async () => {
    mockFetch().mockResolvedValue(
      jsonResponse(
        {
          status: 'invalid payload',
          error: 'missing images',
        },
        { status: 406 },
      ),
    );

    await expect(
      dataProvider.create('tasks', {
        data: { app: 'demo' },
        previousData: undefined,
      }),
    ).rejects.toThrow(HttpError);
  });

  it('throws when creation response omits the identifier', async () => {
    mockFetch().mockResolvedValue(
      jsonResponse(
        {
          status: 'accepted',
        },
        { status: 202 },
      ),
    );

    await expect(
      dataProvider.create('tasks', {
        data: { app: 'demo' },
        previousData: undefined,
      }),
    ).rejects.toThrow('Task creation did not return an identifier');
  });

  it('rejects unsupported update operations', async () => {
    await expect(
      dataProvider.update('tasks', {
        id: '1',
        data: {},
        previousData: {},
      }),
    ).rejects.toThrow(HttpError);
  });

  it('rejects unsupported resources', async () => {
    await expect(dataProvider.getList('unknown', createListParams())).rejects.toThrow('Unsupported resource');
  });

  it('retrieves multiple records via getMany and preserves fallback ids', async () => {
    mockFetch()
      .mockResolvedValueOnce(
        jsonResponse({
          id: 'task-1',
          status: 'ok',
        }),
      )
      .mockResolvedValueOnce(
        jsonResponse({
          status: 'queued',
        }),
      );

    const result = await dataProvider.getMany('tasks', { ids: ['task-1', 'missing-id'] });
    expect(result.data).toHaveLength(2);
    expect(result.data[0].id).toBe('task-1');
    expect(result.data[1].id).toBe('missing-id');
  });

  it('returns empty data for getManyReference', async () => {
    const result = await dataProvider.getManyReference('tasks', {
      target: 'app',
      id: 'demo',
      pagination: { page: 1, perPage: 5 },
      sort: { field: 'created', order: 'DESC' },
      filter: {},
    });
    expect(result).toEqual({ data: [], total: 0 });
  });
});
