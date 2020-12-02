ARG VERSION="dev"

FROM golang:1.15-alpine AS build-env

WORKDIR /app

COPY . .

RUN go build -o service ./main.go

FROM alpine

RUN mkdir /app

RUN addgroup --system --gid 1000 appgroup
RUN adduser --disabled-password --uid 1000 --ingroup appgroup  --no-create-home appuser

WORKDIR /app

COPY --from=build-env /app/service /app/

RUN chown -R appuser:appgroup /app

USER 1000

CMD ["./service"]
