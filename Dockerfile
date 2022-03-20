ARG PYTHON_VERSION=3.10.2-alpine3.15

# Backend build
FROM python:${PYTHON_VERSION} as builder-backend

WORKDIR /src

RUN apk add --no-cache gcc musl-dev python3-dev libffi-dev openssl-dev \
 && pip install cryptography==3.1.1 \
 && pip install "poetry==1.1.5"

COPY poetry.lock pyproject.toml /src/

RUN poetry export -f requirements.txt | pip install -r /dev/stdin

COPY . .

RUN poetry build

# Frontend build
FROM node:17.7-alpine3.15 as builder-frontend

WORKDIR /app

COPY web/package.json .
COPY web/package-lock.json .

RUN npm ci --silent
RUN npm install react-scripts --silent

COPY web/ .

RUN npm run build

# The resulting image build
FROM python:${PYTHON_VERSION}

WORKDIR /app

RUN adduser -u 1000 -h /app -D app

USER app

COPY --chown=app:app --from=builder-backend /src/dist/*.tar.gz /app
COPY --chown=app:app --from=builder-frontend /app/build /app/static
COPY --chown=app:app  run.py /app/

RUN pip install *.tar.gz && rm -f *.tar.gz

CMD ["./run.py"]
