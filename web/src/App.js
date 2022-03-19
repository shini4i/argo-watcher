import {useEffect, useState} from "react";

function App() {

  const [tasks, setTasks] = useState([]);

  useEffect(() => {

    fetch('/api/v1/tasks')
        .then(res => res.json())
        .then(items => {
            setTasks(items);
        });
  }, [])

  return (
    <div>
        <h1>Argo Watcher - Home page</h1>
        <pre>
            {JSON.stringify(tasks, null, 2)}
        </pre>
    </div>
  );
}

export default App;
