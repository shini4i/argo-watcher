import logging
from os import getenv

from environs import Env
from marshmallow.validate import OneOf

env = Env()


class Config:
    class Argo:
        url = env.str("ARGO_URL")
        user = env.str("ARGO_USER")
        password = env.str("ARGO_PASSWORD")
        timeout = env.int("ARGO_TIMEOUT", 300)

    class Watcher:
        state_type = env.str(
            "STATE_TYPE",
            "in-memory",
            validate=OneOf(
                ["in-memory", "postgres"], error="STATE_TYPE must be one of {choices}"
            ),
        )
        ssl_verify = env.bool("SSL_VERIFY", True)
        history_ttl = env.int("HISTORY_TTL", 3600)

    class DB:
        host = getenv("DB_HOST")
        db_name = getenv("DB_NAME")
        db_user = getenv("DB_USER")
        db_password = getenv("DB_PASSWORD")

    class Logs:
        log_level = env.log_level("LOG_LEVEL", logging.INFO)
