/**
 * Minimal WebSocket stand-in for unit tests. Records every constructed instance
 * and exposes hooks to drive the open/message/close callbacks by hand, so a test
 * can sequence a handshake, a pushed frame and a drop deterministically.
 *
 * Install with `vi.stubGlobal('WebSocket', MockWebSocket as unknown as typeof WebSocket)`
 * and call `MockWebSocket.reset()` in `beforeEach` to clear the instance list.
 */
export class MockWebSocket {
  public onopen: (() => void) | null = null;
  public onmessage: ((event: { data: string }) => void) | null = null;
  public onclose: (() => void) | null = null;
  public onerror: ((error: unknown) => void) | null = null;

  constructor(
    public url: string,
    public protocols?: string | string[],
  ) {
    MockWebSocket.instances.push(this);
  }

  /** Fires the open handler, as the browser does once the handshake completes. */
  public open() {
    this.onopen?.();
  }

  /** Fires the close handler; also the entry point when production code closes the socket. */
  public close() {
    this.onclose?.();
  }

  /** Delivers a text frame to the message handler. */
  public emit(message: string) {
    this.onmessage?.({ data: message });
  }

  /**
   * Delivers a frame whose payload is passed through untouched, for exercising
   * non-text frames (a real socket hands those over as Blob/ArrayBuffer).
   */
  public emitRaw(data: unknown) {
    this.onmessage?.({ data } as { data: string });
  }

  /** Every instance constructed since the last reset, in creation order. */
  static readonly instances: MockWebSocket[] = [];

  static reset() {
    MockWebSocket.instances.length = 0;
  }
}
