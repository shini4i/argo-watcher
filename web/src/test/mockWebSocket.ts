/**
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

  public open() {
    this.onopen?.();
  }

  /**
   * When true, `close()` only records the request and the test delivers the close
   * event itself via {@link fireClose} — matching a real socket, which reports
   * closed asynchronously. Defaults to false so the common case stays synchronous.
   */
  public deferClose = false;

  /** Entry point when production code closes the socket. */
  public close() {
    if (!this.deferClose) {
      this.fireClose();
    }
  }

  public fireClose() {
    this.onclose?.();
  }

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
