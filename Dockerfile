ARG PYTHON_VERSION=3.10.3-slim-buster
ARG NODE_VERSION=17.7-alpine3.15

##################
# Backend build
##################
FROM python:${PYTHON_VERSION} as builder-backend

WORKDIR /src

RUN pip install cryptography==3.1.1 \
 && pip install "poetry==1.1.5"

COPY poetry.lock pyproject.toml /src/

RUN poetry export -f requirements.txt | pip install -r /dev/stdin

COPY . .

RUN poetry build

##################
# Frontend build
##################
FROM node:${NODE_VERSION} as builder-frontend

WORKDIR /app

COPY web/package.json .
COPY web/package-lock.json .

RUN npm ci --silent
RUN npm install react-scripts --silent

COPY web/ .

RUN npm run build

##################
# The resulting image build
##################
FROM python:${PYTHON_VERSION}

WORKDIR /app

RUN apt update && apt install libpq-dev -y && apt clean

RUN adduser --uid 1000 --home /app --disabled-password --gecos "" app

COPY --chown=app:app --from=builder-backend /src/dist/*.tar.gz /app
COPY --chown=app:app --from=builder-frontend /app/build /app/static
COPY --chown=app:app run.py /app/
COPY --chown=app:app db /app/db/
COPY --from=amacneil/dbmate /usr/local/bin/dbmate /bin/dbmate

RUN pip install *.tar.gz && rm -f *.tar.gz

USER app

CMD ["./run.py"]
