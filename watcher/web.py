#!/usr/bin/env python

from os import getenv

from fastapi import FastAPI, BackgroundTasks, status
from uvicorn import Config, Server
from uuid import uuid1

from watcher.argo import Argo
from watcher.models import Task
from watcher.logs import setup_logging
from watcher.settings import Settings

app = FastAPI(
    title="ArgoCD Rollout Watcher",
    description="A small tool that will wait for the specific docker image to be rolled out",
    version="0.0.1"
)
argo = Argo()


@app.post("/api/v1/tasks", status_code=status.HTTP_202_ACCEPTED,
          responses={
              202: {
                  "content": {
                      "application/json": {
                          "example": {
                              "status": "accepted",
                              "id": "a09791dc-a615-11ec-b182-f2c4bb72758c"
                          },
                      }
                  }
              }
          })
def add_task(background_tasks: BackgroundTasks, task: Task):
    task_id = str(uuid1())
    background_tasks.add_task(argo.start_task, task=task, task_id=task_id)
    return {"status": "accepted", "id": task_id}


@app.get("/api/v1/tasks/{task_id}", status_code=status.HTTP_200_OK,
         responses={
             200: {
                 "content": {
                     "application/json": {
                         "example": {
                             "status": "Deployed"
                         }
                     }
                 }
             }
         })
def get_task_details(task_id: str):
    return {"status": argo.get_task_status(task_id=task_id)}


@app.get("/api/v1/tasks", status_code=status.HTTP_200_OK,
         responses={
             200: {
                 "content": {
                     "application/json": {
                         "example": {
                             "484269da-a647-11ec-82ad-f2c4bb72758a": {
                                 "app": "test",
                                 "author": "John Doe",
                                 "tags": ["v0.1.0"],
                                 "status": "Deployed"
                             }
                         }
                     }
                 }
             }
         })
def get_state():
    return argo.return_state()


def main():
    server = Server(
        Config(
            "watcher.web:app",
            host=getenv('BIND_IP', '0.0.0.0'),
            port=8080,
            log_level=Settings.Logs.log_level,

        ),
    )

    setup_logging()

    server.run()


if __name__ == "__main__":
    main()
