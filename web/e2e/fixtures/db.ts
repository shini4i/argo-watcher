import { Client } from 'pg';
import { e2eEnv } from '../config/env';

const createClient = () =>
  new Client({
    host: e2eEnv.postgres.host,
    port: e2eEnv.postgres.port,
    user: e2eEnv.postgres.user,
    password: e2eEnv.postgres.password,
    database: e2eEnv.postgres.database,
  });

const runQuery = async (query: string, params: unknown[] = []) => {
  const client = createClient();
  await client.connect();
  try {
    await client.query(query, params);
  } finally {
    await client.end();
  }
};

export const resetTasksTable = async () => {
  await runQuery('TRUNCATE TABLE public.tasks RESTART IDENTITY CASCADE;');
  await runQuery('SELECT pg_advisory_unlock_all();');
};

export interface TaskMetadataUpdate {
  id: string;
  status?: string;
  statusReason?: string;
  createdAt?: Date;
  updatedAt?: Date;
  images?: Array<{ image: string; tag: string }>;
}

export const updateTaskMetadata = async (metadata: TaskMetadataUpdate) => {
  const createdAt = metadata.createdAt ?? new Date();
  const updatedAt = metadata.updatedAt ?? createdAt;
  await runQuery(
    `
      UPDATE public.tasks
      SET
        created = $2,
        updated = $3,
        status = COALESCE($4, status),
        status_reason = COALESCE($5, status_reason),
        images = CASE WHEN $6::jsonb IS NULL THEN images ELSE $6::jsonb END
      WHERE id = $1::uuid;
    `,
    [
      metadata.id,
      createdAt.toISOString(),
      updatedAt.toISOString(),
      metadata.status ?? null,
      metadata.statusReason ?? null,
      metadata.images ? JSON.stringify(metadata.images) : null,
    ],
  );
};
