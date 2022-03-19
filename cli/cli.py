#!/usr/bin/env python

import click
import requests
from time import sleep


def generate_task(app, author, images, tag) -> dict:
    return {
        "app": app,
        "author": author,
        "images": [{"image": image, "tag": tag} for image in images],
    }


def send_task(url: str, task: dict) -> str:
    r = requests.post(url=url, json=task)
    return r.json()['id']


def check_status(url: str, task_id: str) -> str:
    return requests.get(url=f"{url}/{task_id}").json()['status']


@click.command()
@click.option("--url", help="argo-watcher url", default="http://localhost:8080/api/v1/tasks")
@click.option("--app", help="ArgoCD Application name", required=True)
@click.option("--author", help="Name of the person who triggered the pipeline", required=True)
@click.option("--image", help="Image name that should contain specific tag", required=True, multiple=True)
@click.option("--tag", help="Expected tag", required=True)
def main(url, app, author, image, tag):
    task = generate_task(app=app, author=author, images=image, tag=tag)
    task_id = send_task(url=url, task=task)
    while (status := check_status(url=url, task_id=task_id)) not in ["deployed", "failed", "app not found"]:
        click.echo("Application deployment is in progress...")
        sleep(5)

    if status in ["failed", "app not found"]:
        click.echo("The deployment has failed, please check logs.")
        exit(1)
    else:
        click.echo(f"The deployment of {tag} version is done.")


if __name__ == '__main__':
    main()
