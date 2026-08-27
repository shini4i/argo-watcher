import type {
  CreateParams,
  CreateResult,
  DataProvider,
  DeleteManyParams,
  DeleteManyResult,
  DeleteParams,
  DeleteResult,
  GetListParams,
  GetListResult,
  GetManyParams,
  GetManyReferenceParams,
  GetManyReferenceResult,
  GetManyResult,
  GetOneParams,
  GetOneResult,
  RaRecord,
  UpdateManyParams,
  UpdateManyResult,
  UpdateParams,
  UpdateResult,
} from 'react-admin';
import { HttpError } from 'react-admin';
import { buildQueryString, httpClient } from './httpClient';
import type { Task, TaskListFilter, TasksResponse, TaskStatus } from './types';

const RESOURCE_TASKS = 'tasks';

// Mirrors the backend's allowedTaskStatusFilters (internal/models/constants.go).
// Stale URL or localStorage values that don't match are dropped before we hit
// the wire so they don't trigger a 400 from the validating handler.
const ALLOWED_TASK_STATUSES: ReadonlySet<string> = new Set([
  'app not found',
  'in progress',
  'failed',
  'aborted',
  'argocd is unavailable',
  'cannot connect to database',
  'failed to login to argocd',
  'deployed',
  'accepted',
  'cancelled',
]);

const toUnixSeconds = (value: Date | string | number | undefined, fallback: number): number => {
  if (value === undefined || value === null) {
    return fallback;
  }

  if (value instanceof Date) {
    return Math.floor(value.getTime() / 1000);
  }

  const parsed = typeof value === 'string' ? Date.parse(value) : Number(value) * 1000;
  if (Number.isNaN(parsed)) {
    return fallback;
  }

  return Math.floor(parsed / 1000);
};

const selectListWindow = (params: GetListParams) => {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const defaultFrom = nowSeconds - 24 * 60 * 60;

  const filter = (params.filter ?? {}) as TaskListFilter & { start?: number; end?: number };

  const fromCandidate = filter.start ?? filter.from;
  const toCandidate = filter.end ?? filter.to;

  return {
    from: fromCandidate == null ? defaultFrom : toUnixSeconds(fromCandidate, defaultFrom),
    to: toCandidate == null ? undefined : toUnixSeconds(toCandidate, nowSeconds),
    app: filter.app,
    status: filter.status && ALLOWED_TASK_STATUSES.has(filter.status) ? filter.status : undefined,
  };
};

/** Go's `omitempty` drops `total` on an empty result set, hence the coalesce to 0. */
const toRaListResult = (response: TasksResponse): GetListResult<Task> => ({
  data: response.tasks ?? [],
  total: response.total ?? 0,
});

const ensureSupportedResource = (resource: string) => {
  if (resource !== RESOURCE_TASKS) {
    throw new HttpError(`Unsupported resource: ${resource}`, 404);
  }
};

const getList = async (params: GetListParams): Promise<GetListResult<Task>> => {
  const timeframe = selectListWindow(params);
  const { perPage, page } = params.pagination;
  const limit = perPage;
  const offset = (page - 1) * perPage;

  const query = buildQueryString({
    from_timestamp: timeframe.from,
    to_timestamp: timeframe.to,
    app: timeframe.app,
    status: timeframe.status,
    limit,
    offset,
  });

  const { data } = await httpClient<TasksResponse>(`/api/v1/${RESOURCE_TASKS}${query}`);
  const response = data ?? { tasks: [], total: 0 };

  // Backend returns HTTP 200 with a non-empty `error` field when ArgoCD is unreachable.
  // Not rejecting lets the empty-state placeholder render instead of leaving the
  // Datagrid in its loading skeleton forever.
  if (response.error) {
    console.warn(`Tasks endpoint reported a soft error: ${String(response.error).slice(0, 200)}`);
  }

  return toRaListResult(response);
};

const getOne = async (params: GetOneParams): Promise<GetOneResult<TaskStatus>> => {
  const { data, status } = await httpClient<TaskStatus>(`/api/v1/${RESOURCE_TASKS}/${params.id}`);
  // httpClient throws on a real 404, so an empty body here is a 2xx that carried no
  // JSON — an intermediary answering, not a missing task. Status 0 says transport.
  if (!data) {
    throw new HttpError('The server returned no task data', 0);
  }
  // The real response status, never a synthesized 404: this body reports an error on a
  // success, and describeReadFailure names it as such rather than as a missing task.
  if (data.error) {
    throw new HttpError(data.error, status, data);
  }

  return {
    data,
  };
};

const createTask = async (params: CreateParams): Promise<CreateResult<TaskStatus & RaRecord>> => {
  const { data: payload } = params;

  const { data, status } = await httpClient<TaskStatus>(`/api/v1/${RESOURCE_TASKS}`, {
    method: 'POST',
    body: payload,
  });

  if (!data?.id) {
    throw new HttpError('Task creation did not return an identifier', status, data);
  }

  return { data: data as TaskStatus & RaRecord };
};

const unsupported = (method: string): Promise<never> =>
  Promise.reject(new HttpError(`${method} is not supported by this resource`, 405));

export const dataProvider: DataProvider = {
  getList: async (resource, params: GetListParams) => {
    ensureSupportedResource(resource);
    return getList(params);
  },
  getOne: async (resource, params: GetOneParams) => {
    ensureSupportedResource(resource);
    return getOne(params);
  },
  getMany: async (resource, params: GetManyParams) => {
    ensureSupportedResource(resource);
    const records = await Promise.all(
      params.ids.map(async id => {
        const result = await getOne({ id });
        const typedResult: TaskStatus & RaRecord = {
          ...result.data,
          id: result.data.id ?? id,
        };
        return typedResult;
      }),
    );

    return { data: records } satisfies GetManyResult<TaskStatus & RaRecord>;
  },
  getManyReference: async (resource, _params: GetManyReferenceParams): Promise<GetManyReferenceResult<RaRecord>> => {
    ensureSupportedResource(resource);
    return { data: [], total: 0 };
  },
  create: async (resource, params: CreateParams) => {
    ensureSupportedResource(resource);
    return createTask(params);
  },
  update: (_resource: string, _params: UpdateParams): Promise<UpdateResult> => unsupported('update'),
  updateMany: (_resource: string, _params: UpdateManyParams): Promise<UpdateManyResult> => unsupported('updateMany'),
  delete: (_resource: string, _params: DeleteParams): Promise<DeleteResult> => unsupported('delete'),
  deleteMany: (_resource: string, _params: DeleteManyParams): Promise<DeleteManyResult> => unsupported('deleteMany'),
};
