export interface FailureSummary {
  /** Most specific message that could be identified, always non-empty. */
  readonly headline: string;
  /** `file:line[:col]` when the tool reported one. */
  readonly location?: string;
  /** The reason exactly as the backend stored it. */
  readonly raw: string;
  readonly lineCount: number;
}

/** Longest headline that still fits one line of a table sub-row. */
const HEADLINE_MAX_LENGTH = 200;

// Helm reports the template and line it choked on; both are worth promoting.
const EXECUTION_ERROR = /Error:\s*execution error at \(([^)]+)\):\s*([^\n]+)/;
const BARE_ERROR = /Error:\s*([^\n]+)/;

const truncate = (value: string): string =>
  value.length > HEADLINE_MAX_LENGTH ? `${value.slice(0, HEADLINE_MAX_LENGTH - 1).trimEnd()}…` : value;

/**
 * @description Extracts a one-line headline, and a source location when one is
 * reported, from an Argo CD `status_reason`. Extraction is best-effort by
 * design: it never rewrites or discards the raw text, which callers must keep
 * on screen and copyable.
 * @param reason the stored status_reason, or nothing
 * @returns a summary, or null when there is no reason to summarise
 */
export const summariseFailure = (reason?: string | null): FailureSummary | null => {
  if (!reason?.trim()) {
    return null;
  }

  const lineCount = reason.split('\n').length;

  // Each branch falls through when its capture is only whitespace, so the
  // headline is always non-empty as the interface promises.
  const execution = EXECUTION_ERROR.exec(reason);
  if (execution?.[2].trim()) {
    return {
      headline: truncate(execution[2].trim()),
      location: execution[1].trim(),
      raw: reason,
      lineCount,
    };
  }

  const bare = BARE_ERROR.exec(reason);
  if (bare?.[1].trim()) {
    return { headline: truncate(bare[1].trim()), raw: reason, lineCount };
  }

  const firstLine = reason.split('\n').find(line => line.trim().length > 0) ?? reason;
  return { headline: truncate(firstLine.trim()), raw: reason, lineCount };
};
