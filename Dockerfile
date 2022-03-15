FROM python:3.10.2-alpine3.15 as base

WORKDIR /src

FROM base as builder

RUN apk add --no-cache gcc musl-dev python3-dev libffi-dev openssl-dev \
 && pip install cryptography==3.1.1 \
 && pip install "poetry==1.1.5"

COPY poetry.lock pyproject.toml /src/

RUN poetry export -f requirements.txt | pip install -r /dev/stdin

COPY . .

RUN poetry build

FROM base as final

WORKDIR /app

RUN apk add --no-cache git openssh \
 && adduser -u 1000 -h /app -D deployer

# Addreses security vulnerability
RUN apk add --no-cache "expat>2.4.4"

# A shitty hack that makes sure that multi stage copy cache is invalidated
# https://github.com/GoogleContainerTools/kaniko/issues/1262
ARG CI_COMMIT_SHA
RUN echo $CI_COMMIT_SHA > /CI_COMMIT_SHA

COPY --from=builder /src/dist/*.tar.gz /app
COPY run.py /app/

RUN pip install *.tar.gz && rm -f *.tar.gz

USER deployer

CMD ["./run.py"]
