#!/usr/bin/env python

from os import getenv
from os.path import isdir as exists

from fastapi import FastAPI, BackgroundTasks, status
from fastapi.staticfiles import StaticFiles
from starlette.responses import FileResponse
from uvicorn import Config, Server
from uuid import uuid1

from typing import List

from watcher.argo import Argo
from watcher.models import Task
from watcher.logs import setup_logging
from watcher.settings import Settings

app = FastAPI(
    title="ArgoCD Watcher",
    description="A small tool that will wait for the specific docker image to be rolled out",
    version="0.0.1"
)

if exists("static"):
    app.mount("/", StaticFiles(directory="static", html=True))

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
    task.id = str(uuid1())
    background_tasks.add_task(argo.start_task, task=task)
    return {"status": "accepted", "id": task.id}


@app.get("/api/v1/tasks/{task_id}", status_code=status.HTTP_200_OK,
         responses={
             200: {
                 "content": {
                     "application/json": {
                         "example": {
                             "status": "deployed"
                         }
                     }
                 }
             }
         })
def get_task_details(task_id: str):
    return {"status": argo.get_task_status(task_id=task_id)}


@app.get("/api/v1/tasks", status_code=status.HTTP_200_OK, response_model=List[Task])
def get_state():
    return argo.return_state()


@app.get("/")
async def index():
    return FileResponse('static/index.html', media_type='text/html')


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
