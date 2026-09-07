/** One application's aggregate for the selected window, as the API returns it. */
export interface AppSummary {
  app: string;
  project: string;
  total: number;
  failed: number;
  running: number;
  deployed: number;
  median_duration_seconds: number;
  last_created: number;
  last_status: string;
  last_status_reason?: string;
  recent_statuses: string[];
}

export interface AppSummariesResponse {
  apps: AppSummary[];
  total_apps: number;
  error?: string;
}

export type OverviewWindow = '24h' | '7d' | '30d';

/** How far back each window reaches, in seconds. */
export const WINDOW_SECONDS: Readonly<Record<OverviewWindow, number>> = {
  '24h': 24 * 60 * 60,
  '7d': 7 * 24 * 60 * 60,
  '30d': 30 * 24 * 60 * 60,
};

export const WINDOW_LABELS: Readonly<Record<OverviewWindow, string>> = {
  '24h': '24 h',
  '7d': '7 d',
  '30d': '30 d',
};

export const isOverviewWindow = (value: unknown): value is OverviewWindow =>
  value === '24h' || value === '7d' || value === '30d';
