#!/usr/bin/env python

from os import getenv

from fastapi import FastAPI, status
from uvicorn import Config, Server

from watcher.argo import Argo
from watcher.models import Images

app = FastAPI()
argo = Argo()


@app.post("/api/v1/status", status_code=status.HTTP_202_ACCEPTED)
async def get_status(payload: Images):
    return argo.wait_for_rollout(payload)


def main():
    server = Server(
        Config(
            "watcher.web:app",
            host=getenv('BIND_IP', '0.0.0.0'),
            port=8080
        ),
    )

    server.run()


if __name__ == "__main__":
    main()
