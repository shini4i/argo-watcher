/**
 * User-facing descriptions of the ways an OIDC sign-in can fail.
 *
 * A misconfigured provider is otherwise invisible: the browser leaves for the
 * provider, comes back with an error it cannot act on, and the app renders
 * without a session. These descriptions turn what the provider (or the library)
 * reported into something an operator can act on, so the failure is shown once
 * instead of being retried silently.
 */

/** Longest run of provider-supplied text rendered verbatim. */
const MAX_DETAIL_LENGTH = 300;

/**
 * What went wrong, coarse enough to act on:
 * - `provider_error` — the provider answered the authorization request with an error.
 * - `callback_failed` — the response came back but could not be turned into a session.
 * - `redirect_failed` — the sign-in never started, most often because the provider's
 *   discovery document could not be read.
 * - `config_incomplete` — Argo Watcher enables OIDC without the settings it needs.
 */
export type AuthFailureKind =
  | 'provider_error'
  | 'callback_failed'
  | 'redirect_failed'
  | 'config_incomplete';

/** A sign-in failure, shaped for display on the loading screen. */
export interface AuthFailure {
  readonly kind: AuthFailureKind;
  /** One-line headline naming what failed. */
  readonly title: string;
  /** The provider's OAuth error code, when it supplied one. */
  readonly code?: string;
  /** Provider- or library-supplied explanation, truncated for display. */
  readonly detail?: string;
  /** What to check to fix it. */
  readonly hint?: string;
  /**
   * Provider-supplied documentation link (`error_uri`), present only when the
   * provider sent an absolute http(s) address.
   */
  readonly uri?: string;
}

/**
 * The subset of oidc-client-ts's `ErrorResponse` that carries the diagnosis.
 *
 * The optional fields are `unknown` because they are not guaranteed to be strings:
 * on the token-exchange path the library builds `ErrorResponse` from the provider's
 * parsed JSON body without coercing it, so a non-conformant provider can put any
 * value here (RFC 6749 §5.2 asks for strings). Only `error` is checked, by
 * {@link asProviderError}.
 */
interface ProviderErrorShape {
  error: string;
  error_description?: unknown;
  error_uri?: unknown;
}

/** Trims provider-supplied text to a length the error box can show. */
const clamp = (text: string): string =>
  text.length > MAX_DETAIL_LENGTH ? `${text.slice(0, MAX_DETAIL_LENGTH)}…` : text;

/**
 * Normalizes optional provider text, dropping anything that is not usable text.
 *
 * Total by design: these descriptions are produced while handling a sign-in
 * failure, so a throw in here would reject the bootstrap, mount the app on a
 * failed callback, and start the redirect loop this screen exists to end.
 */
