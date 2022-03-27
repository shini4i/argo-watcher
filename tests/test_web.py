from fastapi.testclient import TestClient

from watcher.web import app
from test_in_memory_state import task_template

client = TestClient(app)


def test_add_task():
    response = client.post("/api/v1/tasks", json=task_template)

    assert response.status_code == 202
    assert response.json()['status'] == "accepted"
    assert len(response.json()['id']) == 36


def test_get_task_status():
    task_id = client.post("/api/v1/tasks", json=task_template).json()['id']
    response = client.get(f"/api/v1/tasks/{task_id}")

    assert response.json() == 200
