ARG PYTHON_VERSION=3.10.2-alpine3.15

FROM python:${PYTHON_VERSION} as builder

WORKDIR /src

RUN apk add --no-cache gcc musl-dev python3-dev libffi-dev openssl-dev \
 && pip install cryptography==3.1.1 \
 && pip install "poetry==1.1.5"

COPY poetry.lock pyproject.toml /src/

RUN poetry export -f requirements.txt | pip install -r /dev/stdin

COPY . .

RUN poetry build

FROM python:${PYTHON_VERSION}

WORKDIR /app

RUN adduser -u 1000 -h /app -D app

# Addreses security vulnerability
RUN apk add --no-cache "expat>2.4.4"

COPY --from=builder /src/dist/*.tar.gz /app
COPY run.py /app/

RUN pip install *.tar.gz && rm -f *.tar.gz

USER app

CMD ["./run.py"]