const optionalText = (value: unknown): string | undefined => {
  if (typeof value !== 'string') {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed ? clamp(trimmed) : undefined;
};

/**
 * Keeps `error_uri` only when it is an absolute http(s) address short enough to
 * show in full, so the error box offers a link exclusively when the provider sent
 * something followable. Over-length values are dropped rather than truncated: a
 * clamped URL keeps its host and still resolves, to a path the provider does not
 * have. It also keeps a `javascript:` or `data:` payload — the value arrives in
 * the callback query — from ever becoming a link target.
 */
const optionalUrl = (value: unknown): string | undefined => {
  const text = typeof value === 'string' ? value.trim() : '';
  if (!text || text.length > MAX_DETAIL_LENGTH) {
    return undefined;
  }
  try {
    const url = new URL(text);
    return url.protocol === 'http:' || url.protocol === 'https:' ? text : undefined;
  } catch {
    return undefined;
  }
};

/**
 * Drops trailing slashes so an issuer keeps exactly one before the discovery path.
 *
 * Written as a scan rather than a `/\/+$/` replace, whose backtracking is
 * super-linear in the number of trailing slashes.
 */
const trimTrailingSlashes = (value: string): string => {
  let end = value.length;
  while (end > 0 && value[end - 1] === '/') {
    end -= 1;
  }
  return value.slice(0, end);
};

/** Best-effort message for anything thrown, including non-Error values. */
const messageOf = (error: unknown): string | undefined => {
  if (error instanceof Error) {
    return optionalText(error.message);
  }
  if (typeof error === 'string') {
    return optionalText(error);
  }
  return undefined;
};

/**
 * Recognizes an OAuth error response by its `error` code rather than by class:
 * the code is the load-bearing field, and duck typing keeps this independent of
 * the library's class identity.
 */
const asProviderError = (error: unknown): ProviderErrorShape | null => {
  if (typeof error !== 'object' || error === null) {
    return null;
  }
  const candidate = error as { error?: unknown };
  return typeof candidate.error === 'string' && candidate.error.trim() !== ''
    ? (error as ProviderErrorShape)
    : null;
};

/**
 * Maps an OAuth error code to the setting that produces it. Only the codes with
 * a specific, checkable cause are called out; anything else gets the general
 * pointer at the client registration.
 */
const hintForCode = (code: string): string => {
  switch (code) {
    case 'invalid_scope':
      return 'The provider refused a requested scope. Argo Watcher requests only "openid profile email" — make sure all three are permitted for this client.';
    case 'access_denied':
      return 'The provider or the user denied the request. Check that this user (or their group) is assigned to the Argo Watcher client.';
    case 'invalid_client':
    case 'unauthorized_client':
      return 'The provider did not accept the client. Check that OIDC_CLIENT_ID matches a public client that permits the authorization-code flow with PKCE.';
    default:
      return 'Check the Argo Watcher client registration in your identity provider: allowed scopes, redirect URI, and user assignment.';
  }
};

/**
 * Describes a failure to complete a sign-in callback.
 *
 * A provider error response is reported with its own code and description; any
 * other throw means the response arrived but could not be exchanged locally,
 * which is a different problem and is labelled as one.
 */
export const describeCallbackError = (error: unknown): AuthFailure => {
  const providerError = asProviderError(error);
  if (providerError) {
    // Trimmed once: the code is shown as-is and picks the hint, so a proxy's stray
    // whitespace must not cost the code-specific guidance.
    const code = providerError.error.trim();
    return {
      kind: 'provider_error',
      title: 'The identity provider rejected the sign-in',
      code: clamp(code),
      detail: optionalText(providerError.error_description),
      hint: hintForCode(code),
      uri: optionalUrl(providerError.error_uri),
    };
  }

  return {
    kind: 'callback_failed',
    title: 'The sign-in response could not be completed',
    detail: messageOf(error),
    hint: 'The browser could not match the sign-in it started. This follows from storage cleared mid-flow, a bookmarked callback URL, or clock skew between the browser and the provider.',
  };
};

/**
 * Describes a failure to start the sign-in redirect. Unreadable OIDC discovery is
 * the usual cause, but the same call also fails when the PKCE state cannot be
 * stored, so the title claims only that the sign-in did not start and the hint
 * carries the likely cause.
 *
 * That hint names the discovery URL and insists on the browser as the vantage
 * point: an issuer reachable from the Argo Watcher pod but not from the user's
 * browser is the misconfiguration this screen exists to make visible.
 */
export const describeRedirectError = (error: unknown, issuerUrl?: string): AuthFailure => {
  // Type-checked, not just truthiness-checked: the issuer comes from parsed but
  // unvalidated /api/v1/config JSON, and this must not throw (see optionalText).
  const issuer = typeof issuerUrl === 'string' ? issuerUrl.trim() : '';
  const discoveryUrl = issuer
    ? `${trimTrailingSlashes(issuer)}/.well-known/openid-configuration`
    : undefined;

  return {
    kind: 'redirect_failed',
    title: 'Could not start the sign-in',
    detail: messageOf(error),
    hint: discoveryUrl
      ? `Check that ${discoveryUrl} is reachable from this browser — not only from the Argo Watcher server — and that it answers cross-origin requests.`
      : 'Check that the configured issuer URL is reachable from this browser — not only from the Argo Watcher server — and that its discovery document answers cross-origin requests.',
  };
};

/**
 * Describes an OIDC block that is enabled but missing required settings.
 *
 * @param missing - configuration field names the server did not supply.
 */
export const describeIncompleteConfig = (missing: readonly string[]): AuthFailure => ({
  kind: 'config_incomplete',
  title: 'Argo Watcher is misconfigured for OIDC',
  detail: `OIDC is enabled but the server did not report: ${missing.join(', ')}.`,
  hint: 'Set OIDC_ISSUER_URL and OIDC_CLIENT_ID on the Argo Watcher server (or the legacy KEYCLOAK_URL, KEYCLOAK_REALM and KEYCLOAK_CLIENT_ID).',
});
