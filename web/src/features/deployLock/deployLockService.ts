import { httpClient, type HttpResponse } from '../../data/httpClient';
import { WsStatusService, type WsStatusListener } from '../../data/wsStatusService';

/**
 * Subscribed listener signature invoked whenever the deploy-lock status changes.
 */
export type DeployLockListener = WsStatusListener<boolean>;

/**
 * DeployLockService mirrors the shared deploy lock: `true` while deployments are
 * frozen. On top of the REST-bootstrap-plus-WebSocket protocol it inherits, it
 * adds the imperative operations an operator can trigger from the UI.
 */
export class DeployLockService extends WsStatusService<boolean> {
  constructor() {
    super('deploy-lock');
  }

  /** Reads the current lock state from the backend. */
  protected async fetchState(): Promise<boolean> {
    const response = await httpClient<boolean>('/api/v1/deploy-lock');
    return Boolean(response.data);
  }

  /** Recognises the two lock frames; the other signals on `/ws` are ignored. */
  protected parseMessage(payload: string): boolean | undefined {
    if (payload === 'locked') {
      return true;
    }
    if (payload === 'unlocked') {
      return false;
    }
    return undefined;
  }

  /**
   * Issues a POST request to enable the deploy lock and propagates the new state.
   */
  public async setLock(): Promise<HttpResponse<unknown>> {
    const response = await httpClient('/api/v1/deploy-lock', { method: 'POST' });
    this.applyAuthoritative(true);
    return response;
  }

  /**
   * Issues a DELETE request to release the deploy lock and propagates the new state.
   */
  public async releaseLock(): Promise<HttpResponse<unknown>> {
    const response = await httpClient('/api/v1/deploy-lock', { method: 'DELETE' });
    this.applyAuthoritative(false);
    return response;
  }
}

/**
 * Shared singleton instance consumed by the React-admin UI.
 */
export const deployLockService = new DeployLockService();
