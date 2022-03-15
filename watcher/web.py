#!/usr/bin/env python

from os import getenv

from fastapi import FastAPI, status
from uvicorn import Config, Server
from tenacity import RetryError

from watcher.argo import Argo
from watcher.models import Images
from watcher.logs import setup_logging
from watcher.settings import Settings

app = FastAPI()
argo = Argo()


@app.post("/api/v1/status", status_code=status.HTTP_202_ACCEPTED)
async def get_status(payload: Images):
    try:
        return argo.wait_for_rollout(payload)
    except RetryError:
        return False


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
