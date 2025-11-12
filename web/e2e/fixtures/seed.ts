import type { Task } from '../../src/data/types';
import { createTask, setDeployLock } from './apiClient';
import { resetTasksTable, updateTaskMetadata } from './db';

export interface SeedTaskInput {
  app: string;
  author?: string;
  project?: string;
  images?: Task['images'];
  status?: string;
  statusReason?: string;
  createdAt?: Date;
  updatedAt?: Date;
}

const defaultImages = [
  { image: 'ghcr.io/example/argo-watcher', tag: 'v1.0.0' },
  { image: 'ghcr.io/example/service', tag: 'v2.3.4' },
];

const STATUS = {
  DEPLOYED: 'deployed',
  FAILED: 'failed',
  IN_PROGRESS: 'in progress',
  ABORTED: 'aborted',
};

const createTaskWithMetadata = async (input: SeedTaskInput) => {
  const base = {
    app: input.app,
    author: input.author ?? 'qa.bot@example.com',
    project: input.project ?? 'QA',
    images: input.images ?? defaultImages,
  };
  const created = await createTask(base);
  await updateTaskMetadata({
    id: created.id,
    status: input.status ?? STATUS.IN_PROGRESS,
    statusReason: input.statusReason,
    createdAt: input.createdAt ?? new Date(),
    updatedAt: input.updatedAt ?? new Date(),
    images: base.images,
  });
  return {
    ...base,
    id: created.id,
    status: input.status ?? STATUS.IN_PROGRESS,
    statusReason: input.statusReason ?? '',
  };
};

export const resetState = async () => {
  await resetTasksTable();
  await setDeployLock(false);
};

export const seedRecentTasks = async () => {
  const now = Date.now();
  return Promise.all([
    createTaskWithMetadata({
      app: 'recent-app-deployed',
      status: STATUS.DEPLOYED,
      statusReason: 'Deployment finished successfully',
      createdAt: new Date(now - 5 * 60 * 1000),
      updatedAt: new Date(now - 2 * 60 * 1000),
    }),
    createTaskWithMetadata({
      app: 'recent-app-failed',
      status: STATUS.FAILED,
      statusReason: 'Image digest mismatch detected',
      createdAt: new Date(now - 15 * 60 * 1000),
      updatedAt: new Date(now - 14 * 60 * 1000),
    }),
    createTaskWithMetadata({
      app: 'recent-app-progress',
      status: STATUS.IN_PROGRESS,
      statusReason: '',
      createdAt: new Date(now - 60 * 1000),
      updatedAt: new Date(now - 30 * 1000),
    }),
  ]);
};

export const seedHistoryTasks = async () => {
  const now = Date.now();
  const twoDays = 2 * 24 * 60 * 60 * 1000;
  return Promise.all([
    createTaskWithMetadata({
      app: 'history-app-alpha',
      status: STATUS.DEPLOYED,
      statusReason: 'Rolled out to prod',
      createdAt: new Date(now - twoDays),
      updatedAt: new Date(now - twoDays + 30 * 1000),
    }),
    createTaskWithMetadata({
      app: 'history-app-beta',
      status: STATUS.ABORTED,
      statusReason: 'Sync failed in Argo',
      createdAt: new Date(now - twoDays - 60 * 60 * 1000),
      updatedAt: new Date(now - twoDays - 30 * 60 * 1000),
    }),
  ]);
};

export const seedTaskDetail = async () => {
  const now = Date.now();
  const createdAt = new Date(now - 2 * 60 * 60 * 1000);
  const updatedAt = new Date(now - 60 * 60 * 1000);
  return createTaskWithMetadata({
    app: 'detail-app',
    project: 'https://github.com/shini4i/argo-watcher',
    author: 'ops@example.com',
    status: STATUS.FAILED,
    statusReason: 'Sync failed due to health check timeout.',
    images: [
      { image: 'ghcr.io/shini4i/argo-watcher', tag: 'v1.2.3' },
      { image: 'docker.io/library/nginx', tag: '1.25.3' },
      { image: 'gcr.io/example/db', tag: '2024-11-01' },
    ],
    createdAt,
    updatedAt,
  });
};
