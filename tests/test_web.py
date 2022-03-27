import responses

from fastapi.testclient import TestClient

from watcher.web import app
from test_in_memory_state import task_template

client = TestClient(app)


def responses_configuration():
    responses.add(responses.GET, 'https://argocd.example.com', status=200)
    responses.add(method=responses.GET,
                  url='https://argocd.example.com/api/v1/applications/test_app',
                  json={'status': {
                      'summary': {'images': ['example:latest']},
                      'sync': {'status': 'Synced'},
                      'health': {'status': 'Healthy'}
                  }},
                  status=200)


@responses.activate
def test_add_task():
    responses_configuration()
    response = client.post("/api/v1/tasks", json=task_template)

    assert response.status_code == 202
    assert response.json()['status'] == "accepted"
    assert len(response.json()['id']) == 36


@responses.activate
def test_get_task_status():
    responses_configuration()
    task_id = client.post("/api/v1/tasks", json=task_template).json()['id']
    response = client.get(f"/api/v1/tasks/{task_id}")

    assert response.status_code == 200
    assert response.json()['status'] == "deployed"
