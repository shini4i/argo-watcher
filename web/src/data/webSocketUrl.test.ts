import { beforeEach, describe, expect, it, vi } from 'vitest';
import { resolveWebSocketUrl, webSocketProtocols } from './webSocketUrl';
import * as sharedUtils from '../shared/utils';
import { clearAccessToken, setAccessToken } from '../auth/tokenStore';

/**
 * Covers the extracted, security-sensitive WebSocket URL resolution directly
 * (rather than only through the deploy-lock service), so the same-host guard and
 * protocol mapping keep dedicated coverage independent of any one feature.
 */
describe('resolveWebSocketUrl', () => {
  const originalEnv = { ...import.meta.env };

  const stubLocation = (protocol: string, host: string) =>
    vi.spyOn(sharedUtils, 'getBrowserWindow').mockReturnValue({
      location: { protocol, host },
    } as unknown as Window);

  beforeEach(() => {
    vi.restoreAllMocks();
    import.meta.env.VITE_WS_BASE_URL = originalEnv.VITE_WS_BASE_URL;
  });

  it('accepts a same-host custom base URL', () => {
    stubLocation('https:', 'custom.example');
    import.meta.env.VITE_WS_BASE_URL = 'wss://custom.example';
    expect(resolveWebSocketUrl()).toBe('wss://custom.example/ws');
  });

  it('rejects a cross-host custom base URL and falls back to the current location', () => {
    stubLocation('https:', 'argo.example');
    import.meta.env.VITE_WS_BASE_URL = 'wss://malicious.example';
    expect(resolveWebSocketUrl()).toBe('wss://argo.example/ws');
  });

  it('falls back to the current location when the custom base URL is unparseable', () => {
    const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
    stubLocation('https:', 'argo.example');
    // "ws://" alone is not a valid URL and throws inside the URL constructor.
    import.meta.env.VITE_WS_BASE_URL = 'ws://';
    expect(resolveWebSocketUrl()).toBe('wss://argo.example/ws');
    warnSpy.mockRestore();
  });

  it('derives wss from an https location when no override is set', () => {
    stubLocation('https:', 'argo.example');
    import.meta.env.VITE_WS_BASE_URL = '';
    expect(resolveWebSocketUrl()).toBe('wss://argo.example/ws');
  });

  it('derives ws from an http location when no override is set', () => {
    stubLocation('http:', 'argo.example:8080');
    import.meta.env.VITE_WS_BASE_URL = '';
    expect(resolveWebSocketUrl()).toBe('ws://argo.example:8080/ws');
  });
});

/**
 * The handshake is the one request the browser cannot attach a header to, so the
 * credential rides in the subprotocol list instead. Covered directly because a silent
 * regression here logs every user out of the live banners with OIDC enabled.
 */
describe('webSocketProtocols', () => {
  beforeEach(() => {
    clearAccessToken();
  });

  it('offers only the protocol name when no token is held', () => {
    expect(webSocketProtocols()).toEqual(['argo-watcher.v1']);
  });

  it('appends the token entry when a token is held', () => {
    setAccessToken('abc.def.ghi');

    expect(webSocketProtocols()).toEqual(['argo-watcher.v1', 'argo-watcher.token.abc.def.ghi']);
  });

  it('never sends a token entry with an empty value', () => {
    setAccessToken('');

    expect(webSocketProtocols()).toEqual(['argo-watcher.v1']);
  });
});
