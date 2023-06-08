#######################
# Backend build
#######################
FROM golang:1.20-alpine3.16 as builder-backend

ARG APP_VERSION

WORKDIR /src

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$APP_VERSION" -o argo-watcher ./cmd/argo-watcher

#######################
# Frontend build
#######################
FROM node:17.7-alpine3.15 as builder-frontend

WORKDIR /app

COPY web/package.json .
COPY web/package-lock.json .

RUN npm ci --silent
RUN npm install react-scripts --silent

COPY web/ .

RUN npm run build

#######################
# Final image
#######################
FROM alpine:3.18

COPY --from=builder-backend /src/argo-watcher /argo-watcher
COPY --from=builder-frontend /app/build /static

RUN apk add --no-cache ca-certificates

COPY db /db

CMD ["/argo-watcher", "-server"]
