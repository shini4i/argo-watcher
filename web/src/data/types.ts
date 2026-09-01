export interface Image {
  image: string;
  tag: string;
}

export interface Task {
  id: string;
  created: number;
  updated: number;
  app: string;
  author: string;
  project: string;
  images: Image[];
  status?: string;
  status_reason?: string;
  is_rollback?: boolean;
  rollback_target_id?: string;
}

export interface TasksResponse {
  tasks: Task[];
  total?: number;
  error?: string;
}

export interface TaskStatus {
  id: string;
  created?: number;
  updated?: number;
  app?: string;
  author?: string;
  project?: string;
  images?: Image[];
  status?: string;
  status_reason?: string;
  is_rollback?: boolean;
  rollback_target_id?: string;
  error?: string;
}

export interface TaskListFilter {
  from?: Date | string | number;
  to?: Date | string | number;
  app?: string;
  status?: string;
  /** Free-text term matched by the backend against app, author and image:tag. */
  search?: string;
}

export interface TaskListResult {
  data: Task[];
  total: number;
}
