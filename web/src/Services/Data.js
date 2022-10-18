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
      return res.tasks;
    });
}

export function fetchApplications() {
  return fetch(`/api/v1/apps`).then(res => {
    if (res.status !== 200) {
      throw new Error(res.statusText);
    }
    return res.json();
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
