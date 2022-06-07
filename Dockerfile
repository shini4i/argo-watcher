##################
# Backend build
##################
FROM golang:1.18-alpine3.16 as builder-backend

WORKDIR /src

COPY cmd/ /src/

RUN apk add --no-cache ca-certificates

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o argo-watcher

##################
# Frontend build
##################
FROM node:17.7-alpine3.15 as builder-frontend

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
FROM scratch

COPY --from=builder-backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder-backend /src/argo-watcher /argo-watcher
COPY --from=builder-frontend /app/build /static
COPY db /db

CMD ["/argo-watcher"]
