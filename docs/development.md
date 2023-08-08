# Development

## Prerequisites

This project depends on various git hooks. ([pre-commit](https://pre-commit.com))

They can be installed by running:

```bash
pre-commit install
```

To compile the project locally, you would also need to generate mocks (for testing) and swagger docs (for api documentation).

### Mock classes

To generate mock classes for unit tests, first install `gomock` tool.

```shell
go install go.uber.org/mock/mockgen@latest
```

Then run the mock generation from interfaces.

```shell
make mocks
```

### Swagger documentation

To generate documentation dependencies, first install `swag` tool.

```shell
go install github.com/swaggo/swag/cmd/swag@latest
```

Then run the swagger doc generation.

```shell
make docs
```

> Note: you need to run this only when you're changing the interfaces

## Back-End Development

To start developing argo-watcher you will need golang 1.19+

Start mock of the argo-cd server

```shell
# go to mock directory
cd cmd/mock
# start the server
go run .
```

### Start the argo-watcher server (in-memory)

```shell
# go to backend directory
cd cmd/argo-watcher
# install dependencies
go mod tidy
# start argo-watcher (in-memory)
LOG_LEVEL=debug LOG_FORMAT=text ARGO_URL=http://localhost:8081 ARGO_TOKEN=example STATE_TYPE=in-memory go run . -server
```


### Start the argo-watcher server (postgres)

Start database
```shell
# start the database in a separate terminal window
docker compose up postgres 
```

Start server
```shell
# go to backend directory
cd cmd/argo-watcher
# install dependencies
go mod tidy
# OR start argo-watcher (postgres)
LOG_LEVEL=debug LOG_FORMAT=text ARGO_URL=http://localhost:8081 ARGO_TOKEN=example STATE_TYPE=postgres DB_USER=watcher DB_PASSWORD=watcher DB_NAME=watcher DB_MIGRATIONS_PATH="./../../db/migrations" go run . -server
```

#### Logs in simple text

```shell
# add LOG_FORMAT=text for simple text logs
LOG_LEVEL=debug LOG_FORMAT=text go run . -server
```

### Running the unit tests

Use the following snippets to run argo-watcher unit tests

```shell
# go to backend directory
cd cmd/argo-watcher
# run all tests
go test -v
# run single test suite
go test -v -run TestArgoStatusUpdaterCheck
```

## Front-End Development

To start developing front-end you will need

1. NodeJS version 17.7.0+
2. NPM (comes with NodeJS) 8.9.0+

```shell
# go into web directory
cd web
# install dependencies
npm install
# start web development server
npm start
```

The browser will open on http://localhost:3000

## Requests examples

### Add a task

Post request:

```bash
curl --header "Content-Type: application/json" \
     --request POST \
     --data '{"app":"test-app","author":"name","project":"example","images":[{"image":"example", "tag":"v1.8.0"}]}' \
     http://localhost:8080/api/v1/tasks
```

Example response:

```bash
{"status":"accepted","id":"be8c42c0-a645-11ec-8ea5-f2c4bb72758a"}
```

### Get task details

The ID provided in response for POST request should be provided to get task status:

```bash
curl http://localhost:8080/api/v1/tasks/be8c42c0-a645-11ec-8ea5-f2c4bb72758a
```

Example response:

```bash
{"status":"in progress"}
```

## Swagger

A swagger documentation can be accessed via http://localhost:8080/swagger/index.html
