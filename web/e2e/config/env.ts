export interface PostgresConfig {
  host: string;
  port: number;
  user: string;
  password: string;
  database: string;
}

export const e2eEnv = {
  webBaseUrl: process.env.WEB_BASE_URL ?? 'http://localhost:3100',
  apiBaseUrl: process.env.API_BASE_URL ?? 'http://localhost:8080',
  healthEndpoint: process.env.API_HEALTH_ENDPOINT ?? '/healthz',
  deployToken: process.env.ARGO_WATCHER_DEPLOY_TOKEN ?? 'example',
  postgres: {
    host: process.env.POSTGRES_HOST ?? '127.0.0.1',
    port: Number(process.env.POSTGRES_PORT ?? '5432'),
    user: process.env.POSTGRES_USER ?? 'watcher',
    password: process.env.POSTGRES_PASSWORD ?? 'watcher',
    database: process.env.POSTGRES_DB ?? 'watcher',
  } satisfies PostgresConfig,
};

export type E2EEnv = typeof e2eEnv;
