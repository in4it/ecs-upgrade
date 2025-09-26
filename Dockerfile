#
# Build go project
#
FROM golang:1.24.7-alpine as go-builder

WORKDIR /ecs-upgrade/

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o /ecs-upgrade/ecs-upgrade *.go

#
# Runtime container
#
FROM alpine:3.22.1

RUN apk --no-cache add ca-certificates && mkdir -p /app

WORKDIR /app

COPY --from=go-builder /ecs-upgrade/ecs-upgrade .

# create a non-root user to run the application
RUN apk add --no-cache shadow \
    && useradd -u 1000 -U -m appuser \
    && chown -R appuser:appuser /app

# Switch to the non-root user
USER appuser

CMD ["./ecs-upgrade"]
