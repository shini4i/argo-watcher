export interface FailureSummary {
  /** First line of the reason, always non-empty. */
  readonly headline: string;
  /** Everything after the headline, on one line; absent when nothing remains. */
  readonly detail?: string;
  /** The reason exactly as the backend stored it. */
  readonly raw: string;
  readonly lineCount: number;
}

/** Longest headline that still fits one line of a table sub-row. */
const HEADLINE_MAX_LENGTH = 200;

// The backend prefixes an Argo CD API failure with this; the panel already
// signals that something failed, so the prefix is noise in the headline.
const API_ERROR_PREFIX = /^ArgoCD API Error:\s*/;

const truncate = (value: string): string =>
  value.length > HEADLINE_MAX_LENGTH ? `${value.slice(0, HEADLINE_MAX_LENGTH - 1).trimEnd()}…` : value;

/**
 * @description Extracts a one-line headline from a task's `status_reason`.
 * argo-watcher composes every reason itself and puts a purpose-built headline on
 * the first line (argocd.rolloutFailureHeadline, ImageNotPartOfAppError.Reason,
 * ArgoAPIErrorTemplate, StaleTaskAbortReason), so the first line IS the headline —
 * searching the body for an error-looking line would discard it in favour of a
 * nested one, and Argo CD sync messages routinely embed "Error:".
 * @param reason the stored status_reason, or nothing
 * @returns a summary whose `raw` is untouched, or null when there is no reason
 */
export const summariseFailure = (reason?: string | null): FailureSummary | null => {
  if (!reason?.trim()) {
    return null;
  }

  const lines = reason.split('\n');
  const headlineIndex = lines.findIndex(line => line.trim().length > 0);
  const firstLine = headlineIndex === -1 ? reason : lines[headlineIndex];
  const headline = firstLine.trim().replace(API_ERROR_PREFIX, '').trim();

  // The remainder only — repeating the headline underneath it says nothing.
  const detail = lines
    .slice(headlineIndex + 1)
    .join(' ')
    .replace(/\s+/g, ' ')
    .trim();

  return {
    // A reason that is only the prefix still needs a headline.
    headline: truncate(headline || firstLine.trim()),
    detail: detail || undefined,
    raw: reason,
    lineCount: lines.length,
  };
};
