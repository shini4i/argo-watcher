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

/** Normalizes various date/timestamp inputs to seconds since epoch. */
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

/** Builds the time window and filters for list queries based on React-admin params. */
const selectListWindow = (params: GetListParams) => {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const defaultFrom = nowSeconds - 24 * 60 * 60;

  const filter = (params.filter ?? {}) as TaskListFilter & { start?: number; end?: number };

  const fromCandidate = filter.start ?? filter.from;
  const toCandidate = filter.end ?? filter.to;

  return {
    from: fromCandidate ? toUnixSeconds(fromCandidate, defaultFrom) : defaultFrom,
    to: toCandidate ? toUnixSeconds(toCandidate, nowSeconds) : undefined,
    app: filter.app,
  };
};

/**
 * Normalises backend task list responses into React-admin's list structure while
 * keeping pagination totals aligned with what the UI can actually display.
 */
/** Converts backend pagination metadata into the structure expected by React-admin. */
const toRaListResult = (response: TasksResponse, params: GetListParams): GetListResult<Task> => {
  const tasks = response.tasks ?? [];
  const pagination = params.pagination ?? { page: 1, perPage: tasks.length || 0 };
  const pageSize = pagination.perPage ?? tasks.length;
  const offset = Math.max(0, (pagination.page - 1) * pageSize);

  const total =
    typeof response.total === 'number' ? response.total : Math.max(tasks.length, offset + tasks.length);

  return {
    data: tasks,
    total,
  };
};

/** Guards against consumers requesting unsupported resources via the data provider. */
const ensureSupportedResource = (resource: string) => {
  if (resource !== RESOURCE_TASKS) {
    throw new HttpError(`Unsupported resource: ${resource}`, 404);
  }
};

/** Fetches a page of tasks applying time range filters, pagination, and app filters. */
const getList = async (params: GetListParams): Promise<GetListResult<Task>> => {
  const timeframe = selectListWindow(params);
  const { perPage, page } = params.pagination;
  const limit = perPage;
  const offset = (page - 1) * perPage;

  const query = buildQueryString({
    from_timestamp: timeframe.from,
    to_timestamp: timeframe.to,
    app: timeframe.app,
    limit,
    offset,
  });

  const { data } = await httpClient<TasksResponse>(`/api/v1/${RESOURCE_TASKS}${query}`);
  const response = data ?? { tasks: [], total: 0 };
  return toRaListResult(response, params);
};

/** Retrieves a single task detail, throwing when the backend reports an error. */
const getOne = async (params: GetOneParams): Promise<GetOneResult<TaskStatus>> => {
  const { data } = await httpClient<TaskStatus>(`/api/v1/${RESOURCE_TASKS}/${params.id}`);
  if (!data) {
    throw new HttpError('Task not found', 404);
  }
  if (data.error) {
    throw new HttpError(data.error, 404, data);
  }

  return {
    data,
  };
};

/** Posts a task to the backend and returns the accepted record with its identifier. */
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

/** Utility helper for methods that the data provider intentionally does not implement. */
const unsupported = (method: string): Promise<never> =>
  Promise.reject(new HttpError(`${method} is not supported by this resource`, 405));

/** React-admin DataProvider backed by the Go API used to list/create tasks. */
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
