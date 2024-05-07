export function fetchTasks(fromTimestamp, toTimestamp, application = null) {
  let searchParams = {};
  if (fromTimestamp) {
    searchParams.from_timestamp = fromTimestamp;
  }
  if (toTimestamp) {
    searchParams.to_timestamp = toTimestamp;
  }
  if (application) {
    searchParams.app = application;
  }
  return fetch(`/api/v1/tasks?${new URLSearchParams(searchParams)}`)
    .then(res => {
      if (res.status !== 200) {
        throw new Error(res.statusText);
      }
      return res.json();
    })
    .then(res => {
      if (res?.error) {
        throw new Error(res.error);
      }
      if (!res?.tasks) {
        return [];
      }
      return res.tasks;
    });
}

export function fetchTask(id) {
  return fetch(`/api/v1/tasks/${id}`)
    .then(res => {
      if (res.status !== 200) {
        throw new Error(res.statusText);
      }
      return res.json();
    })
    .then(res => {
      if (res?.error) {
        throw new Error(res.error);
      }
      return res;
    });
}

export function fetchVersion() {
  return fetch(`/api/v1/version`).then(res => {
    if (res.status !== 200) {
      throw new Error(res.statusText);
    }
    return res.json();
  });
}
