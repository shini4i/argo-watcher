import logging

from environs import Env
from marshmallow.validate import OneOf


class Config:
    def __init__(self):
        self.env = Env()

    def get_argo_url(self):
        return self.env.str("ARGO_URL")

    def get_argo_user(self):
        return self.env.str("ARGO_USER")

    def get_argo_password(self):
        return self.env.str("ARGO_PASSWORD")

    def get_argo_timeout(self):
        return self.env.int("ARGO_TIMEOUT", 300)

    def get_watcher_state_type(self):
        return self.env.str(
            "STATE_TYPE",
            "in-memory",
            validate=OneOf(
                ["in-memory", "postgres"], error="STATE_TYPE must be one of {choices}"
            ),
        )

    def get_watcher_ssl_verify(self):
        return self.env.bool("SSL_VERIFY", True)

    def get_watcher_history_ttl(self):
        return self.env.int("HISTORY_TTL", 3600)

    def get_log_level(self):
        return self.env.log_level("LOG_LEVEL", logging.INFO)


config = Config()
