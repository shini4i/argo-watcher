#!/usr/bin/env python

from os import environ
from time import sleep

import requests


def generate_task() -> dict:
    return {
        "app": environ['ARGO_APP'],
        "author": environ['COMMIT_AUTHOR'],
        "project": environ['PROJECT_NAME'],
        "images": [{"image": image, "tag": environ['IMAGE_TAG']} for image in environ["IMAGES"].split(',')],
    }


def send_task(task: dict) -> str:
    return requests.post(url=f"{environ['ARGO_WATCHER_URL']}/api/v1/tasks", json=task).json()['id']


def check_status(task_id: str) -> str:
    return requests.get(url=f"{environ['ARGO_WATCHER_URL']}/api/v1/tasks/{task_id}").json()['status']


def main():
    task = generate_task()
    task_id = send_task(task=task)

    while (status := check_status(task_id=task_id)) == "in progress":
        print("Application deployment is in progress...", flush=True)
        sleep(15)

    match status:
        case "failed":
            print("The deployment has failed, please check logs.", flush=True)
            exit(1)
        case "app not found":
            print(f"Application {environ['ARGO_APP']} does not exist.", flush=True)
            exit(1)
        case "deployed":
            print(f"The deployment of {environ['IMAGE_TAG']} version is done.", flush=True)


if __name__ == '__main__':
    main()
