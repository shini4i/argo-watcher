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
  error?: string;
}

export interface TaskListFilter {
  from?: Date | string | number;
  to?: Date | string | number;
  app?: string;
}

export interface TaskListResult {
  data: Task[];
  total: number;
}
