# Development

## Prerequisites
This project depends on various git hooks. ([pre-commit](https://pre-commit.com))

They can be installed by running:
```bash
pre-commit install
```

## Back-End Development

To start developing argo-watcher you will need golang 1.19+

Start mock of the argo-cd server
```shell
# go to mock directory
cd cmd/mock
# start the server
go run .
```

Start the argo-watcher server
```shell
# go to backend directory
cd cmd/argo-watcher
# install dependencies
go mod tidy
# start argo-watcher
ARGO_URL=http://localhost:8081 STATE_TYPE=in-memory go run .
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
