#!/usr/bin/env python

from os import getenv

from fastapi import FastAPI, Response, status
from uvicorn import Config, Server
from tenacity import RetryError

from watcher.argo import Argo, AppDoesNotExistException
from watcher.models import Images
from watcher.logs import setup_logging
from watcher.settings import Settings

app = FastAPI(
    title="ArgoCD Rollout Watcher",
    description="A small tool that is waiting for the specific docker image to be rolled out",
    version="0.0.1"
)
argo = Argo()


@app.post("/api/v1/status", status_code=status.HTTP_200_OK,
          response_description="If the required version was rolled out",
          responses={
              200: {
                  "content": {
                      "application/json": {
                          "example": {
                              "deployed": True
                          },
                      }
                  }
              },
              424: {
                  "content": {
                      "application/json": {
                          "example": {
                              "deployed": False
                          },
                      }
                  }
              },
              404: {
                  "content": {
                      "application/json": {
                          "example": {
                              "error": "App does not exists"
                          },
                      }
                  }
              }
          }
          )
def get_status(payload: Images, response: Response):
    try:
        return {"deployed": argo.wait_for_rollout(payload)}
    except RetryError:
        response.status_code = status.HTTP_424_FAILED_DEPENDENCY
        return {"deployed": False}
    except AppDoesNotExistException:
        response.status_code = status.HTTP_404_NOT_FOUND
        return {"error": "App does not exists"}


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
