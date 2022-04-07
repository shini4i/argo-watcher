export function fetchTasks(timestamp, application = null) {
  let queryString = "?from_timestamp=" + timestamp;
  if (application !== null) {
    queryString += "&app=" + application;
  }
  return fetch(`/api/v1/tasks${queryString}`)
      .then(res => {
        if (res.status !== 200) {
          throw new Error(res.statusText);
        }
        return res.json();
      });
}

export function fetchApplications() {
  return fetch(`/api/v1/apps`)
      .then(res => {
        if (res.status !== 200) {
          throw new Error(res.statusText);
        }
        return res.json();
      });
}
