from time import time

import responses
from fastapi.testclient import TestClient
from test_in_memory_state import task_template

from watcher.app import app

client = TestClient(app)
api_path = "/api/v1/tasks"
version = "0.0.8"


def responses_configuration():
    responses.add(method=responses.GET, url="https://argocd.example.com", status=200)
    responses.add(
        method=responses.GET,
        url="https://argocd.example.com/api/v1/applications/test_app",
        json={
            "status": {
                "summary": {"images": ["example:latest"]},
                "sync": {"status": "Synced"},
                "health": {"status": "Healthy"},
            }
        },
        status=200,
    )
    responses.add(
        method=responses.GET,
        url="https://argocd.example.com/api/v1/applications/example",
        json={
            "status": {
                "summary": {"images": ["example:latest"]},
                "sync": {"status": "Synced"},
                "health": {"status": "Healthy"},
            }
        },
        status=200,
    )


@responses.activate
def test_healthz():
    responses_configuration()

    responses.add(
        method=responses.GET,
        url="https://argocd.example.com/api/v1/session/userinfo",
        json={"loggedIn": True},
        status=200,
    )

    response = client.get("/healthz")

    assert response.status_code == 200
    assert response.json()["argo_status"] == "up"

    responses.add(
        method=responses.GET,
        url="https://argocd.example.com/api/v1/session/userinfo",
        json={},
        status=503,
    )

    response = client.get("/healthz")

    assert response.status_code == 503
    assert response.json()["argo_status"] == "down"


@responses.activate
def test_add_task():
    responses_configuration()
    response = client.post(api_path, json=task_template)

    assert response.status_code == 202
    assert response.json()["status"] == "accepted"
    assert len(response.json()["id"]) == 36


@responses.activate
def test_get_task_status():
    responses_configuration()
    task_id = client.post(api_path, json=task_template).json()["id"]
    response = client.get(f"{api_path}/{task_id}")

    assert response.status_code == 200
    assert response.json()["status"] == "deployed"


@responses.activate
def test_get_state_with_filter():
    responses_configuration()
    target_task = task_template.copy()
    target_task["app"] = "example"

    client.post(api_path, json=task_template)
    client.post(api_path, json=task_template)
    client.post(api_path, json=target_task)

    response = client.get(
        api_path, params={"from_timestamp": time() - 60, "app": "example"}
    )

    assert response.status_code == 200
    assert len(response.json()) == 1
    assert response.json()[0]["app"] == "example"


def test_get_version():
    response = client.get("/api/v1/version")

    assert response.json() == {"version": version}


def test_get_app_list():
    response = client.get("/api/v1/apps")

    assert len(response.json()) == 2
    assert set(response.json()) == {"test_app", "example"}
