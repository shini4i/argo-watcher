#!/usr/local/bin/python

from os import environ
from time import sleep

import requests
from requests.exceptions import JSONDecodeError
from tenacity import RetryError
from tenacity import retry
from tenacity import retry_if_exception_type
from tenacity import stop_after_attempt
from tenacity import wait_fixed


class Client:
    def __init__(self):
        self.task = self._generate_task()
        self.task_id = self._send_task()

    @staticmethod
    def _generate_task() -> dict:
        return {
            "app": environ["ARGO_APP"],
            "author": environ["COMMIT_AUTHOR"],
            "project": environ["PROJECT_NAME"],
            "images": [
                {"image": image, "tag": environ["IMAGE_TAG"]}
                for image in environ["IMAGES"].split(",")
            ],
        }

    @retry(
        retry=retry_if_exception_type(JSONDecodeError),
        wait=wait_fixed(5),
        stop=stop_after_attempt(5),
    )
    def _send_task(self) -> int:
        try:
            return requests.post(
                url=f"{environ['ARGO_WATCHER_URL']}/api/v1/tasks", json=self.task
            ).json()["id"]
        except JSONDecodeError:
            print("Something went wrong. Retrying...", flush=True)
            raise JSONDecodeError

    @retry(
        retry=retry_if_exception_type(JSONDecodeError),
        wait=wait_fixed(5),
        stop=stop_after_attempt(5),
    )
    def get_deployment_status(self) -> str:
        try:
            return requests.get(
                url=f"{environ['ARGO_WATCHER_URL']}/api/v1/tasks/{self.task_id}"
            ).json()["status"]
        except JSONDecodeError:
            print("Something went wrong. Retrying...", flush=True)
            raise JSONDecodeError


def main():
    client = Client()

    try:
        while (status := client.get_deployment_status()) == "in progress":
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
                print(
                    f"The deployment of {environ['IMAGE_TAG']} version is done.",
                    flush=True,
                )

    except RetryError:
        print("There is a problem with argo-watcher instance. Please investigate.")


if __name__ == "__main__":
    main()
