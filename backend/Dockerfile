FROM golang:1.25.7-alpine AS build

WORKDIR /src

RUN apk add --no-cache ca-certificates tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/api ./cmd
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/worker ./cmd/worker

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata wget

COPY --from=build /out/api /app/api
COPY --from=build /out/worker /app/worker
COPY configs /app/configs

RUN mkdir -p /app/.run/uploads

EXPOSE 8080 6060 6061

CMD ["/app/api"]
