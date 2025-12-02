import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const fetchMock = vi.fn();
const getAccessTokenMock = vi.fn();

vi.mock('../../auth/tokenStore', () => ({
  getAccessToken: () => getAccessTokenMock(),
}));

const importService = async () => {
  vi.resetModules();
  return import('./exportService');
};

const buildSuccessResponse = (body: Blob | string = 'ok') =>
  new Response(body instanceof Blob ? body : new Blob([body]), { status: 200 });

describe('requestHistoryExport', () => {
  beforeEach(() => {
    fetchMock.mockReset();
    getAccessTokenMock.mockReset();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.unstubAllEnvs();
  });

  it('builds a relative URL and maps query parameters when no base URL is set', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '');
    const { requestHistoryExport } = await importService();

    fetchMock.mockResolvedValue(buildSuccessResponse());

    await requestHistoryExport({
      format: 'json',
      anonymize: true,
      filters: { start: 1700000000, end: 1700003600, app: 'demo-app' },
    });

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/tasks/export?format=json&anonymize=true&from_timestamp=1700000000&to_timestamp=1700003600&app=demo-app',
      { headers: {} },
    );
  });

  it('prefixes the URL with VITE_API_BASE_URL and normalizes slashes', async () => {
    vi.stubEnv('VITE_API_BASE_URL', 'https://api.example.com/');
    const { requestHistoryExport } = await importService();

    fetchMock.mockResolvedValue(buildSuccessResponse());

    await requestHistoryExport({
      format: 'csv',
      anonymize: false,
      filters: { start: 1, end: 2, app: 'demo' },
    });

    expect(fetchMock).toHaveBeenCalledWith(
      'https://api.example.com/api/v1/tasks/export?format=csv&anonymize=false&from_timestamp=1&to_timestamp=2&app=demo',
      { headers: {} },
    );
  });

  it('injects auth headers when a token is available', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '');
    getAccessTokenMock.mockReturnValue('token-123');
    const { requestHistoryExport } = await importService();

    fetchMock.mockResolvedValue(buildSuccessResponse());

    await requestHistoryExport({ format: 'json', anonymize: false, filters: {} });

    const [, init] = fetchMock.mock.calls[0];
    expect(init?.headers).toMatchObject({
      Authorization: 'Bearer token-123',
      'Keycloak-Authorization': 'Bearer token-123',
    });
  });

  it('omits auth headers when no token is available', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '');
    getAccessTokenMock.mockReturnValue(null);
    const { requestHistoryExport } = await importService();

    fetchMock.mockResolvedValue(buildSuccessResponse());

    await requestHistoryExport({ format: 'csv', anonymize: true, filters: {} });

    const [, init] = fetchMock.mock.calls[0];
    expect(init?.headers).toEqual({});
  });

  it('propagates descriptive error messages from status and error fields', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '');
    const { requestHistoryExport } = await importService();

    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ status: 'export not ready' }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(
      requestHistoryExport({ format: 'json', anonymize: false, filters: {} }),
    ).rejects.toThrow('export not ready');

    fetchMock.mockResolvedValue(
      new Response(JSON.stringify({ error: 'bad filters' }), {
        status: 400,
        headers: { 'Content-Type': 'application/json' },
      }),
    );

    await expect(
      requestHistoryExport({ format: 'json', anonymize: false, filters: {} }),
    ).rejects.toThrow('bad filters');
  });

  it('falls back to the response status text when no JSON error is available', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '');
    const { requestHistoryExport } = await importService();

    fetchMock.mockResolvedValue(
      new Response('unparseable', {
        status: 500,
        statusText: 'Server exploded',
        headers: { 'Content-Type': 'text/plain' },
      }),
    );

    await expect(
      requestHistoryExport({ format: 'json', anonymize: false, filters: {} }),
    ).rejects.toThrow('Server exploded');
  });

  it('returns a Blob on success', async () => {
    vi.stubEnv('VITE_API_BASE_URL', '');
    const { requestHistoryExport } = await importService();
    const payload = new Blob(['export-bytes']);

    fetchMock.mockResolvedValue({
      ok: true,
      blob: vi.fn().mockResolvedValue(payload),
    } as unknown as Response);

    const result = await requestHistoryExport({ format: 'json', anonymize: false, filters: {} });

    expect(result).toBe(payload);
    expect(result).toBeInstanceOf(Blob);
    expect(result.size).toBe(payload.size);
  });
});
