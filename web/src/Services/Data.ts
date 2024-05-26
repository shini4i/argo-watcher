type TaskResponse = {
  error?: string;
  tasks?: any[];
};

type TaskDetailsResponse = {
  error?: string;
} & Record<any, any>;

type KeycloakConfig = {
  token_validation_interval: number;
  enabled: boolean;
  url: string;
  realm: string;
  client_id: string;
  privileged_groups: string[];
};

type ConfigType = {
  keycloak: KeycloakConfig,
  [key: string]: any
};

let config: ConfigType | null = null;

/**
 * Fetches tasks from the API endpoint.
 *
 * @param {number | null} fromTimestamp - The timestamp from which to fetch tasks.
 * @param {number | null} toTimestamp - The timestamp up to which to fetch tasks.
 * @param {string | null} application - The application for which to fetch tasks.
 * @returns {Promise<any[]>} - An array of tasks fetched from the API.
 */
export async function fetchTasks(
  fromTimestamp: number | null,
  toTimestamp: number | null,
  application: string | null = null
): Promise<any[]> {
  let searchParams: { [index: string]: string } = {};
  if (fromTimestamp) {
    searchParams.from_timestamp = fromTimestamp.toString();
  }
  if (toTimestamp) {
    searchParams.to_timestamp = toTimestamp.toString();
  }
  if (application) {
    searchParams.app = application;
  }

  const res: Response = await fetch(`/api/v1/tasks?${new URLSearchParams(searchParams)}`);
  if (res.status !== 200) {
    throw new Error(res.statusText);
  }

  const data: TaskResponse = await res.json();
  if (data?.error) {
    throw new Error(data.error);
  }

  return data?.tasks ?? [];
}

/**
 * Fetches a specific task from the API endpoint.
 *
 * @param {string | number} id - The unique identifier of the task to fetch.
 * @returns {Promise<{}>} - A promise that resolves to the fetched task details.
 */
export async function fetchTask(id: string | number): Promise<{}> {
  const res: Response = await fetch(`/api/v1/tasks/${id}`);
  if (res.status !== 200) {
    throw new Error(res.statusText);
  }

  const data: TaskDetailsResponse = await res.json();
  if (data?.error) {
    throw new Error(data.error);
  }

  return data;
}

/**
 * Fetches the version information from the API endpoint.
 *
 * @returns {Promise<any>} - A promise that resolves to the version information.
 */
export async function fetchVersion(): Promise<any> {
  const res: Response = await fetch(`/api/v1/version`);
  if (res.status !== 200) {
    throw new Error(res.statusText);
  }

  return res.json();
}

/**
 * Fetches the application configuration from the API endpoint.
 *
 * @returns {Promise<ConfigType>} - A promise that resolves to the application configuration.
 */
export async function fetchConfig(): Promise<ConfigType> {
  if (!config) {
    const response = await fetch('/api/v1/config');
    const data = await response.json();
    config = data as ConfigType;
  }
  return config as ConfigType;
}
